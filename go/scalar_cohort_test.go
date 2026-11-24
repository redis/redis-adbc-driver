// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"

	goredis "github.com/redis/go-redis/v9"
)

type scalarCohortReplies struct {
	rows          map[string]aggRow
	prefix        string
	reads         int
	fetches       int
	failAfterPage bool
	leaseFailure  bool
	cancel        context.CancelFunc
}

func (*scalarCohortReplies) DialHook(goredis.DialHook) goredis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("unexpected network in cohort unit test")
	}
}
func (h *scalarCohortReplies) reply(cmd goredis.Cmder) error {
	switch cmd.Name() {
	case "fcall":
		if h.leaseFailure {
			return errors.New("synthetic lease failure")
		}
		cmd.(interface{ SetVal(any) }).SetVal(int64(1))
	case "zrange":
		h.reads++
		if h.failAfterPage && h.reads > 1 {
			return errors.New("synthetic later-page source failure")
		}
		var ids []string
		for key := range h.rows {
			n, _ := strconv.ParseInt(strings.TrimPrefix(key, h.prefix), 10, 64)
			ids = append(ids, fmt.Sprintf("%020d", n))
		}
		sort.Strings(ids)
		start, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[2]))
		end, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[3]))
		end = min(end+1, len(ids))
		start = min(start, len(ids))
		cmd.(*goredis.StringSliceCmd).SetVal(ids[start:end])
	case "hmget":
		h.fetches++
		raw := h.rows[fmt.Sprint(cmd.Args()[1])]
		values := make([]any, len(cmd.Args())-2)
		for i, field := range cmd.Args()[2:] {
			if v, ok := raw[fmt.Sprint(field)]; ok {
				values[i] = v
			}
		}
		cmd.(*goredis.SliceCmd).SetVal(values)
		if h.cancel != nil {
			h.cancel()
			h.cancel = nil
		}
	default:
		return fmt.Errorf("unexpected cohort command %s", cmd.Name())
	}
	return nil
}
func (h *scalarCohortReplies) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		err := h.reply(cmd)
		if err != nil {
			cmd.SetErr(err)
		}
		return err
	}
}
func (h *scalarCohortReplies) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
		}
		return nil
	}
}

// Metadata injection avoids real catalog I/O, but parsing/binding, registration,
// CASE/outer-row evaluation, subqueryRows activation and directory/HASH scan are
// the production paths. Command hooks never delegate or open a socket.
func cohortFixture(t *testing.T, outerRows int) (*executor, context.Context, *scalarCohortReplies, *tableMeta) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = client.Close() })
	s := &store{client: client, snapshots: true, flex: true}
	access, id := snapshotID(), snapshotID()
	n := int64(3)
	meta := &tableMeta{Schema: defaultSchema, Name: "facts", SnapshotAccessID: access, KeyPrefix: snapshotRows(id, defaultSchema, access), IndexName: snapshotIndexPrefix(id) + "working", SnapshotRows: &n, PlainIndex: true, SnapshotOverlay: true,
		Columns: []columnMeta{{Name: "qty", Type: typeInt32}, {Name: "good", Field: "old_good", Type: decimalType(18, 2), Missing: "12.50", MissingThrough: 2}, {Name: "bad", Type: decimalType(18, 2)}}}
	h := &scalarCohortReplies{prefix: meta.prefix(), rows: map[string]aggRow{}}
	h.rows[h.prefix+"1"] = aggRow{rowIDField: "1", "qty": "1", "bad": "2.00"}
	h.rows[h.prefix+"2"] = aggRow{rowIDField: "2", "qty": "2", nullMarker("old_good"): "1", "bad": "malformed"}
	h.rows[h.prefix+"3"] = aggRow{rowIDField: "3", "qty": "3", "old_good": "7.50", "bad": "4.00"}
	client.AddHook(h)
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s, id: snapshotID(), pin: &snapshotPin{store: s}})
	gate := &tableMeta{Schema: defaultSchema, Name: "gate", isMem: true, Columns: []columnMeta{{Name: "k", Type: typeInt32}}}
	for i := 0; i < outerRows; i++ {
		gate.mem = append(gate.mem, map[string]Value{"k": intValue(typeInt32, 1)})
	}
	e := &executor{store: s, cache: newExecCache(), sess: newSession(defaultSchema), schema: defaultSchema,
		dmlSnapshots: map[string]*tableMeta{metaKey(defaultSchema, "facts"): meta, metaKey(defaultSchema, "gate"): gate}}
	return e, ctx, h, meta
}

const cohortParsedCase = `SELECT CASE WHEN (SELECT COUNT(*) FROM facts WHERE qty>=1)>0 THEN (SELECT AVG(good) FROM facts WHERE qty>=1) ELSE (SELECT AVG(bad) FROM facts WHERE qty>=1) END FROM gate WHERE k=1`

func cohortPlan(t *testing.T, e *executor, ctx context.Context, q string) *selectPlan {
	t.Helper()
	ps, err := ParseScript(q)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.planSelect(ctx, ps[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func oneCohort(t *testing.T, e *executor) *scalarCohort {
	t.Helper()
	if len(e.cache.scalarCohorts) != 3 {
		t.Fatalf("parsed siblings not registered: %d", len(e.cache.scalarCohorts))
	}
	var found *scalarCohort
	for _, c := range e.cache.scalarCohorts {
		if found != nil && found != c {
			t.Fatal("siblings split")
		}
		found = c
	}
	return found
}
func TestScalarCohortParsedCaseActivatesOnceAndDefersUnusedError(t *testing.T) {
	e, ctx, h, _ := cohortFixture(t, 2)
	p := cohortPlan(t, e, ctx, cohortParsedCase)
	c := oneCohort(t, e)
	if c.started || h.reads != 0 || h.fetches != 0 {
		t.Fatal("registration executed source")
	}
	rows, err := e.runSelect(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0][0].Text() != "10.0000000000000000" || rows[1][0].Text() != rows[0][0].Text() {
		t.Fatalf("exact AVG/default/NULL output: %v", rows)
	}
	if c.scans != 1 || h.reads != 1 || h.fetches != 3 {
		t.Fatalf("not one actual raw source scan: %d/%d/%d", c.scans, h.reads, h.fetches)
	}
	for _, lane := range c.lanes {
		if lane.input == "bad" {
			if lane.finalized {
				t.Fatal("unused AVG finalized")
			}
			if lane.err == nil {
				t.Fatal("unused malformed column was not remembered")
			}
			if _, ok := e.cache.sub[lane.sq]; ok {
				t.Fatal("unused CASE member finalized/cached")
			}
			if _, err := e.subqueryRows(ctx, lane.sq, e.newEnv(ctx, nil, nil)); err == nil || !strings.Contains(err.Error(), "bad") {
				t.Fatalf("selected deferred error absent: %v", err)
			}
		}
	}
}
func TestScalarCohortParsedZeroOuterAndNestedCaseRemainLazy(t *testing.T) {
	for _, q := range []string{cohortParsedCase, `SELECT CASE WHEN k=0 THEN (` + strings.TrimSuffix(strings.TrimPrefix(cohortParsedCase, "SELECT "), " FROM gate WHERE k=1") + `) ELSE 42 END FROM gate WHERE k=1`} {
		e, ctx, h, _ := cohortFixture(t, 0)
		p := cohortPlan(t, e, ctx, q)
		c := oneCohort(t, e)
		rows, err := e.runSelect(ctx, p, nil)
		if err != nil || len(rows) != 0 || c.started || h.reads != 0 {
			t.Fatalf("zero outer invoked source: %v %v", rows, err)
		}
	}
	e, ctx, h, _ := cohortFixture(t, 1)
	q := `SELECT CASE WHEN k=0 THEN (` + strings.TrimSuffix(strings.TrimPrefix(cohortParsedCase, "SELECT "), " FROM gate WHERE k=1") + `) ELSE 42 END FROM gate WHERE k=1`
	p := cohortPlan(t, e, ctx, q)
	c := oneCohort(t, e)
	rows, err := e.runSelect(ctx, p, nil)
	if err != nil || rows[0][0].I != 42 || c.started || h.reads != 0 {
		t.Fatalf("unused nested CASE activated source: %v %v", rows, err)
	}
}
func TestScalarCohortRejectedRowsStillDecodeInMetadataOrder(t *testing.T) {
	e, ctx, h, m := cohortFixture(t, 1)
	// The actual AVG rejects every row, but its needed malformed metric must
	// still fail just as decodeRow-before-WHERE does on the serial path.
	h.rows[h.prefix+"1"]["old_good"] = "broken"
	q := `SELECT (SELECT AVG(good) FROM facts WHERE qty<0),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
	p := cohortPlan(t, e, ctx, q)
	if len(e.cache.scalarCohorts) != 2 {
		t.Fatal("two parsed siblings not registered")
	}
	_, err := e.runSelect(ctx, p, nil)
	_, want := decodeRow(m, h.rows[h.prefix+"1"], map[string]bool{"qty": true, "good": true})
	if err == nil || fmt.Sprint(err) != fmt.Sprint(want) {
		t.Fatalf("rejected-row decode order: %v want %v", err, want)
	}
}
func TestScalarCohortCancellationAndLaterSourceFailureFailClosed(t *testing.T) {
	e, ctx, h, _ := cohortFixture(t, 1)
	p := cohortPlan(t, e, ctx, cohortParsedCase)
	cancelledCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	if _, err := e.runSelect(cancelledCtx, p, nil); err == nil {
		t.Fatal("ignored cancellation during native fetch")
	}
	e, ctx, h, m := cohortFixture(t, 1)
	for i := 4; i <= snapshotDeltaPage+1; i++ {
		h.rows[h.prefix+strconv.Itoa(i)] = aggRow{rowIDField: strconv.Itoa(i), "qty": "1", "old_good": "1.00", "bad": "malformed"}
	}
	*m.SnapshotRows = int64(len(h.rows))
	h.failAfterPage = true
	p = cohortPlan(t, e, ctx, cohortParsedCase)
	if _, err := e.runSelect(ctx, p, nil); err == nil || !strings.Contains(err.Error(), "later-page source failure") {
		t.Fatalf("source error lost behind unused lane error: %v", err)
	}
	// An active first-page decode error must stop before that later-page read.
	e, ctx, h, m = cohortFixture(t, 1)
	for i := 4; i <= snapshotDeltaPage+1; i++ {
		h.rows[h.prefix+strconv.Itoa(i)] = aggRow{rowIDField: strconv.Itoa(i), "qty": "1"}
	}
	*m.SnapshotRows = int64(len(h.rows))
	h.failAfterPage = true
	h.rows[h.prefix+"1"]["qty"] = "broken"
	p = cohortPlan(t, e, ctx, cohortParsedCase)
	if _, err := e.runSelect(ctx, p, nil); err == nil || h.reads != 1 || !strings.Contains(err.Error(), "qty") {
		t.Fatalf("read later page after active lane failed: reads=%d error=%v", h.reads, err)
	}
}

func TestScalarCohortExactDecimalAndNullEmptyParity(t *testing.T) {
	for _, mode := range []string{"exact", "null", "empty"} {
		t.Run(mode, func(t *testing.T) {
			e, ctx, h, m := cohortFixture(t, 1)
			for _, row := range h.rows {
				delete(row, nullMarker("old_good"))
				row["old_good"] = "9007199254740.93"
			}
			h.rows[h.prefix+"2"]["old_good"] = "9007199254740.95"
			if mode == "null" {
				for _, row := range h.rows {
					delete(row, "old_good")
					row[nullMarker("old_good")] = "1"
				}
			}
			if mode == "empty" {
				h.rows = map[string]aggRow{}
				*m.SnapshotRows = 0
			}
			q := `SELECT (SELECT AVG(good) FROM facts WHERE qty>=1),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			p := cohortPlan(t, e, ctx, q)
			if len(e.cache.scalarCohorts) != 2 {
				t.Fatal("parsed cohort missing")
			}
			var avg *scalarCohortLane
			for _, c := range e.cache.scalarCohorts {
				for _, lane := range c.lanes {
					if lane.input == "good" {
						avg = lane
					}
				}
			}
			reference := newAccumulator(avg.fn)
			for _, id := range []string{"1", "2", "3"} {
				raw := h.rows[h.prefix+id]
				if raw == nil {
					continue
				}
				values, err := decodeRow(m, raw, avg.need)
				if err != nil {
					t.Fatal(err)
				}
				env := e.newEnv(ctx, m.types(), nil)
				env.row = values
				condition, err := env.eval(avg.sq.plan.sel.Where)
				if err != nil {
					t.Fatal(err)
				}
				if b, valid := truthy(condition); b && valid {
					reference.add(values["good"])
				}
			}
			want, err := reference.result(e.newEnv(ctx, m.types(), nil), avg.typ)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := e.runSelect(ctx, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := rows[0][0]
			if got.T != want.T || got.Null != want.Null || !got.Null && got.Text() != want.Text() {
				t.Fatalf("serial AVG type/value differs: got%v/%s want%v/%s", got.T, cohortText(got), want.T, cohortText(want))
			}
			if mode != "exact" && !got.Null {
				t.Fatal("empty/all-NULL AVG must be typed NULL")
			}
		})
	}
}

func TestScalarCohortEligibilityAndCaps(t *testing.T) {
	for _, mode := range []string{"one", "many", "fields", "volatile", "correlated", "grouped", "ordered", "sum", "float", "retained", "private", "partition", "live", "unmanaged", "matview"} {
		t.Run(mode, func(t *testing.T) {
			e, ctx, _, m := cohortFixture(t, 1)
			q := cohortParsedCase
			switch mode {
			case "one":
				q = `SELECT (SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "many":
				var list []string
				for i := 0; i <= scalarCohortMembers; i++ {
					list = append(list, `(SELECT COUNT(*) FROM facts WHERE qty>=1)`)
				}
				q = `SELECT ` + strings.Join(list, ",") + ` FROM gate`
			case "fields":
				var list []string
				for i := 0; i <= scalarCohortFields; i++ {
					name := fmt.Sprint("metric", i)
					m.Columns = append(m.Columns, columnMeta{Name: name, Type: decimalType(18, 2)})
					list = append(list, `(SELECT AVG(`+name+`) FROM facts WHERE qty>=1)`)
				}
				q = `SELECT ` + strings.Join(list, ",") + ` FROM gate`
			case "volatile":
				q = `SELECT (SELECT AVG(good) FROM facts WHERE random()>0),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "correlated":
				q = `SELECT (SELECT AVG(good) FROM facts WHERE qty=gate.k),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "grouped":
				q = `SELECT (SELECT AVG(good) FROM facts GROUP BY qty),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "ordered":
				q = `SELECT (SELECT AVG(good) FROM facts ORDER BY AVG(good)),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "sum":
				q = `SELECT (SELECT SUM(good) FROM facts WHERE qty>=1),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "float":
				m.Columns[1].Type = typeFloat64
				q = `SELECT (SELECT AVG(good) FROM facts WHERE qty>=1),(SELECT COUNT(*) FROM facts WHERE qty>=1) FROM gate`
			case "retained":
				m.retainedSource = true
			case "private":
				snapshotOf(ctx).write = true
				snapshotOf(ctx).owned = map[string]bool{snapshotDataID(m.prefix()): true}
			case "partition":
				snapshotOf(ctx).partition = true
			case "live":
				m.Live = true
			case "unmanaged":
				m.Unmanaged = true
			case "matview":
				m.MaterializedSQL = "SELECT private_source"
			}
			if mode == "live" || mode == "unmanaged" || mode == "matview" {
				if e.scalarCohortSource(ctx, m) {
					t.Fatal("unsupported source admitted")
				}
				return
			}
			_ = cohortPlan(t, e, ctx, q)
			if len(e.cache.scalarCohorts) != 0 {
				t.Fatalf("ineligible parsed shape admitted: %s", mode)
			}
		})
	}
}

func TestScalarCohortDoesNotReuseAcrossSnapshotOrStatement(t *testing.T) {
	e, ctx, h, _ := cohortFixture(t, 1)
	p := cohortPlan(t, e, ctx, cohortParsedCase)
	c := oneCohort(t, e)
	if _, err := e.runSelect(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	var selected *Subquery
	for _, lane := range c.lanes {
		if lane.input == "good" {
			selected = lane.sq
		}
	}
	// The helper itself refuses a changed snapshot, even before ordinary cache
	// policy is considered. Fresh statements get fresh execCache state.
	other := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{store: e.store, id: snapshotID(), pin: &snapshotPin{store: e.store}})
	if _, handled, err := e.scalarCohortRows(other, selected); handled || err != nil {
		t.Fatal("cross-snapshot cohort reuse")
	}
	e.cache = newExecCache()
	p = cohortPlan(t, e, ctx, cohortParsedCase)
	next := oneCohort(t, e)
	if next == c || next.started {
		t.Fatal("cross-statement cohort state reused")
	}
	if _, err := e.runSelect(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	if next.scans != 1 || h.reads != 2 {
		t.Fatalf("new statement did not rescan source: %d/%d", next.scans, h.reads)
	}
}

func cohortText(v Value) string {
	if v.Null {
		return "NULL"
	}
	return v.Text()
}

func TestScalarCohortParsedStreamingSelectUsesSameLazyActivation(t *testing.T) {
	e, ctx, h, facts := cohortFixture(t, 1)
	// A physical outer relation makes the ordinary SQL Arrow streaming route
	// eligible. Its metadata remains distinct from the sibling fact descriptor.
	gate := *facts
	gate.Name = "gate"
	gate.Columns = []columnMeta{{Name: "k", Type: typeInt32}}
	e.dmlSnapshots[metaKey(defaultSchema, "gate")] = &gate
	q := strings.TrimSuffix(cohortParsedCase, " WHERE k=1")
	p := cohortPlan(t, e, ctx, q)
	if !p.streamable() {
		t.Fatal("parsed physical outer query did not select streaming path")
	}
	c := oneCohort(t, e)
	result, err := e.streamSelect(ctx, p, nil, &streamOpts{mem: memory.DefaultAllocator, batchRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	count := len(result.rows)
	if result.stream != nil {
		defer result.stream.Release()
		for result.stream.Next() {
			count += int(result.stream.RecordBatch().NumRows())
		}
		if err := result.stream.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if count != 3 || c.scans != 1 || h.reads != 2 {
		t.Fatalf("ordinary streamed CASE did not share a single fact scan: rows=%d scans=%d directoryReads=%d", count, c.scans, h.reads)
	}
	for _, lane := range c.lanes {
		if lane.input == "bad" && lane.finalized {
			t.Fatal("stream finalized unused CASE AVG")
		}
	}
}

func TestScalarCohortCountOnlyStaysNarrowAndSourceProofsFailClosed(t *testing.T) {
	e, ctx, h, _ := cohortFixture(t, 1)
	p := cohortPlan(t, e, ctx, `SELECT (SELECT COUNT(*) FROM facts),(SELECT COUNT(*) FROM facts) FROM gate`)
	if len(e.cache.scalarCohorts) != 2 {
		t.Fatal("COUNT siblings not registered")
	}
	var c *scalarCohort
	for _, x := range e.cache.scalarCohorts {
		c = x
	}
	if c.fields == nil || len(c.fields) != 0 {
		t.Fatal("zero-user-field scan would become HGETALL")
	}
	rows, err := e.runSelect(ctx, p, nil)
	if err != nil || rows[0][0].I != 3 || rows[0][1].I != 3 || c.scans != 1 || h.fetches != 3 {
		t.Fatalf("narrow COUNT cohort: %v %v", rows, err)
	}
	for _, mode := range []string{"identity", "lease"} {
		e, ctx, h, _ = cohortFixture(t, 1)
		if mode == "identity" {
			delete(h.rows[h.prefix+"2"], rowIDField)
		} else {
			h.leaseFailure = true
		}
		p = cohortPlan(t, e, ctx, cohortParsedCase)
		if _, err := e.runSelect(ctx, p, nil); err == nil {
			t.Fatalf("%s source proof failure was swallowed", mode)
		}
		if len(e.cache.sub) != 0 {
			t.Fatalf("%s source failure published a successful lane", mode)
		}
	}
}
