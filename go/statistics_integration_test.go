// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

func TestStatisticsManagedCountsAndFilters(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_adbc_statistics"
	filter := table
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT, label TEXT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	stats := h.conn.(adbc.ConnectionGetStatistics)
	names, err := stats.GetStatisticNames(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !names.Schema().Equal(adbc.GetStatisticNamesSchema) || names.Next() || names.Err() != nil {
		t.Fatal("custom names must be empty with canonical schema")
	}
	names.Release()
	for _, phase := range []struct {
		sql   string
		count int64
	}{
		{"", 0},
		{"INSERT INTO " + table + " VALUES (1,'x'),(1,NULL),(3,'x')", 3},
		{"UPDATE " + table + " SET label='changed' WHERE id=1", 3},
		{"DELETE FROM " + table + " WHERE id=3", 2},
		{"INSERT INTO " + table + " VALUES (4,NULL)", 3},
		{"TRUNCATE " + table, 0},
	} {
		expected := phase.count
		if phase.sql != "" {
			h.exec(phase.sql)
		}
		for _, approximate := range []bool{true, false} {
			r, err := stats.GetStatistics(h.ctx, nil, nil, &filter, approximate)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Schema().Equal(adbc.GetStatisticsSchema) {
				t.Fatal(r.Schema())
			}
			count := 0
			for r.Next() {
				record := r.RecordBatch()
				schemas := record.Column(1).(*array.List).ListValues().(*array.Struct)
				values := schemas.Field(1).(*array.List).ListValues().(*array.Struct)
				for i := 0; i < values.Len(); i++ {
					if values.Field(0).(*array.String).Value(i) != table || !values.Field(1).IsNull(i) || values.Field(2).(*array.Int16).Value(i) != adbc.StatisticRowCountKey || !values.Field(4).(*array.Boolean).Value(i) {
						t.Fatal(values)
					}
					union := values.Field(3).(*array.DenseUnion)
					if union.TypeCode(i) != 0 || union.Field(0).(*array.Int64).Value(int(union.ValueOffset(i))) != expected {
						t.Fatal(union)
					}
					count++
				}
			}
			if r.Err() != nil || count != 1 {
				t.Fatalf("count=%d error=%v", count, r.Err())
			}
			r.Release()
		}
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if _, err := stats.GetStatistics(ctx, nil, nil, nil, true); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal("cancel:", err)
	}
}

func TestStatisticsReadOnlyPermissions(t *testing.T) {
	a := newACLHarness(t)
	const table = "it_statistics_acl"
	a.exec("DROP TABLE IF EXISTS " + table)
	a.exec("CREATE TABLE " + table + " (id BIGINT)")
	a.exec("INSERT INTO " + table + " VALUES (1),(2)")
	t.Cleanup(func() { a.exec("DROP TABLE IF EXISTS " + table) })
	const user = "it_acl_statistics"
	a.user(user, append(a.readOnlyCommands(), "+ft.info", "%R~adbc:*", "%R~public:*")...)
	worker := a.connect(user, nil)
	stats := worker.conn.(adbc.ConnectionGetStatistics)
	filter := table
	r, err := stats.GetStatistics(a.ctx, nil, nil, &filter, true)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	r.Release()
	if err := a.eachNode(func(n *goredis.Client) error {
		return n.Do(a.ctx, "ACL", "SETUSER", user, "-ft.search").Err()
	}); err != nil {
		t.Fatal(err)
	}
	// Redis command denials use the shared I/O wrapper, preserving NOPERM.
	// Require that denial, not just any error, and never accept a result reader.
	r, err = stats.GetStatistics(a.ctx, nil, nil, &filter, true)
	if r != nil {
		r.Release()
		t.Fatal("denied count returned a result reader")
	}
	if !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "FT.SEARCH failed: NOPERM") {
		t.Fatalf("denied count must fail, never return zero: %v", err)
	}
}

// Read complete materialized statistics after the operation's pin is released.
func readStatisticCounts(t *testing.T, r array.RecordReader) map[string]int64 {
	t.Helper()
	defer r.Release()
	if !r.Schema().Equal(adbc.GetStatisticsSchema) {
		t.Fatal("noncanonical statistics schema")
	}
	out := map[string]int64{}
	for r.Next() {
		schemas := r.RecordBatch().Column(1).(*array.List).ListValues().(*array.Struct)
		values := schemas.Field(1).(*array.List).ListValues().(*array.Struct)
		for i := 0; i < values.Len(); i++ {
			name := values.Field(0).(*array.String).Value(i)
			if _, exists := out[name]; exists {
				t.Fatal("duplicate table statistic", name)
			}
			if !values.Field(1).IsNull(i) || values.Field(2).(*array.Int16).Value(i) != adbc.StatisticRowCountKey || !values.Field(4).(*array.Boolean).Value(i) {
				t.Fatal("invalid row statistic")
			}
			value := values.Field(3).(*array.DenseUnion)
			if value.TypeCode(i) != 0 {
				t.Fatal("non-int64 row count")
			}
			out[name] = value.Field(0).(*array.Int64).Value(int(value.ValueOffset(i)))
		}
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

type statisticsReadHook struct {
	snapshotHook
	approximate, deny, fired bool
	indexes, prefixes        map[string]bool
	change                   func()
	token                    string
}

func (h *statisticsReadHook) touch(ctx context.Context) {
	if h.fired {
		return
	}
	h.fired = true
	scope := snapshotOf(ctx)
	if scope != nil && scope.pin != nil {
		h.token = scope.pin.token
	}
	if h.change != nil {
		h.change()
	}
}
func (h *statisticsReadHook) matches(cmd goredis.Cmder) bool {
	args := cmd.Args()
	if len(args) < 2 {
		return false
	}
	if h.approximate {
		return cmd.Name() == "ft.search" && h.indexes[fmt.Sprint(args[1])]
	}
	return cmd.Name() == "hmget" && h.prefixes[snapshotRowPrefix(fmt.Sprint(args[1]))]
}
func (h *statisticsReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		match := h.matches(cmd)
		if match && h.deny {
			h.touch(ctx)
			err := errors.New("NOPERM injected statistics source denial")
			cmd.SetErr(err)
			return err
		}
		err := next(ctx, cmd)
		if err == nil && match {
			h.touch(ctx)
		}
		return err
	}
}
func (h *statisticsReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		match := false
		for _, cmd := range cmds {
			match = match || h.matches(cmd)
		}
		if match && h.deny {
			h.touch(ctx)
			err := errors.New("NOPERM injected statistics source denial")
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
			return err
		}
		err := next(ctx, cmds)
		if err == nil && match {
			h.touch(ctx)
		}
		return err
	}
}
func statisticsHookedContext(t *testing.T, h *sqlHarness, hook *statisticsReadHook, tables ...string) (context.Context, *connectionImpl) {
	t.Helper()
	hook.indexes, hook.prefixes = map[string]bool{}, map[string]bool{}
	raw := h.rawClient()
	for _, table := range tables {
		for _, seg := range h.inspectTable(raw, defaultSchema, table).snapshotSegments() {
			hook.indexes[seg.Index] = true
			hook.prefixes[seg.Prefix] = true
		}
	}
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stmt.Close(h.ctx) })
	impl := stmt.(*statementImpl).conn
	if len(impl.store.activeExecutions()) != 0 {
		t.Fatal("statistics hook requires an idle connection")
	}
	// Search uses the maintenance client; row fetches use the execution client.
	// The background compactor owns a separate pool.
	impl.store.client.AddHook(hook)
	ctx, transport := impl.executionContext(h.ctx, &impl.streams)
	transport.client.AddHook(hook)
	t.Cleanup(transport.finish)
	return ctx, impl
}

func TestStatisticsSnapshotVisibilityAndLifetime(t *testing.T) {
	for _, approximate := range []bool{true, false} {
		t.Run(fmt.Sprint(approximate), func(t *testing.T) {
			h := newSQLHarness(t)
			h.dropTables("it_stat_pin_a", "it_stat_pin_b")
			h.exec("CREATE TABLE it_stat_pin_a(id BIGINT)")
			h.exec("INSERT INTO it_stat_pin_a SELECT i FROM generate_series(1,6) AS g(i)")
			h.exec("CREATE TABLE it_stat_pin_b(id BIGINT)")
			h.exec("INSERT INTO it_stat_pin_b SELECT i FROM generate_series(1,4) AS g(i)")
			raw := h.rawClient()
			other := newSQLHarness(t)
			hook := &statisticsReadHook{approximate: approximate, change: func() {
				other.exec("TRUNCATE it_stat_pin_a")
				other.exec("INSERT INTO it_stat_pin_a VALUES (999)")
				other.exec("DROP TABLE it_stat_pin_b")
				other.exec("CREATE TABLE it_stat_pin_b(id BIGINT)")
				other.exec("INSERT INTO it_stat_pin_b VALUES (9),(10)")
				snapshotReapTest(&store{client: raw, snapshots: true}, h.ctx)
			}}
			ctx, impl := statisticsHookedContext(t, h, hook, "it_stat_pin_a", "it_stat_pin_b")
			filter := "it_stat_pin_%"
			r, err := impl.GetStatistics(ctx, nil, nil, &filter, approximate)
			if err != nil {
				t.Fatal(err)
			}
			if !hook.fired || hook.token == "" {
				r.Release()
				t.Fatal("statistics did not reach pinned source")
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+hook.token).Val() {
				r.Release()
				t.Fatal("materialized statistics retained reader pin")
			}
			if got, want := readStatisticCounts(t, r), map[string]int64{"it_stat_pin_a": 6, "it_stat_pin_b": 4}; !reflect.DeepEqual(got, want) {
				t.Fatal("mixed generation statistics", got)
			}
			r, err = h.conn.(adbc.ConnectionGetStatistics).GetStatistics(h.ctx, nil, nil, &filter, approximate)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := readStatisticCounts(t, r), map[string]int64{"it_stat_pin_a": 1, "it_stat_pin_b": 2}; !reflect.DeepEqual(got, want) {
				t.Fatal("fresh statistics stayed stale", got)
			}
		})
	}
}

func TestStatisticsSourceDenials(t *testing.T) {
	for _, approximate := range []bool{true, false} {
		t.Run(fmt.Sprint(approximate), func(t *testing.T) {
			h := newSQLHarness(t)
			h.dropTables("it_stat_denied")
			h.exec("CREATE TABLE it_stat_denied(id BIGINT)")
			h.exec("INSERT INTO it_stat_denied VALUES (1),(2)")
			hook := &statisticsReadHook{approximate: approximate, deny: true}
			ctx, impl := statisticsHookedContext(t, h, hook, "it_stat_denied")
			filter := "it_stat_denied"
			r, err := impl.GetStatistics(ctx, nil, nil, &filter, approximate)
			if r != nil {
				r.Release()
				t.Fatal("denied source returned statistics")
			}
			if !hook.fired || hook.token == "" || !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "NOPERM") {
				t.Fatal("source denial was not preserved", err)
			}
			if h.rawClient().HExists(h.ctx, snapshotCatalogKey, "p:"+hook.token).Val() {
				t.Fatal("denied statistics retained reader pin")
			}
		})
	}
}

func TestStatisticsSparseNativeFallback(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_stat_sparse")
	h.exec("CREATE TABLE it_stat_sparse(id BIGINT)")
	h.exec("INSERT INTO it_stat_sparse VALUES (1),(2),(3)")
	h.sparseSnapshotTable("it_stat_sparse", exactDoubleLimit+11, map[int64]int64{1: exactDoubleLimit + 1})
	// Search row-ID masks cannot represent this source exactly; a count must
	// succeed from authoritative directories even if Search counting is denied.
	hook := &statisticsReadHook{approximate: true, deny: true}
	ctx, impl := statisticsHookedContext(t, h, hook, "it_stat_sparse")
	filter := "it_stat_sparse"
	for _, approximate := range []bool{true, false} {
		r, err := impl.GetStatistics(ctx, nil, nil, &filter, approximate)
		if err != nil {
			t.Fatal(err)
		}
		if got := readStatisticCounts(t, r); !reflect.DeepEqual(got, map[string]int64{filter: 3}) {
			t.Fatal("sparse count differs", got)
		}
	}
	if hook.fired {
		t.Fatal("inexact IDs used Search counting")
	}
}
