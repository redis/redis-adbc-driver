// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"math"
	"strconv"
	"testing"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
)

func TestPartitionCountDatabaseOptionsAtCreation(t *testing.T) {
	ctx := context.Background()
	for _, configured := range []string{"", "1", "16", "1024"} {
		t.Run(configured, func(t *testing.T) {
			opts := map[string]string{}
			want := int64(8)
			if configured != "" {
				opts[OptionIntPartitionCount] = configured
				want, _ = strconv.ParseInt(configured, 10, 64)
			}
			// Public wrapped database creation consumes options without connecting.
			db, err := NewDriver(nil).NewDatabaseWithContext(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(ctx)
			options := db.(adbc.GetSetOptionsWithContext)
			if got, err := options.GetOptionInt(ctx, OptionIntPartitionCount); err != nil || got != want {
				t.Fatalf("int default %d %v, want %d", got, err, want)
			}
			if got, err := options.GetOption(ctx, OptionIntPartitionCount); err != nil || got != strconv.FormatInt(want, 10) {
				t.Fatalf("string default %q %v", got, err)
			}
			if err := options.SetOptionInt(ctx, OptionIntPartitionCount, 32); err != nil {
				t.Fatal(err)
			}
			if got, _ := options.GetOption(ctx, OptionIntPartitionCount); got != "32" {
				t.Fatal(got)
			}
			if _, err := options.GetOptionInt(ctx, "unknown.partition.option"); !isStatus(err, adbc.StatusNotFound) {
				t.Fatal(err)
			}
			if err := options.SetOptionInt(ctx, "unknown.partition.option", 1); !isStatus(err, adbc.StatusNotImplemented) {
				t.Fatal(err)
			}
		})
	}
}

func TestPartitionCountConnectionNewStatementSnapshotAndOverride(t *testing.T) {
	ctx := context.Background()
	d := newDriverImpl(nil)
	db, err := d.newDatabaseImpl(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	c := &connectionImpl{ConnectionImplBase: driverbase.NewConnectionImplBase(&db.DatabaseImplBase), store: &store{}}
	newStatement := func() *statementImpl {
		t.Helper()
		st, err := c.NewStatement(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st.(*statementImpl)
	}
	initial := newStatement()
	if initial.partitions() != 8 {
		t.Fatal("default changed")
	}
	if err := c.SetOption(ctx, OptionIntPartitionCount, "16"); err != nil {
		t.Fatal(err)
	}
	a := newStatement()
	state := &incrementalPartitions{}
	a.partitionExecution = state
	if err := c.SetOptionInt(ctx, OptionIntPartitionCount, 32); err != nil {
		t.Fatal(err)
	}
	b := newStatement()
	if initial.partitions() != 8 || a.partitions() != 16 || b.partitions() != 32 || a.partitionExecution != state {
		t.Fatal("connection change mutated existing statement or incremental state")
	}
	if err := a.SetOptionInt(ctx, OptionIntPartitionCount, 4); err != nil {
		t.Fatal(err)
	}
	if a.partitionExecution != nil {
		t.Fatal("statement override did not reset own discovery")
	}
	if err := c.SetOption(ctx, OptionIntPartitionCount, "64"); err != nil {
		t.Fatal(err)
	}
	if a.partitions() != 4 || b.partitions() != 32 || newStatement().partitions() != 64 {
		t.Fatal("statement override/default lifetime changed")
	}
	if got, err := c.GetOptionInt(ctx, OptionIntPartitionCount); err != nil || got != 64 {
		t.Fatal(got, err)
	}
	if got, err := c.GetOption(ctx, OptionIntPartitionCount); err != nil || got != "64" {
		t.Fatal(got, err)
	}
	// Existing typed connection dispatch remains available.
	if got, err := c.GetOptionInt(ctx, OptionIntReadSnapshotRetention); err != nil || got != 900000 {
		t.Fatal(got, err)
	}
	if _, err := c.GetOptionInt(ctx, "unknown.partition.option"); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal(err)
	}
	if err := c.SetOptionInt(ctx, "unknown.partition.option", 1); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	// Database changes affect future Open calls, not this connection's copied scalar.
	if err := db.SetOption(ctx, OptionIntPartitionCount, "128"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.GetOptionInt(ctx, OptionIntPartitionCount); got != 64 {
		t.Fatal("database default leaked into existing connection")
	}
}

func TestPartitionCountInvalidDefaultsAreAtomic(t *testing.T) {
	ctx := context.Background()
	db := &databaseImpl{partitionCount: 16}
	c := &connectionImpl{partitionCount: 16}
	s := &statementImpl{partitionCount: 16}
	for _, object := range []struct {
		name   string
		set    func(context.Context, string, string) error
		setInt func(context.Context, string, int64) error
		get    func(context.Context, string) (int64, error)
	}{
		{"database", db.SetOption, db.SetOptionInt, db.GetOptionInt},
		{"connection", c.SetOption, c.SetOptionInt, c.GetOptionInt},
		{"statement", s.SetOption, s.SetOptionInt, s.GetOptionInt},
	} {
		t.Run(object.name, func(t *testing.T) {
			for _, value := range []string{"", "0", "-1", "1025", "auto", "16.0", " 16", "99999999999999999999999999"} {
				if err := object.set(ctx, OptionIntPartitionCount, value); !isStatus(err, adbc.StatusInvalidArgument) {
					t.Fatalf("%q: %v", value, err)
				}
				if got, _ := object.get(ctx, OptionIntPartitionCount); got != 16 {
					t.Fatalf("invalid string changed default: %d", got)
				}
			}
			for _, value := range []int64{math.MinInt64, -1, 0, 1025, math.MaxInt64} {
				if err := object.setInt(ctx, OptionIntPartitionCount, value); !isStatus(err, adbc.StatusInvalidArgument) {
					t.Fatalf("%d: %v", value, err)
				}
				if got, _ := object.get(ctx, OptionIntPartitionCount); got != 16 {
					t.Fatalf("invalid int changed default: %d", got)
				}
			}
		})
	}
}
