// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/apache/arrow-adbc/go/adbc"
)

const scalarCohortMembers = 32
const scalarCohortFields = 8

type scalarCohortLane struct {
	sq             *Subquery
	fn             *Func
	typ            ColType
	need           map[string]bool
	predicate      *scanPredicate
	input          string
	acc            *accumulator
	finalized      bool
	columns        []int // metadata-order decoded column slots
	predicateSlots []int
	inputSlot      int
	err            error
}
type scalarCohort struct {
	scope         *snapshotScope
	store         *store
	meta          *tableMeta
	descriptor    string
	lanes         []*scalarCohortLane
	columns       []columnMeta
	fields        []string
	started, done bool
	sourceErr     error
	scans         int // statement-local source-scan evidence; no global/cache telemetry
}

// Eligibility is metadata-only. In particular this must not validate a lease
// over the network, finalize an aggregate, or plan an unused CASE arm again.
func (e *executor) scalarCohortSource(ctx context.Context, m *tableMeta) bool {
	scope := snapshotOf(ctx)
	if e.planOnly || e.retained != nil || e.gpu != nil || e.partitionDirectTable != nil || e.partitionForeignTable != "" ||
		e.store == nil || !e.store.snapshots || !e.store.flex || scope == nil || scope.pin == nil || scope.store != e.store ||
		scope.operation || scope.partition || scope.write || scope.maintenance || scope.pin.shared != nil ||
		!managedRowIDTable(m) || !e.store.flexSnapshotTable(m) || !m.PlainIndex || m.Live || m.Unmanaged ||
		m.MaterializedSQL != "" || m.matviewRead != nil || m.retainedSource || m.view != nil || m.lazy != nil || m.group != nil || m.join != nil ||
		m.SnapshotRows == nil || len(m.PendingCleanup) != 0 {
		return false
	}
	if ctx.Value(subBudgetKey{}) != nil || ctx.Value(retainedAllocationKey{}) != nil || ctx.Value(snapshotDirectReadKey{}) != nil {
		return false
	}
	if validateSnapshotDescriptor(ctx, m) != nil {
		return false
	}
	for _, seg := range m.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			return false
		}
	}
	return true
}

func (e *executor) scalarCohortLane(ctx context.Context, sq *Subquery) *scalarCohortLane {
	if sq.Kind != SubqueryScalar || sq.correlated || sq.plan == nil {
		return nil
	}
	p := sq.plan
	s := p.sel
	if s == nil || !p.aggregate || !e.scalarCohortSource(ctx, p.meta) || len(p.items) != 1 || p.setop != nil || p.distinct || p.windowed() || p.grouping != nil || p.having != nil || p.qualify != nil || len(p.outerAggs) != 0 || len(p.extraNeed) != 0 ||
		s.From == nil || s.FromSelect != nil || s.FromFunc != nil || len(s.FromGroup) != 0 || len(s.With) != 0 || len(s.Joins) != 0 || len(s.GroupBy) != 0 || s.GroupingSets != nil || s.Having != nil || s.Qualify != nil || len(s.Windows) != 0 || s.Distinct || len(s.OrderBy) != 0 || s.Limit != nil || s.Offset != nil || s.LimitExpr != nil || s.OffsetExpr != nil {
		return nil
	}
	f, ok := p.items[0].expr.(*Func)
	if !ok || f.Distinct || f.Filter != nil || len(f.OrderBy) != 0 || f.WithinGroup || f.Nulls != NullsUnspecified || f.JSON != nil {
		return nil
	}
	lane := &scalarCohortLane{sq: sq, fn: f, need: map[string]bool{}}
	if f.Name == "COUNT" && f.Star && len(f.Args) == 0 {
	} else if f.Name == "AVG" && !f.Star && len(f.Args) == 1 {
		ref, ok := f.Args[0].(*ColumnRef)
		if !ok || ref.Outer != 0 || ref.item != "" || ref.outerAgg != nil || ref.whole != nil {
			return nil
		}
		cm, ok := p.meta.column(ref.Name)
		if !ok || !(cm.Type.Kind.isInteger() || cm.Type.Kind == KindDecimal) {
			return nil
		}
		lane.input = ref.Name
		lane.need[ref.Name] = true
	} else {
		return nil
	}
	var err error
	lane.typ, err = inferType(f, p.meta.types(), nil)
	if err != nil || lane.typ != p.items[0].typ {
		return nil
	}
	if s.Where != nil {
		lane.predicate = bindScanPredicate(s.Where, p.meta.types(), e.params, e.zone())
		if lane.predicate == nil {
			return nil
		}
		for _, name := range lane.predicate.fields {
			lane.need[name] = true
		}
	}
	// Only real user fields; system IDs/provenance cannot become predicate slots.
	for name := range lane.need {
		found := false
		for _, cm := range p.meta.Columns {
			if cm.Name == name {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return lane
}

// Register only existing bound sibling nodes in a successful parent's output
// expressions. Partition expressions already exclude subqueries. No execution
// starts until evalSubquery reaches subqueryRows for an actual selected node.
func (e *executor) registerScalarCohorts(ctx context.Context, p *selectPlan) {
	if e.planOnly || p == nil || p.sel == nil || p.aggregate || p.setop != nil || p.windowed() {
		return
	}
	groups := map[string]*scalarCohort{}
	seen := map[*Subquery]bool{}
	eligibleCount := 0
	tooMany := false
	for _, item := range p.items {
		walkExprPruned(item.expr, func(x Expr) bool {
			sq, ok := x.(*Subquery)
			if !ok {
				return true
			}
			if tooMany {
				return false
			}
			if seen[sq] {
				return false
			}
			seen[sq] = true
			lane := e.scalarCohortLane(ctx, sq)
			if lane == nil {
				return false
			}
			eligibleCount++
			if eligibleCount > scalarCohortMembers {
				tooMany = true
				return false
			}
			descriptor, err := json.Marshal(sq.plan.meta)
			if err != nil {
				return false
			}
			key := string(descriptor)
			c := groups[key]
			if c == nil {
				c = &scalarCohort{scope: snapshotOf(ctx), store: e.store, meta: sq.plan.meta, descriptor: key, fields: make([]string, 0)}
				groups[key] = c
			}
			c.lanes = append(c.lanes, lane)
			return false
		})
	}
	if tooMany {
		return
	}
	for _, c := range groups {
		if len(c.lanes) < 2 || len(c.lanes) > scalarCohortMembers {
			continue
		}
		need := map[string]bool{}
		for _, lane := range c.lanes {
			for name := range lane.need {
				need[name] = true
			}
		}
		if len(need) > scalarCohortFields {
			continue
		}
		for _, cm := range c.meta.Columns {
			if !need[cm.Name] {
				continue
			}
			c.columns = append(c.columns, cm)
			c.fields = append(c.fields, cm.field())
			if cm.MissingThrough > 0 {
				c.fields = append(c.fields, nullMarker(cm.field()))
			}
		}
		if !c.bindLanes() {
			continue
		}
		e.ensureCache()
		if e.cache.scalarCohorts == nil {
			e.cache.scalarCohorts = map[*Subquery]*scalarCohort{}
		}
		for _, lane := range c.lanes {
			e.cache.scalarCohorts[lane.sq] = c
		}
	}
}

// Complete every binding before publishing any lane. Failure keeps the whole
// candidate cohort on the established subquery execution path. Work here is
// bounded by the existing registration limits and never examines source rows.
func (c *scalarCohort) bindLanes() bool {
	slots := make(map[string]int, len(c.columns))
	for i, cm := range c.columns {
		if _, exists := slots[cm.Name]; exists {
			return false
		}
		slots[cm.Name] = i
	}
	type binding struct {
		columns, predicate []int
		input              int
	}
	bindings := make([]binding, len(c.lanes))
	for i, lane := range c.lanes {
		if lane == nil || lane.fn == nil {
			return false
		}
		b := &bindings[i]
		b.input = -1
		for at, cm := range c.columns {
			if lane.need[cm.Name] {
				b.columns = append(b.columns, at)
			}
		}
		if len(b.columns) != len(lane.need) {
			return false
		}
		if lane.predicate != nil {
			for _, name := range lane.predicate.fields {
				at, ok := slots[name]
				if !ok || !lane.need[name] {
					return false
				}
				b.predicate = append(b.predicate, at)
			}
		}
		if lane.fn.Name == "AVG" && !lane.fn.Star {
			at, ok := slots[lane.input]
			if !ok || !lane.need[lane.input] {
				return false
			}
			b.input = at
		} else if lane.fn.Name != "COUNT" || !lane.fn.Star || lane.input != "" {
			return false
		}
	}
	for i, lane := range c.lanes {
		lane.columns = bindings[i].columns
		lane.predicateSlots = bindings[i].predicate
		lane.inputSlot = bindings[i].input
	}
	return true
}

func (e *executor) scalarCohortRows(ctx context.Context, sq *Subquery) ([][]Value, bool, error) {
	c := e.cache.scalarCohorts[sq]
	if c == nil || c.scope != snapshotOf(ctx) || c.store != e.store || !e.scalarCohortSource(ctx, c.meta) {
		return nil, false, nil
	}
	descriptor, err := json.Marshal(c.meta)
	if err != nil || string(descriptor) != c.descriptor {
		return nil, false, nil
	}
	var active *scalarCohortLane
	for _, lane := range c.lanes {
		if lane.sq == sq {
			active = lane
			break
		}
	}
	if active == nil {
		return nil, false, nil
	}
	subqueryStats.runs.Add(1)
	if !c.started {
		c.started = true
		if e.diagnostics != nil && !e.diagnostics.metricsOnly {
			e.diagnostics.setPlan(map[string]any{"kind": "lazy_scalar_aggregate_cohort", "cohort_members": len(c.lanes), "physical_fields": len(c.columns), "source": "exact_snapshot_directory"})
		}
		for _, lane := range c.lanes {
			lane.acc = newAccumulator(lane.fn)
		}
		c.sourceErr = c.scan(ctx, active)
		c.done = true
	}
	if c.sourceErr != nil {
		return nil, true, c.sourceErr
	}
	if !c.done {
		return nil, true, errorf(adbc.StatusInternal, "recursive scalar aggregate cohort execution")
	}
	if err := c.store.checkKeys(ctx, c.meta, false); err != nil {
		return nil, true, err
	}
	if active.err != nil {
		return nil, true, active.err
	}
	// Finalize only the actual requested member, with precisely the ordinary
	// aggregate result type and output expression evaluation path.
	env := e.newEnv(ctx, c.meta.types(), e.params)
	v, err := active.acc.result(env, active.typ)
	if err != nil {
		return nil, true, invalidArg(err)
	}
	env.aggs = map[*Func]Value{active.fn: v}
	row, err := evalItems(env, sq.plan.items)
	if err != nil {
		return nil, true, err
	}
	active.finalized = true
	return [][]Value{row}, true, nil
}

type scalarCohortDecoded struct {
	value Value
	err   error
	set   bool
}

// Preserve each lane's metadata decode order, including fields of rejected
// rows. A row's shared decode cache is ephemeral and never retains input rows.
func (c *scalarCohort) input(raw aggRow, lane *scalarCohortLane, decoded []scalarCohortDecoded) error {
	for _, i := range lane.columns {
		cm := c.columns[i]
		slot := &decoded[i]
		if !slot.set {
			slot.set = true
			if text, ok := raw[cm.field()]; ok {
				slot.value, slot.err = cm.decode(text)
			} else {
				slot.value, slot.err = cm.missingValue(raw)
			}
			if slot.err != nil {
				slot.err = errorf(adbc.StatusInternal, "column %q: %v", cm.Name, slot.err)
			}
		}
		if slot.err != nil {
			return slot.err
		}
	}
	if lane.predicate != nil {
		for i, slot := range lane.predicateSlots {
			lane.predicate.values[i] = decoded[slot].value
		}
		match, err := lane.predicate.matches()
		if err != nil {
			return invalidArg(err)
		}
		if !match {
			return nil
		}
	}
	if lane.fn.Star {
		lane.acc.add(Value{})
		return nil
	}
	lane.acc.add(decoded[lane.inputSlot].value)
	return nil
}

func (c *scalarCohort) scan(ctx context.Context, active *scalarCohortLane) error {
	c.scans++
	d := newSnapshotDeltaScan(c.store, c.meta, c.fields)
	decoded := make([]scalarCohortDecoded, len(c.columns))
	for {
		if err := c.store.checkKeys(ctx, c.meta, false); err != nil {
			return err
		}
		_, rows, err := d.next(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := c.store.checkKeys(ctx, c.meta, false); err != nil {
			return err
		}
		for _, raw := range rows {
			if err := cancelled(ctx); err != nil {
				return err
			}
			clear(decoded)
			// The actually invoked member determines serial first-error behavior.
			if err := c.input(raw, active, decoded); err != nil {
				active.err = err
				return err
			}
			for _, lane := range c.lanes {
				if lane == active || lane.err != nil {
					continue
				}
				if err := c.input(raw, lane, decoded); err != nil {
					lane.err = err
				}
			}
		}
	}
}
