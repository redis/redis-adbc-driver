// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Cumulative coordinator admission limits for opt-in retained execution.
type retainedExecutionLimits struct {
	TransportBytes int64 `json:"transport_bytes"`
	SourceBytes    int64 `json:"source_bytes"`
	WorkingBytes   int64 `json:"working_bytes"`
	Work           int64 `json:"work"`
	SourceRows     int   `json:"source_rows"`
	CandidateKeys  int   `json:"candidate_keys"`
}

func defaultRetainedExecutionLimits() retainedExecutionLimits {
	return retainedExecutionLimits{TransportBytes: 128 << 20, SourceBytes: 64 << 20, WorkingBytes: 256 << 20, Work: 1_000_000, SourceRows: 100_000, CandidateKeys: 200_000}
}

type retainedAllocationKey struct{}
type retainedWorkContext struct {
	context.Context
	work   *retainedBudget
	cancel context.CancelCauseFunc
}

// Existing SQL row loops already check Context.Err through cancelled. Only this
// private context consumes work credits; ordinary execution's hot path is unchanged.
func (c *retainedWorkContext) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	if err := c.work.reserve(1); err != nil {
		c.cancel(codeStateErrorf(adbc.StatusInvalidState, "53200", "retained execution work budget exhausted"))
		return c.Context.Err()
	}
	return nil
}
func retainedAllocate(ctx context.Context, count, width int64) error {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(retainedAllocationKey{}).(*retainedBudget)
	if budget == nil {
		return nil
	}
	if count < 0 || width < 0 || width > 0 && count > math.MaxInt64/width {
		return budget.reserve(-1)
	}
	return budget.reserve(count * width)
}

type retainedExecution struct {
	capture   *retainedCapture
	working   *retainedBudget
	transport *retainedBudget
	tables    map[string]*tableMeta
	views     map[string]string
	vetted    map[*SelectStmt]bool
	nodes     int
}

func (limits retainedExecutionLimits) valid() bool {
	return !(limits.TransportBytes < 1 || limits.TransportBytes > 1<<30 || limits.SourceBytes < 1 || limits.SourceBytes > 1<<30 ||
		limits.WorkingBytes < 1 || limits.WorkingBytes > 1<<30 || limits.Work < 1 || limits.Work > 10_000_000 ||
		limits.SourceRows < 1 || limits.SourceRows > retainedIPCRows || limits.CandidateKeys < 1 || limits.CandidateKeys > 2_000_000)
}
func newRetainedExecutor(ctx context.Context, base *executor, cfg *clientConfig, t timeouts, limits retainedExecutionLimits) (context.Context, *executor, *retainedExecution, func(), error) {
	if !limits.valid() {
		return nil, nil, nil, nil, errorf(adbc.StatusInvalidArgument, "invalid retained coordinator resource limits")
	}

	_, cluster := base.store.client.(*goredis.ClusterClient)
	working := newRetainedBudget(limits.WorkingBytes)
	timed, stopTimer := context.WithTimeout(ctx, time.Minute)
	parent, cancel := context.WithCancelCause(timed)
	workCtx := &retainedWorkContext{Context: parent, work: newRetainedBudget(limits.Work), cancel: cancel}
	ctx = context.WithValue(workCtx, retainedAllocationKey{}, working)
	transport := newRetainedBudget(limits.TransportBytes)
	client := newRetainedSourceClient(cfg, cluster, t, transport, ctx)
	st := &store{client: client, foreignIndexes: base.store.foreignIndexes}
	base.store.temp.mu.Lock()
	st.temp.id = base.store.temp.id
	st.temp.objects = maps.Clone(base.store.temp.objects)
	base.store.temp.mu.Unlock()
	r := &retainedExecution{capture: &retainedCapture{store: st, budget: newRetainedBudget(limits.SourceBytes), maxRows: limits.SourceRows, maxKeys: limits.CandidateKeys}, working: working, transport: transport,
		tables: map[string]*tableMeta{}, views: map[string]string{}, vetted: map[*SelectStmt]bool{}}

	e := *base
	e.store = st
	e.retained = r
	e.cache = nil
	e.pushdown = PushdownNone
	e.queryWorkers = 1
	close := func() { cancel(nil); stopTimer(); _ = client.Close() }
	return ctx, &e, r, close, nil
}
func (r *retainedExecution) table(ctx context.Context, schema, name string) (*tableMeta, error) {
	if isTempAlias(schema) || isTempSchema(schema) && !r.capture.store.ownTempSchema(schema) {
		return nil, errorf(adbc.StatusNotImplemented, "retained temporary sources must belong to the producing connection")
	}
	key := metaKey(schema, name)
	if m := r.tables[key]; m != nil {
		return m, nil
	}
	if err := r.working.reserve(1 << 20); err != nil {
		return nil, err
	} // bounded metadata decode allowance
	meta, err := r.capture.store.getTable(ctx, schema, name)
	if err != nil {
		return nil, err
	}
	if meta.Schema != schema || meta.Name != name {
		return nil, errorf(adbc.StatusInvalidData, "retained source metadata identity does not match its catalog key")
	}
	if meta.AppIndex {
		if err := r.capture.store.loadAppIndex(ctx, meta); err != nil {
			return nil, err
		}
	}
	for _, col := range meta.Columns {
		if err := retainedType(col.Type, 0); err != nil {
			return nil, err
		}
	}
	snapshot, err := r.capture.table(ctx, meta)
	if err != nil {
		return nil, err
	}
	r.tables[key] = snapshot
	return snapshot, nil
}
func retainedType(t ColType, depth int) error {
	if t.Alias != "" {
		return errorf(adbc.StatusNotImplemented, "catalog/OID aliases require separate retained admission")
	}
	if depth > 32 || t.Length > 64<<10 || t.Length < 0 || t.Kind == KindDecimal && (t.Precision < 1 || t.Precision > 76 || t.Scale < -76 || t.Scale > 76) {
		return codeStateErrorf(adbc.StatusInvalidState, "53200", "retained value type exceeds resource limits")
	}
	if t.Rec != nil {
		if len(t.Rec.fields) > 4096 {
			return codeStateErrorf(adbc.StatusInvalidState, "53200", "retained record has too many fields")
		}
		for _, field := range t.Rec.fields {
			if err := retainedType(field, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *retainedExecution) value(v Value) error {
	elements := 0
	var visit func(Value, int) error
	visit = func(v Value, depth int) error {
		elements++
		if depth > 32 || elements > retainedIPCRows || len(v.S) > 64<<10 {
			return r.working.reserve(-1)
		}
		if err := retainedType(v.T, depth); err != nil {
			_ = r.working.reserve(-1)
			return err
		}
		size := int64(512 + len(v.S)*8)
		if v.D != nil {
			size += int64(len(v.D.Bits())) * 16
		}
		if err := r.working.reserve(size); err != nil {
			return err
		}
		if !v.Null {
			for _, child := range v.fields() {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(v, 0)
}

// An explicit initial policy: stable, side-effect-free functions with bounded
// expansion. Functions that need additional argument-dependent admission (REPEAT,
// padding/replacement, regex/JSON generators and table functions) stay rejected.
var retainedFunctions = func() map[string]bool {
	names := strings.Fields(`COUNT SUM MIN MAX AVG BOOL_AND BOOL_OR EVERY BIT_AND BIT_OR BIT_XOR ARRAY_AGG STRING_AGG
 ABS CEIL CEILING FLOOR ROUND TRUNC SIGN MOD DIV POWER POW SQRT CBRT LN LOG LOG10 EXP PI
 SIN COS TAN COT ASIN ACOS ATAN ATAN2 SIND COSD TAND COTD ASIND ACOSD ATAND ATAN2D RADIANS DEGREES
 LOWER UPPER LENGTH CHAR_LENGTH CHARACTER_LENGTH OCTET_LENGTH BIT_LENGTH BTRIM LTRIM RTRIM REVERSE SUBSTR LEFT RIGHT
 POSITION STRPOS STARTS_WITH CONCAT CONCAT_WS COALESCE NULLIF GREATEST LEAST IIF
 CURRENT_TIMESTAMP CURRENT_DATE CURRENT_TIME LOCALTIMESTAMP LOCALTIME NOW
 CURRENT_DATABASE CURRENT_SCHEMA CURRENT_USER CURRENT_SETTING PG_TYPEOF VERSION
 ROW_NUMBER RANK DENSE_RANK PERCENT_RANK CUME_DIST NTILE LAG LEAD FIRST_VALUE LAST_VALUE NTH_VALUE
 GROUPING __ARRAY ARRAY_LENGTH CARDINALITY`)
	result := map[string]bool{}
	for _, name := range names {
		result[name] = true
	}
	return result
}()

func retainedCall(f *Func) error {
	if (f.Name == "BTRIM" || f.Name == "LTRIM" || f.Name == "RTRIM") && len(f.Args) > 1 {
		literal, ok := f.Args[1].(*Literal)
		if !ok || len(literal.V.S) > 256 {
			return errorf(adbc.StatusNotImplemented, "retained trim cutsets require a bounded constant")
		}
	}
	if (f.Name == "ROUND" || f.Name == "TRUNC") && len(f.Args) > 1 {
		literal, ok := f.Args[1].(*Literal)
		if !ok || literal.V.Null || !literal.V.T.Kind.isInteger() || literal.V.I < -76 || literal.V.I > 76 {
			return errorf(adbc.StatusNotImplemented, "retained numeric rounding precision must be a bounded integer literal")
		}
	}
	def, ok := callee(f)
	if !ok || def.volatile || !retainedFunctions[def.name] || len(f.Args) > 256 {
		return errorf(adbc.StatusNotImplemented, "function %s is not admitted for bounded retained execution", f.Name)
	}
	return nil
}
func (r *retainedExecution) vet(sel *SelectStmt, depth int) (err error) {
	if sel == nil {
		return nil
	}
	if r.vetted[sel] {
		return nil
	}
	if depth > 32 || len(sel.Items) > 1024 || len(sel.With) > 64 || len(sel.Joins) > 16 || len(sel.GroupingSets) > 64 {
		return errorf(adbc.StatusNotImplemented, "retained query shape exceeds coordinator limits")
	}
	r.vetted[sel] = true
	defer func() {
		if err != nil {
			delete(r.vetted, sel)
		}
	}()
	table := func(t *TableName) error {
		if t == nil {
			return nil
		}
		if isInfoSchema(t.Schema) || isPgCatalog(t.Schema) || t.Schema == "" && pgCatalogRelation(t.Name) {
			return errorf(adbc.StatusNotImplemented, "catalog relations are not retained source tables")
		}
		return nil
	}
	if err := table(sel.From); err != nil {
		return err
	}
	if sel.FromFunc != nil || sel.FromLateral {
		return errorf(adbc.StatusNotImplemented, "retained table functions and lateral FROM items require separate admission")
	}
	if err := r.vet(sel.FromSelect, depth+1); err != nil {
		return err
	}
	for _, cte := range sel.With {
		if cte.Recursive {
			return errorf(adbc.StatusNotImplemented, "recursive retained CTEs require separate admission")
		}
		if err := r.vet(cte.Select, depth+1); err != nil {
			return err
		}
	}
	if sel.SetOp != nil {
		if err := r.vet(sel.SetOp.Left, depth+1); err != nil {
			return err
		}
		if err := r.vet(sel.SetOp.Right, depth+1); err != nil {
			return err
		}
	}
	exprs := []Expr{sel.Where, sel.Having, sel.Qualify, sel.LimitExpr, sel.OffsetExpr}
	for _, row := range sel.Values {
		exprs = append(exprs, row...)
	}
	for _, item := range sel.Items {
		exprs = append(exprs, item.Expr)
	}
	for _, order := range sel.OrderBy {
		exprs = append(exprs, order.Expr)
	}
	exprs = append(exprs, sel.GroupBy...)
	exprs = append(exprs, sel.DistinctOn...)
	for _, window := range sel.Windows {
		exprs = append(exprs, windowSpecExprs(window.Spec)...)
	}
	for _, join := range sel.Joins {
		if join.Func != nil || join.Lateral {
			return errorf(adbc.StatusNotImplemented, "retained lateral/table-function joins require separate admission")
		}
		if err := table(join.Table); err != nil {
			return err
		}
		if err := r.vet(join.Select, depth+1); err != nil {
			return err
		}
		exprs = append(exprs, join.On)
	}
	for _, expr := range exprs {
		walkExprPruned(expr, func(x Expr) bool {
			if err != nil {
				return false
			}
			r.nodes++
			if r.nodes > 4096 {
				err = errorf(adbc.StatusNotImplemented, "retained query has too many expression nodes")
				return false
			}
			switch x := x.(type) {
			case *Func:
				err = retainedCall(x)
			case *WindowFunc:
				err = retainedCall(x.Func)
			case *Subquery:
				err = r.vet(x.Select, depth+1)
			case *Cast:
				err = retainedType(x.T, 0)
			case *Literal:
				err = r.value(x.V)
			}
			return err == nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *retainedExecution) view(v *viewMeta) error {
	if isTempSchema(v.Schema) && !r.capture.store.ownTempSchema(v.Schema) {
		return errorf(adbc.StatusNotImplemented, "retained temporary views must belong to the producing connection")
	}
	if len(v.SQL) > 32<<10 || len(v.Columns) > 1024 {
		return errorf(adbc.StatusNotImplemented, "retained view definition exceeds coordinator limits")
	}
	bytes, _ := json.Marshal(v)
	if err := r.working.reserve(int64(len(bytes))*4 + 4096); err != nil {
		return err
	}
	key := viewKey(v.Schema, v.Name)
	identity := retainedSHA(bytes)
	if prior, ok := r.views[key]; ok && prior != identity {
		return errorf(adbc.StatusInvalidState, "retained view definition changed during planning")
	}
	r.views[key] = identity
	return nil
}
func (r *retainedExecution) budgetError() error {
	resource := ""
	switch {
	case r.working.failed.Load():
		resource = "working_bytes or value shape"
	case r.capture != nil && r.capture.budget.failed.Load():
		resource = "source_bytes, source_rows or candidate_keys"
	case r.transport != nil && r.transport.failed.Load():
		resource = "transport_bytes"
	}
	if resource != "" {
		return codeStateErrorf(adbc.StatusInvalidState, "53200", "retained execution %s budget exhausted", resource)
	}
	return nil
}
func (r *retainedExecution) check(ctx context.Context) error {
	if err := r.budgetError(); err != nil {
		return err
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := r.capture.checkAll(ctx); err != nil {
		return err
	}
	for key, identity := range r.views {
		raw, err := r.capture.store.client.GetRange(ctx, key, 0, retainedRESPBulk).Bytes()
		if err != nil {
			return wrapRedis(err, "retained view definition unavailable")
		}
		var current viewMeta
		if len(raw) > retainedRESPBulk || json.Unmarshal(raw, &current) != nil {
			return errorf(adbc.StatusInvalidData, "invalid retained view metadata")
		}
		data, _ := json.Marshal(&current)
		if retainedSHA(data) != identity {
			return errorf(adbc.StatusInvalidState, "retained view definition changed during execution")
		}
	}
	return nil
}
func (e *executor) retainedMemTable(ctx context.Context, name string, cols []resultColumn, rows [][]Value, rename []string) (*tableMeta, error) {
	if err := retainedAllocate(ctx, int64(len(rows)), int64(len(cols))*512+128); err != nil {
		return nil, err
	}
	return memTable(name, cols, rows, rename)
}
