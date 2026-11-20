// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"strings"
	"testing"
	"time"
)

func liveMergeFixture(t *testing.T) *hashFixture {
	t.Helper()
	f := newHashFixture(t, "it:live:merge:")
	f.hset(f.prefix+"one", "id", "1", "n", "10")
	f.hset(f.prefix+"two", "id", "2", "n", "20")
	f.hset(f.prefix+"three", "id", "3", "n", "30")
	if _, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_merge", Apply: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(f.h.ctx, f.opts, "it_live_merge"); err != nil {
			t.Error(err)
		}
	})
	if err := f.h.conn.(adbc.GetSetOptionsWithContext).SetOption(f.h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestLiveMergeActionsAndOwnership(t *testing.T) {
	f := liveMergeFixture(t)
	h := f.h
	f.hset(f.prefix+"one", "unknown", "preserve")
	if err := f.raw.Expire(h.ctx, f.prefix+"one", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id,11 AS n UNION ALL SELECT 2,22 UNION ALL SELECT 4,44) s ON t.id=s.id
 WHEN MATCHED AND s.id=2 THEN DELETE WHEN MATCHED THEN UPDATE SET n=s.n
 WHEN NOT MATCHED THEN INSERT(__key,id,n) VALUES('`+f.prefix+`four',s.id,s.n)
 WHEN NOT MATCHED BY SOURCE THEN UPDATE SET n=-1 RETURNING merge_action(),t.id,t.n`, "UPDATE|1|11", "DELETE|2|20", "INSERT|4|44", "UPDATE|3|-1")
	h.expectRows("SELECT id,n FROM redis_live.it_live_merge ORDER BY id", "1|11", "3|-1", "4|44")
	if f.raw.HGet(h.ctx, f.prefix+"one", "unknown").Val() != "preserve" || f.raw.TTL(h.ctx, f.prefix+"one").Val() <= 0 {
		t.Fatal("MERGE removed unknown field or TTL")
	}
	if f.raw.Exists(h.ctx, f.prefix+"two").Val() != 0 {
		t.Fatal("DELETE left source HASH")
	}
	rows, _ := h.query(`MERGE INTO redis_live.it_live_merge t USING (SELECT 5 AS id) s ON t.id=s.id WHEN NOT MATCHED THEN INSERT(id,n) VALUES(s.id,NULL) RETURNING t.__key,t.n`)
	if len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
	key, value, _ := strings.Cut(rows[0], "|")
	suffix, ok := strings.CutPrefix(key, f.prefix)
	id, err := uuid.Parse(suffix)
	if !ok || err != nil || id.Version() != 4 || value != "NULL" {
		t.Fatalf("generated RETURNING=%v", rows)
	}
	if f.raw.HExists(h.ctx, key, rowIDField).Val() || f.raw.HExists(h.ctx, key, keyField).Val() {
		t.Fatal("internal fields added")
	}
	h.expectAffected(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON t.id=s.id WHEN MATCHED THEN DO NOTHING`, 0)
	h.expectAffected(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON t.id=s.id WHEN MATCHED THEN UPDATE SET n=12`, 1)
}
func TestLiveMergePreflightAndIsolation(t *testing.T) {
	f := liveMergeFixture(t)
	h := f.h
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id UNION ALL SELECT 1) s ON t.id=s.id WHEN MATCHED THEN UPDATE SET n=99`, "second time")
	h.expectRows("SELECT n FROM redis_live.it_live_merge WHERE id=1", "10")
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING (SELECT 4 AS id) s ON t.id=s.id WHEN NOT MATCHED THEN INSERT(__key,id) VALUES(NULL,s.id)`, "__key")
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING (SELECT 4 AS id) s ON t.id=s.id WHEN NOT MATCHED THEN INSERT(__key,id) VALUES('`+f.prefix+`one',s.id)`, "already exists")
	h.exec("CREATE TABLE it_merge_managed(id INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_merge_managed") })
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING it_merge_managed s ON t.id=s.id WHEN MATCHED THEN DELETE`, "live writes may read only live tables")
	h.expectError(`MERGE INTO it_merge_managed t USING redis_live.it_live_merge s ON t.id=s.id WHEN MATCHED THEN DELETE`, "managed writes cannot access live tables")
	h.expectError(`WITH changed AS (DELETE FROM redis_live.it_live_merge RETURNING id) MERGE INTO redis_live.it_live_merge t USING changed s ON t.id=s.id WHEN MATCHED THEN DELETE`, "modifying CTEs")
}
func TestLiveMergeUncertainDeleteAndCancellation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost_reply", true: "cancel"}[stop], func(t *testing.T) {
			f := liveMergeFixture(t)
			h := f.h
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, `MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON TRUE WHEN MATCHED THEN DELETE`); err != nil {
				t.Fatal(err)
			}
			ctx, execution := st.(*statementImpl).conn.executionContext(h.ctx)
			if execution == nil {
				t.Fatal("missing execution owner")
			}
			defer execution.finish()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			calls := 0
			execution.client.AddHook(&unmanagedReplaceHook{after: func(_ context.Context, cmd goredis.Cmder) error {
				if cmd.Name() == "eval" && len(cmd.Args()) > 1 && strings.Contains(cmd.Args()[1].(string), "redis.call('DEL',KEYS[1])") {
					calls++
					if stop {
						cancel()
						return nil
					}
					return errors.New("injected lost DELETE reply")
				}
				return nil
			}})
			if _, err := st.ExecuteUpdate(ctx); err == nil {
				t.Fatal("failure hidden")
			}
			if calls != 1 {
				t.Fatalf("DELETE calls=%d", calls)
			}
			if n := f.raw.Exists(h.ctx, f.prefix+"one", f.prefix+"two", f.prefix+"three").Val(); n != 2 {
				t.Fatalf("remaining rows=%d", n)
			}
		})
	}
}

func TestLiveMergeUpdateFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"lost_reply", "composed_reply", "vanished", "permission", "registration"} {
		t.Run(mode, func(t *testing.T) {
			f := liveMergeFixture(t)
			h := f.h
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, `MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON TRUE WHEN MATCHED THEN UPDATE SET n=99`); err != nil {
				t.Fatal(err)
			}
			ctx, execution := st.(*statementImpl).conn.executionContext(h.ctx)
			if execution == nil {
				t.Fatal("missing execution owner")
			}
			defer execution.finish()
			calls := 0
			hook := &unmanagedReplaceHook{}
			hook.before = func(_ context.Context, cmd goredis.Cmder) error {
				if !replaceScript(cmd, keyRowUpdateLua) {
					return nil
				}
				calls++
				if mode == "vanished" {
					return f.raw.Del(h.ctx, cmd.Args()[3].(string)).Err()
				}
				if mode == "permission" {
					return errors.New("NOPERM injected source UPDATE refusal")
				}
				return nil
			}
			hook.after = func(_ context.Context, cmd goredis.Cmder) error {
				if !replaceScript(cmd, keyRowUpdateLua) {
					return nil
				}
				switch mode {
				case "registration":
					raw := f.raw.HGet(h.ctx, liveRegistry, "it_live_merge").Val()
					var r liveRecord
					if err := json.Unmarshal([]byte(raw), &r); err != nil {
						return err
					}
					r.Meta.LiveVersion = snapshotID()
					value, err := json.Marshal(r)
					if err != nil {
						return err
					}
					return f.raw.HSet(h.ctx, liveRegistry, "it_live_merge", value).Err()
				case "lost_reply":
					return errors.New("injected lost UPDATE reply")
				case "composed_reply":
					return errors.Join(goredis.Nil, errors.New("injected lost UPDATE reply"))
				}
				return nil
			}
			execution.client.AddHook(hook)
			if _, err := st.ExecuteUpdate(ctx); err == nil {
				t.Fatal("failure hidden")
			}
			if calls != 1 {
				t.Fatalf("UPDATE calls=%d", calls)
			}
			changes := 0
			remaining := 0
			for _, suffix := range []string{"one", "two", "three"} {
				key := f.prefix + suffix
				if f.raw.Exists(h.ctx, key).Val() == 1 {
					remaining++
				}
				if f.raw.HGet(h.ctx, key, "n").Val() == "99" {
					changes++
				}
			}
			want := 1
			if mode == "vanished" || mode == "permission" {
				want = 0
			}
			if changes != want {
				t.Fatalf("applied updates=%d want %d", changes, want)
			}
			if mode == "vanished" && remaining != 2 {
				t.Fatal("UPDATE recreated vanished HASH")
			}
		})
	}
}

func TestLiveMergeLiveSourceAndBinaryKeys(t *testing.T) {
	f := liveMergeFixture(t)
	h := f.h
	key := f.prefix + string([]byte{'b', 0, 'k'})
	if !strings.ContainsRune(key, 0) {
		t.Fatal("binary fixture lacks zero byte")
	}
	f.hset(key, "id", "7", "n", "70")
	h.expectAffected(`MERGE INTO redis_live.it_live_merge t USING redis_live.it_live_merge s ON t.id=s.id WHEN MATCHED THEN UPDATE SET n=s.n+1`, 4)
	if got := f.raw.HGet(h.ctx, key, "n").Val(); got != "71" {
		t.Fatalf("binary HASH value %q", got)
	}
	h.expectRows(`MERGE INTO redis_live.it_live_merge t USING (SELECT '`+key+`' AS k) s ON t.__key=s.k WHEN MATCHED THEN UPDATE SET n=72 RETURNING t.__key,t.n`, key+"|72")
	schema := h.expectRows(`SELECT t.* FROM redis_live.it_live_merge t JOIN (SELECT 7 AS id) s ON t.id=s.id`, "7|72")
	for _, field := range schema.Fields() {
		if strings.Contains(field.Name, "__key") {
			t.Fatal("star exposed application key")
		}
	}
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON TRUE WHEN MATCHED THEN UPDATE SET id=NULL,n=NULL`, "without fields")
	h.expectError(`MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON t.id=s.id WHEN MATCHED THEN UPDATE SET __key='other'`, "__key")
}

// A nil sentinel joined to transport loss is still a failed preflight. It must
// not be mistaken for a successfully observed NULL before a live mutation.
type liveMergePreflightLoss struct{ snapshotHook }

func (h *liveMergePreflightLoss) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			if cmd.Name() == "hlen" {
				return errors.Join(goredis.Nil, errors.New("injected preflight reply loss"))
			}
		}
		return nil
	}
}
func TestLiveMergePreflightComposedReply(t *testing.T) {
	f := liveMergeFixture(t)
	h := f.h
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, `MERGE INTO redis_live.it_live_merge t USING (SELECT 1 AS id) s ON TRUE WHEN MATCHED THEN UPDATE SET id=NULL`); err != nil {
		t.Fatal(err)
	}
	ctx, execution := st.(*statementImpl).conn.executionContext(h.ctx)
	if execution == nil {
		t.Fatal("missing execution owner")
	}
	defer execution.finish()
	execution.client.AddHook(&liveMergePreflightLoss{})
	if _, err := st.ExecuteUpdate(ctx); err == nil {
		t.Fatal("composed preflight loss hidden")
	}
	for _, suffix := range []string{"one", "two", "three"} {
		if !f.raw.HExists(h.ctx, f.prefix+suffix, "id").Val() {
			t.Fatal("source changed after failed preflight")
		}
	}
}
