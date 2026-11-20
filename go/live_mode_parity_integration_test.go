// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A definition change must invalidate an attached live stream, including its
// buffered rows, while new reads and MERGE use the replacement definition.
func TestLiveModeParityEvolutionStreamAndMerge(t *testing.T) {
	f := newHashFixture(t, "it:live:parity:")
	h := f.h
	const table = "it_live_mode_parity"
	for _, key := range []string{f.prefix + "one", f.prefix + "two"} {
		f.hset(key, "n", "10", "old", "preserve")
	}
	reg, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{
		Prefix: f.prefix, Table: table, Apply: true,
		Types: map[string]string{"n": "BIGINT", "old": "VARCHAR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, table); err != nil {
			t.Error(err)
		}
	})
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.Expire(h.ctx, f.prefix+"one", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for option, value := range map[string]string{OptionStringStreamRequired: "true", OptionIntStreamBatchRows: "1"} {
		if err := st.SetOption(h.ctx, option, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT n FROM redis_live."+table); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatalf("first batch unavailable: %v", reader.Err())
	}
	// Writers are quiescent; an old reader may remain open but cannot continue
	// using its old definition after the explicit replacement.
	_, err = EvolveLiveHashes(h.ctx, f.opts, LiveHashEvolutionOptions{
		HashAdoptOptions: HashAdoptOptions{Prefix: f.prefix, Table: table, Apply: true},
		Columns:          []string{"n"}, ExpectedIndex: reg.Index, ExclusiveQuiescent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.Next() || !isStatus(reader.Err(), adbc.StatusInvalidState) {
		t.Fatalf("old definition emitted another batch: %v", reader.Err())
	}
	h.expectRows("SELECT n FROM redis_live."+table+" ORDER BY __key", "10", "10")
	h.expectAffected("MERGE INTO redis_live."+table+" t USING (SELECT 10 AS n) s ON t.n=s.n WHEN MATCHED THEN UPDATE SET n=11", 2)
	h.expectRows("SELECT n FROM redis_live."+table+" ORDER BY __key", "11", "11")
	for _, key := range []string{f.prefix + "one", f.prefix + "two"} {
		if value := f.raw.HGet(h.ctx, key, "old").Val(); value != "preserve" {
			t.Fatalf("removed public column lost its application field: %q", value)
		}
	}
	if ttl := f.raw.TTL(h.ctx, f.prefix+"one").Val(); ttl <= 0 {
		t.Fatal("schema evolution or MERGE removed source TTL")
	}
}
