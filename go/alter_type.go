// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// ALTER TABLE … ALTER [COLUMN] c [SET DATA] TYPE t [USING expr].
//
// As in Postgres 16 (ATPrepAlterColumnType and ATExecAlterColumnType in
// src/backend/commands/tablecmds.c):
//
//   - Conversion: without USING, the column's value is converted as an
//     assignment converts it (INSERT's conversion), which needs an implicit
//     or assignment cast in Postgres (pg_cast, or I/O conversion to a
//     string type): see assignable. Otherwise it fails with `column "c"
//     cannot be cast automatically to type integer`. USING converts an
//     expression over the row instead, whose result must be assignable to
//     the new type in turn. The expression and the conversion read the table
//     as it was before the statement: a column the statement drops, and the
//     old value of a column whose type it changes too.
//   - The column keeps its name, NOT NULL, primary key, DEFAULT (which must
//     be assignable to the new type, and is checked as a new DEFAULT is),
//     comment and field. The CHECK constraints that read it are bound again
//     for the new type (step AT_PASS_OLD_CONSTR, after every ALTER COLUMN …
//     TYPE of the statement), and the converted rows must pass them.
//   - A column that a view reads can't change its type (views keep their
//     SQL text and the column types they had when they were created).
//   - ALTER COLUMN … TYPE runs in AT_PASS_ALTER_TYPE, after the drops and
//     before ADD COLUMN; a column can't change its type twice in one
//     statement.
//
// When the values stay as they are stored (the same type without USING, or
// a VARCHAR that gets longer or loses its length), only the metadata
// changes, as for the other actions. Otherwise every row is rewritten, as
// in Postgres, and the statement runs as a rewrite (rewriteTable) on the
// machinery of a re-keying rename (rekey.go): the rows are copied, converted,
// to a new key prefix and index of the same table name, which the metadata
// then switches to in one transaction. So the old rows, index and metadata
// stay as they were until the switch, and an ALTER that fails (a value that
// doesn't convert, a row that fails a constraint, a cancel, a lost
// connection) is rolled back by dropping the new index with its copies. In
// steps:
//
//  1. Begin (beginMove): one transaction checks that the metadata is still
//     the table the change was planned for, reserves new names, sets
//     rekey_to (and rewriting), which refuses writes and other ALTERs, and
//     records the job with its lease, as a re-key does.
//  2. Copy: the new index is created, with every column's new attribute
//     (TAG for strings, NUMERIC for numbers, dates and times, none for the
//     others), and after rekeyFence, every row the old index lists is read
//     (HGETALL, pipelined), converted, checked against NOT NULL and the
//     CHECKs (also the statement's new ones), and written to its new key
//     (HSET, pipelined). The copies hold the table's columns only: a dropped
//     column's field isn't copied, and an added column's DEFAULT (or an
//     altered column's missing value) is written into every row.
//  3. Switch: one transaction makes the metadata the table after the ALTER,
//     on the new names, if it is otherwise as it was when the rewrite began.
//  4. Finish: the old index is dropped with its rows (DD), and the old names
//     released, as after a re-keying rename.
//
// Concurrent statements are those of a re-key: writers that read the
// metadata after step 1 are refused ("is being altered"), and a writer
// that read it before then either finished within rekeyFence of reading it,
// and so before the copy began (its rows are copied), or checks afterwards
// and fails (checkWritten finds the job, or the old prefix released). No
// write reaches the new rows before the switch, and after it, a statement
// with the old metadata writes nothing (its keys moved, see sameKeys) or
// fails. Readers see the old rows until the switch and the new ones after
// it; one that read across the switch fails (checkReads). Recovery, cancel
// and Close are a re-key's: until the switch, the rewrite is rolled back.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// alteredColumn is a column whose type ALTER COLUMN … TYPE changes.
type alteredColumn struct {
	name, field string
	from, to    ColType
	// using is the USING expression, bound over the table as it was before
	// the statement; nil converts the column's own value. text is as
	// written.
	using Expr
	text  string
}

// alterType applies ALTER COLUMN … TYPE to m; orig is the table before the
// statement, whose values are converted.
func (e *executor) alterType(ctx context.Context, m, orig *tableMeta, cmd AlterCmd, in *alterInputs, res *alterResult) error {
	// The application's HASHes can't be rewritten under new keys.
	if err := refuseAdopted(m, "ALTER COLUMN … TYPE"); err != nil {
		return err
	}
	i, ok := m.resolve(cmd.Column)
	if !ok {
		return columnMissing(cmd.Column, m.Name)
	}
	col := &m.Columns[i]
	to := cmd.Def.Type
	// Drops come first, and ADD COLUMN later, so orig has the column.
	j, ok := orig.resolve(col.Name)
	if !ok {
		return columnMissing(cmd.Column, m.Name)
	}
	src := orig.Columns[j]
	var using Expr
	if cmd.Using != nil {
		x, err := e.bindUsing(ctx, orig, cmd.Using)
		if err != nil {
			return err
		}
		t, err := inferType(x, orig.types(), nil)
		if err != nil {
			return invalidArg(err)
		}
		if !unknownOrNull(x) && !assignable(t, to) {
			return stateErrorf(stateDatatypeMismatch, "result of USING clause for column %q cannot be cast automatically to type %s; you might need to add an explicit cast",
				col.Name, pgTypeName(to, false))
		}
		using = x
	} else if !assignable(src.Type, to) {
		return stateErrorf(stateDatatypeMismatch, `column %q cannot be cast automatically to type %s; you might need to specify "USING %s::%s"`,
			col.Name, pgTypeName(to, false), col.Name, pgTypeName(to, true))
	}
	// As Postgres: a second ALTER COLUMN … TYPE of the column fails if the
	// first changed its type, and otherwise replaces it.
	if k := slices.IndexFunc(res.altered, func(a *alteredColumn) bool { return a.field == col.field() }); k >= 0 {
		if a := res.altered[k]; a.from != a.to {
			return stateErrorf(stateFeatureNotSupported, "cannot alter type of column %q twice", col.Name)
		}
		res.altered = slices.Delete(res.altered, k, k+1)
	}
	if col.Default != "" {
		if err := e.alterTypeDefault(ctx, *col, to); err != nil {
			return err
		}
	}
	if d, ok := in.views[strings.ToLower(col.Name)]; ok {
		return stateErrorf(stateFeatureNotSupported, "cannot alter type of a column used by a view or rule; %s", d)
	}
	was := col.Type
	col.Type = to
	switch {
	case !indexable(to):
		col.Indexed = false
	case !col.Indexed && !indexable(was):
		// A BYTEA or INTERVAL column that becomes indexable. One that was
		// indexable and isn't indexed was declared NOINDEX, or came after
		// maxIndexed others.
		n := 0
		for _, c := range m.Columns {
			if c.Indexed {
				n++
			}
		}
		col.Indexed = simpleName(col.field()) && n < maxIndexed
	}
	if using != nil || !keepsValues(src.Type, to) {
		res.rewrite = true
		// The rewrite records them for the new values (see rewrite.next).
		col.TagValues, col.TagsChecked, col.NaNs = "", false, false
		col.initTags()
	}
	res.altered = append(res.altered, &alteredColumn{name: col.Name, field: col.field(), from: src.Type, to: to, using: using, text: cmd.UsingText})
	res.log("type %q field %q: %s to %s using %q indexed %v", col.Name, col.field(), src.Type.SQLName(), to.SQLName(), cmd.UsingText, col.Indexed)
	return nil
}

// alterTypeDefault checks that the DEFAULT of a column whose type becomes
// to converts, as Postgres does: the expression's type must be assignable to
// to (a string literal has the column's type, as Postgres stores it), and
// the value is checked as for a new DEFAULT (see checkDefault).
func (e *executor) alterTypeDefault(ctx context.Context, col columnMeta, to ColType) error {
	x, err := parseExprText(col.Default)
	if err != nil {
		return errorf(adbc.StatusInternal, "invalid DEFAULT %q of column %q: %v", col.Default, col.Name, err)
	}
	t := col.Type
	if !unknownOrNull(x) {
		if it, err := inferType(x, nil, nil); err == nil {
			t = it
		}
	}
	if !assignable(t, to) {
		return stateErrorf(stateDatatypeMismatch, "default for column %q cannot be cast automatically to type %s", col.Name, pgTypeName(to, false))
	}
	_, _, err = e.checkDefault(ctx, ColumnDef{Name: col.Name, Type: to, Default: x, DefaultText: col.Default})
	return err
}

// alterOldChecks binds the CHECK constraints that read a column whose type
// changed again, for the new types, as Postgres adds them again: one that
// no longer works (`function length(integer) does not exist`) fails the
// statement. The rewritten rows must pass them.
func (e *executor) alterOldChecks(ctx context.Context, m *tableMeta, res *alterResult) error {
	for _, k := range m.Checks {
		if !slices.ContainsFunc(res.altered, func(a *alteredColumn) bool { return checkReads(k.Expr, a.name) }) {
			continue
		}
		x, err := parseExprText(k.Expr)
		if err != nil {
			return errorf(adbc.StatusInvalidState, "check constraint %q of relation %q can't be read (%s); drop it with ALTER TABLE … DROP CONSTRAINT",
				k.Name, m.Name, errText(err))
		}
		if _, err := e.checkCheck(ctx, m, x); err != nil {
			return err
		}
		res.rechecked = append(res.rechecked, k)
		res.log("check %q again", k.Name)
	}
	return nil
}

// bindUsing checks and binds a USING expression over the table before the
// statement, as Postgres's transform expressions: no subqueries,
// parameters, aggregates, window or set-returning functions.
func (e *executor) bindUsing(ctx context.Context, orig *tableMeta, x Expr) (Expr, error) {
	var err error
	walkExpr(x, func(n Expr) {
		if err != nil {
			return
		}
		switch v := n.(type) {
		case *Subquery:
			err = stateErrorf(stateFeatureNotSupported, "cannot use subquery in transform expression")
		case *Param:
			err = stateErrorf(stateUndefinedParameter, "there is no parameter $%d", v.Index+1)
		case *WindowFunc:
			err = stateErrorf(stateWindowing, "window functions are not allowed in transform expressions")
		case *Func:
			d, ok := lookupFunc(v.Name)
			switch {
			case aggregateFuncs[v.Name]:
				err = stateErrorf(stateGrouping, "aggregate functions are not allowed in transform expressions")
			case v.Name == "GROUPING":
				err = stateErrorf(stateGrouping, "grouping operations are not allowed in transform expressions")
			case ok && d.kind == tableKind:
				err = stateErrorf(stateFeatureNotSupported, "set-returning functions are not allowed in transform expressions")
			}
		}
	})
	if err != nil {
		return nil, err
	}
	scopes, sq := e.scopes, e.pendingSq
	e.scopes, e.pendingSq = nil, nil
	defer func() { e.scopes, e.pendingSq = scopes, sq }()
	if _, err := e.bindIn(ctx, x, orig, "", "transform expressions"); err != nil {
		return nil, err
	}
	return x, nil
}

// unknownOrNull reports whether x is a quoted string literal or NULL, which
// Postgres types as unknown and converts to any type (see unknownLiteral).
func unknownOrNull(x Expr) bool {
	l, ok := x.(*Literal)
	return ok && (l.V.Null || l.Unknown)
}

// assignable reports whether Postgres 16 converts values of type from to
// type to in an assignment: with an implicit or assignment cast of
// pg_cast, or with I/O conversion to a string type. As ALTER COLUMN … TYPE
// finds on postgres:16 for every pair of the driver's types:
//
//	boolean            to boolean and strings
//	numeric types      to numeric types and strings
//	strings            to strings
//	bytea              to bytea and strings
//	date               to date, timestamp(tz) and strings
//	time               to time, timetz, interval and strings
//	timetz             to time, timetz and strings
//	timestamp          to date, time, timestamp(tz) and strings
//	timestamptz        to date, time, timetz, timestamp(tz) and strings
//	interval           to time, interval and strings
func assignable(from, to ColType) bool {
	f, t := from.Kind, to.Kind
	switch {
	case f == KindNull || t == KindString:
		return true
	case f == KindString:
		return false
	case f.isNumeric():
		return t.isNumeric()
	case f == KindDate:
		return t == KindDate || t == KindTimestamp
	case f == KindTime:
		return t == KindTime || t == KindTimeTZ || t == KindInterval
	case f == KindTimeTZ:
		return t == KindTime || t == KindTimeTZ
	case f == KindTimestamp:
		return t == KindDate || t == KindTime || t == KindTimestamp || t == KindTimeTZ && from.TZ != ""
	case f == KindInterval:
		return t == KindTime || t == KindInterval
	}
	return f == t // boolean, bytea
}

// keepsValues reports whether the stored values of type from are those of
// type to, unchanged, so that ALTER COLUMN … TYPE (without USING) only
// changes the metadata: the same type, or a VARCHAR that gets a longer
// length or none (TEXT). Postgres doesn't rewrite the table for these
// either.
func keepsValues(from, to ColType) bool {
	if from == to {
		return true
	}
	return from.Kind == KindString && to.Kind == KindString && !from.Fixed && !to.Fixed &&
		(to.Length == 0 || from.Length > 0 && to.Length >= from.Length)
}

// pgTypeName is Postgres's name of a type in messages (format_type_be), or
// with typmod, with its type modifier (format_type_with_typemod). A time or
// timestamp of microseconds has none, as one declared without a precision.
func pgTypeName(t ColType, typmod bool) string {
	switch t.Kind {
	case KindBool:
		return "boolean"
	case KindInt16:
		return "smallint"
	case KindInt32:
		return "integer"
	case KindInt64:
		return "bigint"
	case KindFloat32:
		return "real"
	case KindFloat64:
		return "double precision"
	case KindDecimal:
		if typmod {
			return fmt.Sprintf("numeric(%d,%d)", t.Precision, t.Scale)
		}
		return "numeric"
	case KindString:
		switch {
		case t.Fixed && t.Length > 0 && typmod:
			return fmt.Sprintf("character(%d)", t.Length)
		case t.Fixed && typmod:
			return "bpchar"
		case t.Fixed:
			return "character"
		case t.Length > 0 && typmod:
			return fmt.Sprintf("character varying(%d)", t.Length)
		case t.Length > 0:
			return "character varying"
		}
		return "text"
	case KindBinary:
		return "bytea"
	case KindDate:
		return "date"
	case KindTime, KindTimeTZ, KindTimestamp:
		name := "time"
		if t.Kind == KindTimestamp {
			name = "timestamp"
		}
		if d := t.fracDigits(); typmod && d != 6 {
			name += fmt.Sprintf("(%d)", d)
		}
		if t.TZ != "" || t.Kind == KindTimeTZ {
			return name + " with time zone"
		}
		return name + " without time zone"
	case KindInterval:
		return "interval"
	}
	return strings.ToLower(t.SQLName())
}

// assignValue converts a value for a column of type to as an assignment
// does (INSERT's conversion): a string too long for the type is an error,
// unless the rest is spaces (see fitLength).
func assignValue(v Value, to ColType, z tzZone) (Value, error) {
	cv, err := coerceIn(v, to, z)
	if err != nil {
		return Value{}, invalidArg(err)
	}
	return fitLength(to, cv)
}

// ---- views ----

// viewDependencies records in in.views, for the columns whose type ALTER
// COLUMN … TYPE changes, a view that reads each one, as Postgres refuses:
// `cannot alter type of a column used by a view or rule`, with the detail
// `rule _RETURN on view v depends on column "c"`. The views checked are the
// permanent ones of every schema and the connection's temporary ones (other
// connections' can't be read), whose SQL text names the table; each one is
// planned, as CREATE VIEW plans it, and the columns of the table that its
// query reads (and not through another view, which is that view's) are
// those it depends on, with the ones * selects. Dependencies aren't
// recorded, so this is a scan of the views.
func (e *executor) viewDependencies(ctx context.Context, meta *tableMeta, cmds []AlterCmd, in *alterInputs) error {
	cols := map[string]bool{}
	for _, c := range cmds {
		if c.Action != AlterColumnType {
			continue
		}
		if i, ok := meta.resolve(c.Column); ok {
			cols[strings.ToLower(meta.Columns[i].Name)] = true
		}
	}
	if len(cols) == 0 {
		return nil
	}
	schemas, err := e.store.listSchemas(ctx)
	if err != nil {
		return err
	}
	if s := e.store.tempSchema(); s != tempAlias {
		schemas = append(schemas, s)
	}
	name := strings.ToLower(meta.Name)
	for _, schema := range schemas {
		views, err := e.store.listViews(ctx, schema)
		if err != nil {
			return err
		}
		for _, vn := range views {
			v, err := e.store.getView(ctx, schema, vn)
			if err != nil {
				var ae adbc.Error
				if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
					continue // dropped meanwhile
				}
				return err
			}
			if !strings.Contains(strings.ToLower(v.SQL), name) {
				continue
			}
			for _, c := range e.viewReads(ctx, v, meta) {
				lc := strings.ToLower(c)
				if cols[lc] && in.views[lc] == "" {
					if in.views == nil {
						in.views = map[string]string{}
					}
					title := v.Name
					if schema != defaultSchema && !isTempSchema(schema) {
						title = v.Schema + "." + v.Name
					}
					in.views[lc] = fmt.Sprintf("rule _RETURN on view %s depends on column %q", title, c)
				}
			}
		}
	}
	return nil
}

// viewReads returns the columns of meta that a view's query reads itself,
// found by planning it (nothing if it no longer plans).
func (e *executor) viewReads(ctx context.Context, v *viewMeta, meta *tableMeta) []string {
	parsed, err := ParseScript(v.SQL)
	if err != nil || len(parsed) != 1 {
		return nil
	}
	sel, ok := parsed[0].Stmt.(*SelectStmt)
	if !ok {
		return nil
	}
	home, temp := v.Schema, isTempSchema(v.Schema)
	if temp && v.DefaultSchema != "" {
		home = v.DefaultSchema
	}
	// A cache of its own: the tables it loads aren't the statement's reads
	// (see checkReads).
	saved := e.cache
	defer func() { e.cache = saved }()
	e.cache = newExecCache()
	var out []string
	e.cache.colReads = func(m *tableMeta, column string) {
		if m.Schema == meta.Schema && m.Name == meta.Name && !slices.Contains(out, column) {
			out = append(out, column)
		}
	}
	e.cache.materializing["view:"+v.Schema+"."+v.Name] = true
	defer e.planningOnly(true)()
	_ = e.isolated(home, temp, func() error {
		_, err := e.planSelect(ctx, sel, nil)
		return err
	})
	return out
}

// ---- the rewrite ----

// testHookRewriteStep, when set by a test, runs at two steps of a rewrite:
// "copy" before each page of rows is copied, and "switched" once the
// metadata names the new keys. ctx is the rewrite's, which a cancel or
// Close cancels.
var testHookRewriteStep func(ctx context.Context, step string)

var (
	// errLostRewrite means that this connection may no longer own the
	// rewrite: it could not renew the lease in time.
	errLostRewrite = errorf(adbc.StatusIO, "ALTER TABLE stopped: its lease on the rewrite of the table's rows could not be renewed in time, so another connection may take it over; the change is rolled back")
	// errRewriteClosed is the error of a rewrite that Close stopped.
	errRewriteClosed = errorf(adbc.StatusInvalidState, "the connection was closed while ALTER COLUMN … TYPE was rewriting the table's rows; the change was rolled back")
)

// rewriteTable runs an ALTER TABLE whose ALTER COLUMN … TYPE converts
// values, with all its actions, as a rewrite of every row to new keys (see
// the top of this file). planned is applyAlter's result on meta.
func (e *executor) rewriteTable(ctx context.Context, meta *tableMeta, cmds []AlterCmd, in *alterInputs, planned *alterResult) error {
	s := e.store
	run, err := s.trackRekey(ctx)
	if err != nil {
		return err
	}
	defer s.untrackRekey(run)
	// Closing the connection stops the rewrite as cancelling it does.
	sctx := run.ctx
	// Leftovers of abandoned re-keys and rewrites (one may hold the names).
	_ = s.recoverDead(sctx)
	rw := &rewrite{}
	job, start, err := s.beginMove(sctx, meta.Schema, meta.Name, meta.Name, func(cur *tableMeta) error {
		changed := errorf(adbc.StatusIO, "table %q changed concurrently; try again", cur.Name)
		// TRUNCATE moves a table to new keys.
		if cur.index() != meta.index() || cur.prefix() != meta.prefix() {
			return changed
		}
		target := cloneMeta(cur)
		res, err := e.applyAlter(sctx, target, cmds, in)
		if err != nil {
			return err
		}
		if !slices.Equal(res.trace, planned.trace) {
			return changed
		}
		rw.target, rw.res = target, res
		return nil
	})
	if err != nil {
		if run.closing() {
			return errRewriteClosed
		}
		return err
	}
	s.holdRekey(run, job.lease)
	defer job.lease.release()
	fence := time.Now().Add(rekeyFence)
	rw.start = start
	rw.target.KeyPrefix, rw.target.IndexName, rw.target.PrefixGen = job.KeyPrefix, job.IndexName, job.KeyPrefixGen
	rw.target.RekeyTo, rw.target.Rewriting = "", false
	// Cancelling the statement stops the copy, but not the rollback.
	mctx, cancel := context.WithCancel(job.lease.ctx)
	defer cancel()
	defer context.AfterFunc(sctx, cancel)()
	err = e.copyRewritten(mctx, job, rw, fence)
	if err == nil {
		err = e.switchRewrite(mctx, job, rw)
	}
	if err == nil && testHookRewriteStep != nil {
		testHookRewriteStep(mctx, "switched")
	}
	bg := context.WithoutCancel(ctx)
	if err != nil {
		switch {
		case run.closing():
			err = errRewriteClosed
		case job.lease.ctx.Err() != nil:
			err = errLostRewrite
		case ctx.Err() != nil:
			err = cancelled(ctx)
		}
		// Roll back (or, if the switch did happen, finish). If that fails
		// too, another connection does it once the lease has expired.
		switched, serr := s.settleRekey(job.lease.ctx, job)
		if serr != nil {
			s.client.Del(bg, rekeyAliveKey(job.Owner))
		}
		if !switched {
			return err
		}
	} else if err := s.finishRekey(job.lease.ctx, job); err != nil {
		// The table is altered; another connection removes the old rows.
		s.client.Del(bg, rekeyAliveKey(job.Owner))
	}
	return nil
}

// rewrite is the conversion of a table's rows by a rewrite.
type rewrite struct {
	// start is the table as the rewrite began, whose rows are read; target
	// the table as the ALTER leaves it, on the new keys; res what the
	// actions did (applied to start).
	start, target *tableMeta
	res           *alterResult
	env           *evalEnv // evaluates the USING expressions
	// checks are the CHECKs the new rows must pass: those that read an
	// altered column, and the new ones; notNull the columns of target whose
	// values are new and may not be NULL.
	checks  *tableChecks
	notNull []int
	// altered and added are the altered and added columns by field; added
	// holds the value the rows get.
	altered map[string]*alteredColumn
	added   map[string]addedColumn
	values  map[string]Value
	// levels and nans are what the altered columns' new values need
	// recorded (see tags.go and nan.go).
	levels map[string]string
	nans   map[string]bool
}

// prepare binds what the conversion of each row needs.
func (rw *rewrite) prepare(ctx context.Context, e *executor) error {
	rw.env = e.newEnv(ctx, rw.start.types(), nil)
	m := *rw.target
	m.Checks = append(slices.Clone(rw.res.rechecked), rw.res.checks...)
	tc, err := e.tableChecks(ctx, &m)
	if err != nil {
		return err
	}
	rw.checks = tc
	if rw.values, err = rw.res.addedValues(); err != nil {
		return err
	}
	rw.altered, rw.added = map[string]*alteredColumn{}, map[string]addedColumn{}
	for _, a := range rw.res.altered {
		rw.altered[a.field] = a
	}
	for _, a := range rw.res.added {
		rw.added[a.field()] = a
	}
	for i, c := range rw.target.Columns {
		_, alt := rw.altered[c.field()]
		if !c.Nullable && (alt || slices.Contains(rw.res.notNull, c.field())) {
			rw.notNull = append(rw.notNull, i)
		}
	}
	rw.levels, rw.nans = map[string]string{}, map[string]bool{}
	return nil
}

// convert returns the fields of a row (as read) rewritten for the target
// table, once the row passes its constraints.
func (rw *rewrite) convert(raw aggRow) ([]any, error) {
	old, err := decodeRow(rw.start, raw, nil)
	if err != nil {
		return nil, err
	}
	vals := make(map[string]Value, len(rw.altered))
	for _, a := range rw.res.altered {
		v := old[a.name]
		if a.using != nil {
			rw.env.row = old
			if v, err = rw.env.eval(a.using); err != nil {
				return nil, invalidArg(err)
			}
		}
		if v, err = assignValue(v, a.to, rw.env.zone()); err != nil {
			return nil, err
		}
		vals[a.field] = v
	}
	row := make(map[string]Value, len(rw.target.Columns))
	for _, c := range rw.target.Columns {
		if v, ok := vals[c.field()]; ok {
			row[c.Name] = v
		} else if _, ok := rw.added[c.field()]; ok {
			row[c.Name] = rw.values[c.Name]
		} else {
			row[c.Name] = old[c.Name]
		}
	}
	// As Postgres checks a rewritten row: NOT NULL in column order, then
	// the constraints.
	for _, i := range rw.notNull {
		if c := rw.target.Columns[i]; row[c.Name].Null {
			return nil, stateErrorf(stateNotNull, "column %q of relation %q contains null values", c.Name, rw.target.Name)
		}
	}
	if rw.checks != nil {
		i, err := rw.checks.violated(row)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			return nil, stateErrorf(stateCheck, "check constraint %q of relation %q is violated by some row", rw.checks.meta.Checks[i].Name, rw.target.Name)
		}
	}
	fields := make([]any, 0, 2+2*len(raw))
	fields = append(fields, rowIDField, raw[rowIDField])
	for _, c := range rw.target.Columns {
		f := c.field()
		if v, ok := vals[f]; ok {
			if !v.Null {
				s := encodeStored(v)
				fields = append(fields, f, s)
				rw.note(c, v, s)
			}
			continue
		}
		if a, ok := rw.added[f]; ok {
			if a.missing {
				fields = append(fields, f, a.Missing)
			}
			continue
		}
		if s, ok := raw[f]; ok {
			fields = append(fields, f, s)
		}
		if c.MissingThrough > 0 {
			if s, ok := raw[nullMarker(f)]; ok {
				fields = append(fields, nullMarker(f), s)
			}
		}
	}
	return fields, nil
}

// note records what an altered column's new value s (v, encoded) needs.
func (rw *rewrite) note(c columnMeta, v Value, s string) {
	switch {
	case !c.Indexed:
	case c.Type.Kind == KindString:
		if l := tagLevelOf(s); tagRank(l) > tagRank(rw.levels[c.field()]) {
			rw.levels[c.field()] = l
		}
	case c.Type.Kind.isFloat() && math.IsNaN(v.F):
		rw.nans[c.field()] = true
	}
}

// next returns the table's metadata after the switch: target, with what
// writes and background tasks recorded in cur meanwhile for the columns
// whose values were copied (string levels, NaNs, string checks). The new
// rows have no dropped columns' fields to clean up, and their altered and
// added columns' values are written, so they read no missing values.
func (rw *rewrite) next(cur *tableMeta) *tableMeta {
	next := cloneMeta(rw.target)
	next.PendingCleanup = nil
	for i := range next.Columns {
		c := &next.Columns[i]
		f := c.field()
		_, alt := rw.altered[f]
		_, add := rw.added[f]
		switch {
		case alt:
			c.Missing, c.MissingThrough = "", 0
			c.TagValues, c.TagsChecked = "", false
			c.initTags()
			if c.TagsChecked {
				c.TagValues = rw.levels[f]
			}
			c.NaNs = c.Indexed && c.Type.Kind.isFloat() && rw.nans[f]
		case add:
			c.Missing, c.MissingThrough = "", 0
		default:
			if j := slices.IndexFunc(cur.Columns, func(k columnMeta) bool { return k.field() == f }); j >= 0 {
				k := cur.Columns[j]
				c.TagValues, c.TagsChecked, c.NaNs = k.TagValues, k.TagsChecked, k.NaNs
			}
		}
	}
	return next
}

// sameDefinition reports whether a table's metadata cur is start's, apart
// from what writes and background tasks change: the levels and NaNs that
// writes record (tags.go, nan.go), the string checks, the dropped columns'
// cleanup, and the move's rekey_to.
func sameDefinition(start, cur *tableMeta) bool {
	norm := func(m *tableMeta) string {
		c := cloneMeta(m)
		c.RekeyTo, c.Rewriting, c.PendingCleanup = "", false, nil
		for i := range c.Columns {
			c.Columns[i].TagValues, c.Columns[i].TagsChecked, c.Columns[i].NaNs = "", false, false
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	a := norm(start)
	return a != "" && a == norm(cur)
}

// copyRewritten is step 2: it creates the new index and, once the writes
// that may have missed rekey_to are done (at fence), converts and copies
// every row.
func (e *executor) copyRewritten(ctx context.Context, job *rekeyJob, rw *rewrite, fence time.Time) error {
	s := e.store
	if err := job.mayAct(); err != nil {
		return err
	}
	// The index name was free in the registry, so an index with that name
	// is left over from an interrupted CREATE/DROP (see foreignIndex).
	_ = s.searchDo(ctx, job.IndexName, "FT.DROPINDEX", job.IndexName, "DD").Err()
	if err := s.searchDo(ctx, job.IndexName, indexCreateArgs(rw.target)...).Err(); err != nil {
		return wrapRedis(err, "failed to create the new search index")
	}
	if err := rw.prepare(ctx, e); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return wrapRedis(ctx.Err(), "ALTER TABLE stopped")
	case <-time.After(time.Until(fence)):
	}
	// Background dropped-column cleanup can reindex old HASHes while a
	// cursor is open. RediSearch then replaces document ids, and that cursor
	// can omit rows. Writers are fenced out, so the source count is stable;
	// verify the copies before switching metadata, and rescan missing rows.
	want, err := s.countMatches(ctx, job.OldIndex, "*")
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := e.copyRewrittenPageScan(ctx, job, rw, attempt > 0); err != nil {
			return err
		}
		// DROP TABLE may have removed both indexes during the scan. Preserve
		// its table-not-found error instead of probing a now-missing index.
		if _, err := s.getTable(ctx, job.Schema, job.From); err != nil {
			return err
		}
		copied, err := s.countMatches(ctx, job.IndexName, "*")
		if err != nil {
			return err
		}
		if copied == want {
			return nil
		}
	}
	return errorf(adbc.StatusIO, "ALTER TABLE: the source index changed during the rewrite and not every row could be copied; try again")
}

// copyRewrittenPageScan reads every cursor page of the old index. A retry
// skips existing copies so USING expressions are evaluated once per row,
// even when they contain volatile functions.
func (e *executor) copyRewrittenPageScan(ctx context.Context, job *rekeyJob, rw *rewrite, retry bool) error {
	s := e.store
	node, err := s.search(ctx, job.OldIndex)
	if err != nil {
		return err
	}
	reply, err := node.Do(ctx, "FT.AGGREGATE", job.OldIndex, "*", "LOAD", 1, "@__key",
		"TIMEOUT", 0, "WITHCURSOR", "COUNT", cursorCount, "DIALECT", 2).Result()
	if err != nil {
		return wrapRedis(err, "ALTER TABLE: FT.AGGREGATE failed")
	}
	for {
		rows, cursor, err := parseCursorReply(reply)
		if err != nil {
			if cursor != 0 {
				_ = node.Do(context.WithoutCancel(ctx), "FT.CURSOR", "DEL", job.OldIndex, cursor).Err()
			}
			return err
		}
		keys := make([]string, 0, len(rows))
		for _, r := range rows {
			if k, ok := r["__key"]; ok && strings.HasPrefix(k, job.OldPrefix) {
				keys = append(keys, k)
			}
		}
		if testHookRewriteStep != nil {
			testHookRewriteStep(ctx, "copy")
		}
		if retry {
			keys, err = s.missingRewriteKeys(ctx, job, keys)
		}
		if err == nil {
			err = e.rewriteKeys(ctx, job, rw, keys)
		}
		if err != nil {
			if cursor != 0 {
				_ = node.Do(context.WithoutCancel(ctx), "FT.CURSOR", "DEL", job.OldIndex, cursor).Err()
			}
			return err
		}
		if cursor == 0 {
			return nil
		}
		if reply, err = node.Do(ctx, "FT.CURSOR", "READ", job.OldIndex, cursor, "COUNT", cursorCount).Result(); err != nil {
			return wrapRedis(err, "ALTER TABLE: FT.CURSOR READ failed")
		}
	}
}

// missingRewriteKeys keeps source keys whose new-prefix HASH was not
// written by an earlier scan. No other writer can create these copies
// until the rewrite switches metadata.
func (s *store) missingRewriteKeys(ctx context.Context, job *rekeyJob, keys []string) ([]string, error) {
	missing := make([]string, 0, len(keys))
	for start := 0; start < len(keys); start += pipelineChunk {
		chunk := keys[start:min(start+pipelineChunk, len(keys))]
		pipe := s.client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(chunk))
		for i, key := range chunk {
			cmds[i] = pipe.Exists(ctx, job.KeyPrefix+strings.TrimPrefix(key, job.OldPrefix))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, wrapRedis(err, "ALTER TABLE: checking copied rows failed")
		}
		for i, cmd := range cmds {
			if cmd.Val() == 0 {
				missing = append(missing, chunk[i])
			}
		}
	}
	return missing, nil
}

// rewriteKeys converts and copies rows to their new keys, with pipelined
// HGETALL and HSET.
func (e *executor) rewriteKeys(ctx context.Context, job *rekeyJob, rw *rewrite, keys []string) error {
	s := e.store
	for start := 0; start < len(keys); start += pipelineChunk {
		chunk := keys[start:min(start+pipelineChunk, len(keys))]
		pipe := s.client.Pipeline()
		reads := make([]*goredis.MapStringStringCmd, len(chunk))
		for i, k := range chunk {
			reads[i] = pipe.HGetAll(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "ALTER TABLE: HGETALL failed")
		}
		pipe = s.client.Pipeline()
		for i, k := range chunk {
			raw := reads[i].Val()
			if len(raw) == 0 {
				continue // deleted after the index listed it
			}
			fields, err := rw.convert(aggRow(raw))
			if err != nil {
				return err
			}
			pipe.HSet(ctx, job.KeyPrefix+strings.TrimPrefix(k, job.OldPrefix), fields...)
		}
		if pipe.Len() == 0 {
			continue
		}
		if err := job.mayAct(); err != nil {
			return err
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "ALTER TABLE: HSET failed")
		}
	}
	return nil
}

// switchRewrite is step 3: the metadata becomes the altered table's, on the
// new keys, if the table is otherwise as the rewrite began (sameDefinition):
// another ALTER that had started before it, a COMMENT ON or a DROP TABLE
// meanwhile makes it fail ("try again"), and it is rolled back.
func (e *executor) switchRewrite(ctx context.Context, job *rekeyJob, rw *rewrite) error {
	s := e.store
	schema, name := job.Schema, job.From
	key := metaKey(schema, name)
	for attempt := 0; attempt < 20; attempt++ {
		err := s.client.Watch(ctx, func(tx *goredis.Tx) error {
			if _, err := ownRekey(ctx, tx, job); err != nil {
				return err
			}
			cur, err := s.getTableWith(ctx, tx, schema, name)
			if err != nil {
				return err
			}
			if cur.RekeyTo != job.KeyPrefix || !sameDefinition(rw.start, cur) {
				return errorf(adbc.StatusIO, "table %q.%q was changed while ALTER COLUMN … TYPE rewrote its rows; try again", displaySchema(schema), name)
			}
			raw, err := marshalMeta(rw.next(cur))
			if err != nil {
				return err
			}
			switched := *job
			switched.Switched = true
			rawJob, err := json.Marshal(&switched)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.Set(ctx, key, raw, 0)
				// The new rows have no dropped columns' fields.
				p.SRem(ctx, cleanupKey, cleanupMember(schema, name))
				p.HSet(ctx, rekeyKey, job.OldPrefix, rawJob)
				return nil
			})
			return err
		}, key, rekeyKey)
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		if err != nil {
			return wrapRedis(err, "failed to switch the table to its rewritten rows")
		}
		job.Switched = true
		return nil
	}
	return errorf(adbc.StatusIO, "table %q.%q is being changed concurrently; try again", displaySchema(schema), name)
}
