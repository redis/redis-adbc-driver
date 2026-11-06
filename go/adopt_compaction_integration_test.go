// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

type adoptedLogicalState struct {
	definition []byte
	rows       []string
	schema     *arrow.Schema
}

func captureAdoptedLogicalState(t *testing.T, f *hashFixture, table string) adoptedLogicalState {
	t.Helper()
	s, ctx, pin := f.h.inspectionSnapshot(f.raw)
	defer pin.release()
	meta := *snapshotTableTest(t, s, ctx, table)
	// These fields describe physical storage, which background compaction may
	// replace without changing the logical definition, lineage or imported data.
	meta.KeyPrefix, meta.IndexName, meta.PrefixGen = "", "", 0
	meta.SnapshotRows, meta.SnapshotSegments, meta.SnapshotOverlay = nil, nil, false
	definition, err := json.Marshal(&meta)
	if err != nil {
		t.Fatal(err)
	}
	rows, schema := f.h.query("SELECT __rowid,__key,* FROM " + table + " ORDER BY __rowid")
	return adoptedLogicalState{definition, rows, schema}
}

func assertAdoptedLogicalState(t *testing.T, before, after adoptedLogicalState) {
	t.Helper()
	if !bytes.Equal(before.definition, after.definition) || !before.schema.Equal(after.schema) || !slices.Equal(before.rows, after.rows) {
		t.Fatal("failed refresh changed the logical definition, schema, provenance or complete rows")
	}
}

type adoptCompactionDuringRead struct {
	prefix string
	run    func(context.Context) error
	once   sync.Once
	err    error
	ran    bool
}

func (*adoptCompactionDuringRead) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*adoptCompactionDuringRead) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *adoptCompactionDuringRead) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			if a := cmd.Args(); len(a) > 1 && cmd.Name() == "hmget" && strings.HasPrefix(fmt.Sprint(a[1]), h.prefix) {
				h.once.Do(func() { h.ran = true; h.err = h.run(ctx) })
				if h.err != nil {
					for _, cmd := range cmds {
						cmd.SetErr(h.err)
					}
					return h.err
				}
			}
		}
		return nil
	}
}

func TestAdoptBlockedRefreshDuringCompaction(t *testing.T) {
	f := newHashFixture(t, "it:ad:compact:")
	h := f.h
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	statement.(*statementImpl).conn.store.stopSnapshotCompactor()
	if err := statement.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	s := &store{client: f.raw, snapshots: true}
	snapshotDrainCompactionTest(t, s)
	h.dropTables("it_ad_compact")
	t.Cleanup(func() { h.dropTables("it_ad_compact") })
	f.users(3)
	f.adopt(HashAdoptOptions{Table: "it_ad_compact", Apply: true})
	for i := range 3 {
		h.exec(fmt.Sprintf("UPDATE it_ad_compact SET name='version %d' WHERE __key='it:ad:compact:1'", i))
	}
	before := captureAdoptedLogicalState(t, f, "it_ad_compact")
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("fixture did not stage compaction", started, err)
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	f.hset("it:ad:compact:bad", "name", "bad", "balance", "lots")
	root := f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	c, err := dialHashes(h.ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(h.ctx)
	hook := &adoptCompactionDuringRead{prefix: f.prefix, run: func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		owner := snapshotID()
		defer s.releaseSnapshotCompaction(context.Background(), owner)
		for range 1000 {
			if _, err := s.snapshotCompactionStep(ctx, owner); err != nil {
				return err
			}
			job, err := s.readSnapshotCompaction(ctx)
			if err != nil || job == nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		return fmt.Errorf("compaction did not complete")
	}}
	c.client.AddHook(hook)
	a := &adopter{c: c, o: HashAdoptOptions{Prefix: f.prefix, Table: "it_ad_compact", Apply: true, Types: map[string]string{"balance": "NUMERIC(38,2)"}}, progress: func(string) {}, res: &HashAdoption{Report: &HashReport{Prefix: f.prefix}}}
	blocked, err := a.snapshotAdopt(c.context(h.ctx))
	if err == nil || blocked == nil || len(blocked.Report.Checks) != 1 || blocked.Report.Checks[0].Ready || blocked.Applied {
		t.Fatal("typed refresh was not blocked")
	}
	if !hook.ran || hook.err != nil {
		t.Fatal("compaction did not interleave with planning", hook.err)
	}
	if f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() == root {
		t.Fatal("fixture did not publish its legitimate compaction")
	}
	assertAdoptedLogicalState(t, before, captureAdoptedLogicalState(t, f, "it_ad_compact"))
}
