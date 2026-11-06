// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type dirtyDescriptorReadFault struct {
	key    string
	failAt int64
	reads  atomic.Int64
	err    error
}

func (*dirtyDescriptorReadFault) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*dirtyDescriptorReadFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (f *dirtyDescriptorReadFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if cmd.Name() == "get" && len(a) == 2 && fmt.Sprint(a[1]) == f.key && f.reads.Add(1) >= f.failAt {
			cmd.SetErr(f.err)
			return f.err
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotDirtyDescriptorReusePreservesHydrationAndFreshness(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT DEFAULT 7 CHECK(v>0))")
	h.exec("INSERT INTO t VALUES(9007199254740993)")
	s := snapshotTestStore(t, raw)
	ctx, _, _ := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v+1 WHERE __rowid=1")
	suffix := strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
	key, err := resolveSnapshotMetadata(ctx, suffix, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := raw.Get(h.ctx, key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var meta tableMeta
	if err = json.Unmarshal(encoded, &meta); err != nil {
		t.Fatal(err)
	}
	// Force the same local policies needed by normal descriptor loads: catalog
	// OID hydration and an authoritative directory for the deferred output index.
	meta.OID, meta.IndexOID = 0, 0
	meta.SnapshotOverlay = false
	meta.Columns[0].DefaultOID = 0
	for i := range meta.Checks {
		meta.Checks[i].OID = 0
	}
	save := func() {
		t.Helper()
		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err = raw.Set(h.ctx, key, b, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	save()
	fault := &dirtyDescriptorReadFault{key: key, failAt: 2, err: errors.New("redundant descriptor fetch refused")}
	s.client.AddHook(fault)
	for _, comment := range []string{"first pass", "updated for validation pass"} {
		meta.Comment = comment
		save()
		fault.reads.Store(0)
		began := time.Now()
		tables, err := s.snapshotDirtyTables(ctx)
		if err != nil {
			t.Fatal("failed to reuse the authorized descriptor read", err)
		}
		if len(tables) != 1 {
			t.Fatal("unexpected dirty table count", len(tables))
		}
		m := tables[0]
		if m.Comment != comment {
			t.Fatal("descriptor cached across publication phases")
		}
		if m.OID == 0 || m.IndexOID == 0 || m.Columns[0].DefaultOID == 0 || len(m.Checks) == 0 || m.Checks[0].OID == 0 {
			t.Fatal("catalog OID hydration lost")
		}
		if !m.SnapshotOverlay {
			t.Fatal("deferred output did not retain authoritative-directory visibility")
		}
		if m.readAt.Before(began) || m.readAt.After(time.Now()) {
			t.Fatal("descriptor read-time fence lost")
		}
		if fault.reads.Load() != 1 {
			t.Fatal("authorized descriptor was fetched more than once", fault.reads.Load())
		}
	}
}

func TestSnapshotDirtyDescriptorReuseRetainsRefusals(t *testing.T) {
	for _, kind := range []string{"first_get_error", "malformed_json", "wrong_name", "access_identity", "plain_index", "missing"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(v BIGINT)")
			h.exec("INSERT INTO t VALUES(9007199254740993)")
			s := snapshotTestStore(t, raw)
			ctx, _, _ := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v+1 WHERE __rowid=1")
			suffix := strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
			key, err := resolveSnapshotMetadata(ctx, suffix, false)
			if err != nil {
				t.Fatal(err)
			}
			b, err := raw.Get(h.ctx, key).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			var meta tableMeta
			if err = json.Unmarshal(b, &meta); err != nil {
				t.Fatal(err)
			}
			want := "invalid private snapshot table descriptor"
			switch kind {
			case "first_get_error":
				f := &dirtyDescriptorReadFault{key: key, failAt: 1, err: errors.New("descriptor read denied")}
				s.client.AddHook(f)
				want = "descriptor read denied"
			case "malformed_json":
				b = []byte("{")
			case "wrong_name":
				meta.Name = "another_table"
			case "access_identity":
				meta.SnapshotAccessID = "bad"
				want = "invalid snapshot table access identity"
			case "plain_index":
				meta.PlainIndex = true
				want = "plain-index tables require"
			case "missing":
				if err = raw.Del(h.ctx, key).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "missing" && kind != "first_get_error" {
				if kind != "malformed_json" {
					b, err = json.Marshal(meta)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err = raw.Set(h.ctx, key, b, 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			tables, err := s.snapshotDirtyTables(ctx)
			if kind == "missing" {
				if err != nil || len(tables) != 0 {
					t.Fatal("removed descriptor was not skipped", err, len(tables))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), want) || len(tables) != 0 {
				t.Fatal("invalid/unauthorized descriptor accepted", kind, err)
			}
		})
	}
}
