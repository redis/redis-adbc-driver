// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// This opt-in client-call profile isolates an internal cold execution pool from
// a drained reusable pool. TLS/RESP3 remains required by the supplied Software
// URI. It is not a saturated throughput benchmark or Redis server CPU profile.
// Only operation categories/counts/times and dial counts are retained; SQL,
// application/physical keys, endpoint identities and credentials are omitted.
func TestOLTPStatementReadPoolProfile(t *testing.T) {
	if os.Getenv("REDIS_OLTP_STATEMENT_PROFILE") == "" {
		t.Skip("set REDIS_OLTP_STATEMENT_PROFILE on an owned verified Software fixture")
	}
	h, raw, _ := newSnapshotHarnessMode(t, true, false)
	h.exec("CREATE TABLE pool_point(id BIGINT,grp BIGINT,amount BIGINT,label VARCHAR)")
	h.exec("INSERT INTO pool_point SELECT i,i%16,i*3,'row-'||i FROM generate_series(1,1000) AS g(i)")
	if err := raw.HSet(h.ctx, "pool_app:1", "id", "1", "grp", "1", "amount", "3", "label", "row-1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "pool_app_idx", "ON", "HASH", "PREFIX", 1, "pool_app:", "SCHEMA", "id", "NUMERIC", "grp", "NUMERIC", "amount", "NUMERIC", "label", "TAG").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "pool_app_idx"); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Mode: "unmanaged", Prefix: "pool_app:", Table: "pool_live", Apply: true}); err != nil {
		t.Fatal(err)
	}
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	c := stmt.(*statementImpl).conn
	c.store.closeIdleExecutionPool()
	var dials atomic.Int64
	original := c.clientCfg.single.Dialer
	if original == nil {
		original = goredis.NewDialer(c.clientCfg.single)
	}
	c.clientCfg.single.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return original(ctx, network, address)
	}
	profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
	c.store.client.AddHook(profile)
	hooked := map[goredis.UniversalClient]bool{}
	type sample struct {
		Mode   string  `json:"mode"`
		Fresh  bool    `json:"fresh_execution_pool"`
		Sample int     `json:"sample"`
		Millis float64 `json:"milliseconds"`
		Dials  int64   `json:"dials"`
	}
	var samples []sample
	for _, mode := range []string{"managed", "unmanaged"} {
		query := "SELECT id,grp,amount,label FROM pool_point WHERE __rowid=1"
		if mode == "unmanaged" {
			query = "SELECT id,grp,amount,label FROM pool_live WHERE __key='pool_app:1'"
		}
		for _, fresh := range []bool{true, false} {
			c.store.closeIdleExecutionPool()
			for n := 0; n < 5; n++ {
				if fresh {
					c.store.closeIdleExecutionPool()
				}
				phase := fmt.Sprintf("%s/fresh=%t/sample=%d", mode, fresh, n)
				profile.mu.Lock()
				profile.phase = phase
				profile.phases[phase] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
				profile.mu.Unlock()
				before := dials.Load()
				started := time.Now()
				ctx, transport := c.executionContext(h.ctx, &c.streams)
				if !hooked[transport.client] {
					transport.client.AddHook(profile)
					hooked[transport.client] = true
				}
				if err := stmt.SetSqlQuery(ctx, query); err != nil {
					t.Fatal(err)
				}
				reader, _, err := stmt.ExecuteQuery(ctx)
				if err != nil {
					transport.finish()
					t.Fatal(err)
				}
				var rows []string
				for reader.Next() {
					rec := reader.RecordBatch()
					for i := 0; i < int(rec.NumRows()); i++ {
						rows = append(rows, strings.Join([]string{rec.Column(0).ValueStr(i), rec.Column(1).ValueStr(i), rec.Column(2).ValueStr(i), rec.Column(3).ValueStr(i)}, "|"))
					}
				}
				readErr := reader.Err()
				reader.Release()
				transport.finish()
				elapsed := float64(time.Since(started)) / float64(time.Millisecond)
				if readErr != nil || len(rows) != 1 || rows[0] != "1|1|3|row-1" {
					t.Fatal("incorrect completely fetched Arrow row", rows, readErr)
				}
				profile.mu.Lock()
				profile.phase = ""
				profile.phases[phase].Steps = 1
				profile.phases[phase].Millis = elapsed
				profile.mu.Unlock()
				samples = append(samples, sample{Mode: mode, Fresh: fresh, Sample: n, Millis: elapsed, Dials: dials.Load() - before})
			}
		}
	}
	encoded, err := json.Marshal(map[string]any{"samples": samples, "foreground_client_calls": profile.phases, "scope": "Complete Go SQL+Arrow read with isolated fresh versus reusable execution pools; sample 0 is warmup; worker maintenance unhooked; one known row; no caller retry"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pool_app:") || strings.Contains(string(encoded), "rediss://") {
		t.Fatal("profile exposed operational identifiers")
	}
	t.Logf("oltp_statement_pool_profile %s", encoded)
}
