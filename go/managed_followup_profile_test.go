// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func followupValue(id int64, col int) int64 {
	if col == 0 {
		return id
	}
	if col == 2 {
		return 9007199254740993 + id
	}
	return id*1000 + int64(col)
}

func followupRecord(rows int) arrow.RecordBatch {
	fields, columns := make([]arrow.Field, 105), make([]arrow.Array, 105)
	for col := range columns {
		name := fmt.Sprintf("c%d", col)
		if col == 0 {
			name = "id"
		}
		fields[col] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64, Nullable: true}
		b := array.NewInt64Builder(memory.DefaultAllocator)
		b.Reserve(rows)
		for id := int64(1); id <= int64(rows); id++ {
			if col == 1 && id%17 == 0 {
				b.AppendNull()
			} else {
				b.Append(followupValue(id, col))
			}
		}
		columns[col] = b.NewArray()
		b.Release()
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, int64(rows))
	for _, a := range columns {
		a.Release()
	}
	return rec
}

func followupIngest(ctx context.Context, conn adbc.ConnectionWithContext, table, mode string, rec arrow.RecordBatch) error {
	st, err := conn.NewStatement(ctx)
	if err != nil {
		return err
	}
	defer st.Close(ctx)
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(ctx, k, v); err != nil {
			return err
		}
	}
	r, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
	if err != nil {
		return err
	}
	defer r.Release()
	if err := st.BindStream(ctx, r); err != nil {
		return err
	}
	n, err := st.ExecuteUpdate(ctx)
	if err == nil && n != rec.NumRows() {
		err = fmt.Errorf("ingested %d rows, expected %d", n, rec.NumRows())
	}
	return err
}

func followupFetch(ctx context.Context, conn adbc.ConnectionWithContext, query string) (records []arrow.RecordBatch, err error) {
	defer func() {
		if err != nil {
			for _, rec := range records {
				rec.Release()
			}
			records = nil
		}
	}()
	st, err := conn.NewStatement(ctx)
	if err != nil {
		return nil, err
	}
	defer st.Close(ctx)
	if err := st.SetSqlQuery(ctx, query); err != nil {
		return nil, err
	}
	r, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Release()
	for r.Next() {
		rec := r.RecordBatch()
		rec.Retain()
		records = append(records, rec)
	}
	return records, r.Err()
}

func followupValidate(t *testing.T, records []arrow.RecordBatch, lo, hi int64) {
	t.Helper()
	seen := make([]bool, hi-lo+1)
	var total int64
	for _, rec := range records {
		if rec.NumCols() != 105 {
			t.Fatal("incorrect column count", rec.NumCols())
		}
		ids := rec.Column(0).(*array.Int64)
		for row := 0; row < int(rec.NumRows()); row++ {
			id := ids.Value(row)
			if id < lo || id > hi || seen[id-lo] {
				t.Fatal("unexpected or duplicate row", id)
			}
			seen[id-lo], total = true, total+1
			for col := 0; col < 105; col++ {
				a := rec.Column(col).(*array.Int64)
				null := col == 1 && id%17 == 0
				if a.IsNull(row) != null || !null && a.Value(row) != followupValue(id, col) {
					t.Fatal("incorrect value/NULL", id, col)
				}
			}
		}
		rec.Release()
	}
	if total != hi-lo+1 {
		t.Fatal("incomplete fetch", total, hi-lo+1)
	}
}

// Attribution diagnostic: persistent Go connections, synthetic BIGINT/NULL
// values, full Arrow consumption. Validation is outside timing. This does not
// replace the Python/C-ABI 100K/105-column benchmark or claim its throughput.
func TestManagedFollowupProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_FOLLOWUP_PROFILE") != "true" {
		t.Skip("set REDIS_MANAGED_FOLLOWUP_PROFILE=true on owned Software")
	}
	h, _, db := newSnapshotHarnessMode(t, true, false)
	rec := followupRecord(100000)
	defer rec.Release()
	if err := followupIngest(h.ctx, h.conn, "followup_read", adbc.OptionValueIngestModeCreate, rec); err != nil {
		t.Fatal(err)
	}
	calls := &managedPerformanceCalls{}
	calls.reset()
	ctx, _ := statisticsHookedTransport(t, h, calls)
	small := rec.NewSlice(0, 10000)
	defer small.Release()
	for _, batch := range []int64{1000, 10000} {
		for trial := range 4 {
			table := fmt.Sprintf("followup_write_%d_%d", batch, trial)
			empty := small.NewSlice(0, 0)
			err := followupIngest(ctx, h.conn, table, adbc.OptionValueIngestModeCreate, empty)
			empty.Release()
			if err != nil {
				t.Fatal(err)
			}
			calls.reset()
			start := time.Now()
			pprof.Do(ctx, pprof.Labels("phase", fmt.Sprintf("write_%d", batch)), func(ctx context.Context) {
				for lo := int64(0); lo < small.NumRows(); lo += batch {
					part := small.NewSlice(lo, lo+batch)
					err = followupIngest(ctx, h.conn, table, adbc.OptionValueIngestModeAppend, part)
					part.Release()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
			calls.report(t, fmt.Sprintf("write batch=%d trial=%d", batch, trial), time.Since(start))
			actual, err := followupFetch(ctx, h.conn, "SELECT * FROM "+table)
			if err != nil {
				t.Fatal(err)
			}
			followupValidate(t, actual, 1, 10000)
			h.exec("DROP TABLE " + table)
		}
	}
	for _, readers := range []int{1, 4} {
		connections := make([]adbc.ConnectionWithContext, readers)
		contexts := make([]context.Context, readers)
		for i := range connections {
			connections[i] = admissionConnection(t, db)
			mini := &sqlHarness{t: t, ctx: h.ctx, conn: connections[i]}
			contexts[i], _ = statisticsHookedTransport(t, mini, calls)
		}
		for trial := range 4 {
			type result struct {
				records []arrow.RecordBatch
				lo, hi  int64
				err     error
			}
			tasks := 1
			if readers == 4 {
				tasks = 8
			}
			jobs, out := make(chan int, tasks), make(chan result, tasks)
			for j := range tasks {
				jobs <- j
			}
			close(jobs)
			calls.reset()
			start := time.Now()
			pprof.Do(h.ctx, pprof.Labels("phase", fmt.Sprintf("read_%d", readers)), func(context.Context) {
				for worker := range readers {
					go func() {
						for j := range jobs {
							lo, hi := int64(j*100000/tasks+1), int64((j+1)*100000/tasks)
							q := "SELECT * FROM followup_read"
							if tasks > 1 {
								q += fmt.Sprintf(" WHERE id BETWEEN %d AND %d", lo, hi)
							}
							r, err := followupFetch(contexts[worker], connections[worker], q)
							out <- result{r, lo, hi, err}
						}
						return
					}()
				}
			})
			var results []result
			for range tasks {
				results = append(results, <-out)
			}
			calls.report(t, fmt.Sprintf("read readers=%d trial=%d", readers, trial), time.Since(start))
			for _, r := range results {
				if r.err != nil {
					t.Fatal(r.err)
				}
				followupValidate(t, r.records, r.lo, r.hi)
			}
		}
	}
}
