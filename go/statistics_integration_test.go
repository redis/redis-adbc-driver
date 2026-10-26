// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
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
	for _, expected := range []int64{0, 3} {
		if expected > 0 {
			h.exec("INSERT INTO " + table + " VALUES (1,'x'),(1,NULL),(3,'x')")
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
