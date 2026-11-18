// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestUnmanagedWideFullAggregateProjection(t *testing.T) {
	rows := 1001
	if value := os.Getenv("REDIS_UNMANAGED_FULL_PROFILE_ROWS"); value != "" {
		var err error
		rows, err = strconv.Atoi(value)
		if err != nil || rows < 1 {
			t.Fatal("invalid profile row count")
		}
	}
	h, _, table := unmanagedWideProjectionFixture(t, rows)
	for _, query := range []string{
		"SELECT SUM(id), COUNT(*), AVG(c1) FROM " + table,
		"SELECT AVG(c2) FROM " + table,
		"SELECT COUNT(DISTINCT c2) FROM " + table,
		"SELECT COUNT(DISTINCT c104) FROM " + table,
	} {
		t.Run(query, func(t *testing.T) {
			observer := &unmanagedAggregateProfileHook{commands: map[string]int64{}, wait: map[string]time.Duration{}}
			ctx, e, plan, _ := distinctSnapshotTestPlan(t, h, query, observer)
			observer.active.Store(true)
			started := time.Now()
			got, err := e.selectAggregate(ctx, plan, nil)
			projected := time.Since(started)
			observer.active.Store(false)
			if err != nil {
				t.Fatal(err)
			}
			// The established complete-image source remains the semantic oracle.
			// Defeat only the wide-schema heuristic on a private metadata copy.
			narrow := *plan.meta
			var cols []columnMeta
			for _, c := range narrow.Columns {
				if c.Name == "id" || c.Name == "c1" || c.Name == "c2" || c.Name == "c104" {
					cols = append(cols, c)
				}
			}
			narrow.Columns = cols
			baseline := *plan
			baseline.meta = &narrow
			started = time.Now()
			want, err := e.selectAggregate(ctx, &baseline, nil)
			complete := time.Since(started)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshotResultStrings(got), snapshotResultStrings(want)) {
				t.Fatal(got, want)
			}
			if observer.counts()["hmget"] == 0 {
				t.Fatal("narrow full aggregate still transfers complete 105-column HASHes")
			}
			// Exercise the public Arrow path and its shared worker admission too.
			h.expectRows(query, snapshotResultStrings(want)...)
			options := h.conn.(adbc.GetSetOptionsWithContext)
			last, err := options.GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
			grant, parseErr := strconv.Atoi(last)
			if err != nil || parseErr != nil || grant < 1 || grant > 5 {
				t.Fatal("unmanaged aggregate did not reserve a bounded grant", last, err, parseErr)
			}
			active, err := options.GetOption(h.ctx, OptionIntQueryWorkersActive)
			if err != nil || active != "0" {
				t.Fatal("completed aggregate retained fetch workers", active, err)
			}
			t.Logf("Synthetic fixture rows=%d; pinned Go execution projected=%s complete-image=%s; complete exact outputs match (not full-scale ClickBench)", rows, projected, complete)
		})
	}
}

func TestUnmanagedCountDistinctSpillAndCleanup(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	const distinct = spillBufferRows + 17
	// Repeated values cross both cursor-page and spill-run boundaries. Values
	// above 2^53 must stay distinct; embedded zero bytes are SQL string data.
	for start := 0; start < distinct*2; start += 1000 {
		pipe := raw.Pipeline()
		for i := start; i < min(start+1000, distinct*2); i++ {
			n := i % distinct
			text := "value" + string([]byte{0}) + strconv.Itoa(n)
			pipe.HSet(h.ctx, "app:spill"+strconv.Itoa(i), "n", strconv.FormatInt(9007199254740993+int64(n), 10), "extra", text)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.HSet(h.ctx, "app:null", "unselected", "present").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"n", "extra"} {
		h.expectRows("SELECT COUNT(DISTINCT "+column+") FROM live", fmt.Sprint(distinct+1))
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatal("distinct query retained temporary files", entries, err)
		}
	}
	// A spill error must fail the query and clean the private directory rather
	// than returning an incomplete cardinality or replaying input.
	t.Setenv("TMPDIR", dir+"/missing")
	h.expectError("SELECT COUNT(DISTINCT n) FROM live", "creating distinct spill directory failed")
	h.expectRows("SELECT COUNT(DISTINCT n) FROM live WHERE FALSE", "0")
}
