// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"testing"
)

func TestDistinctStreamingLateErrorAndLifecycle(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_distinct_stream_lifecycle"
	h.setupStreamTable(table, 25000)
	defer h.dropTables(table)
	st, r, _ := h.openStmtStream(h.ctx, h.conn, "SELECT DISTINCT id/(id-22000) AS v FROM "+table, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"})
	rows := 0
	for r.Next() {
		rows += int(r.RecordBatch().NumRows())
	}
	if rows == 0 || r.Err() == nil || sqlState(r.Err()) != stateDivisionByZero {
		t.Fatal("late error lost", rows, r.Err())
	}
	r.Release()
	st.Close(h.ctx)
	for _, mode := range []string{"release", "context", "statement", "connection"} {
		other := newSQLHarness(t)
		ctx, cancel := context.WithCancel(other.ctx)
		st, r, _ := other.openStmtStream(ctx, other.conn, "SELECT DISTINCT id FROM "+table, nil, map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"})
		sr, ok := inspectionReader(r).(*streamReader)
		if !ok || sr.distinct == nil {
			r.Release()
			t.Fatal("distinct query did not stream")
		}
		if !r.Next() {
			t.Fatal(r.Err())
		}
		batch := r.RecordBatch()
		batch.Retain()
		switch mode {
		case "release":
			r.Release()
		case "context":
			cancel()
		case "statement":
			if err := canceler(t, st).Cancel(ctx); err != nil {
				t.Fatal(err)
			}
		case "connection":
			if err := other.conn.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if mode != "release" {
			if r.Next() || r.Err() == nil {
				t.Fatal("distinct survived cancellation", mode)
			}
			r.Release()
		}
		if sr.distinct != nil || sr.pending != nil || !sr.closed {
			t.Fatal("distinct state retained after close", mode)
		}
		if batch.NumRows() != 1 {
			t.Fatal("consumer retained batch invalidated")
		}
		batch.Release()
		cancel()
		st.Close(h.ctx)
	}
	h.exec("SELECT 1")
	// This selected path never grants aggregate worker tokens.
	opts := h.conn.(interface {
		GetOption(context.Context, string) (string, error)
	})
	active, err := opts.GetOption(h.ctx, OptionIntQueryWorkersActive)
	if err != nil || active != "0" {
		t.Fatal("worker budget not released", active, err)
	}
}
