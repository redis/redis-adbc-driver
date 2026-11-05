// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type controlScriptWireObserver struct {
	enabled   atomic.Bool
	mu        sync.Mutex
	names     []string
	bodyBytes []int
	suffixes  [][]any
	successes int
	missing   int
	denied    int
	noRetry   bool
}

func (*controlScriptWireObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*controlScriptWireObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (o *controlScriptWireObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		a := cmd.Args()
		if !o.enabled.Load() || len(a) < 3 || !((strings.EqualFold(fmt.Sprint(a[0]), "eval") && a[1] == snapshotControl) ||
			(strings.EqualFold(fmt.Sprint(a[0]), "evalsha") && a[1] == snapshotControlDigest)) {
			return err
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		o.names = append(o.names, strings.ToUpper(fmt.Sprint(a[0])))
		o.bodyBytes = append(o.bodyBytes, len(fmt.Sprint(a[1])))
		o.suffixes = append(o.suffixes, append([]any(nil), a[2:]...))
		if marker, ok := cmd.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
			o.noRetry = true
		}
		if err == nil {
			o.successes++
		}
		missing, denied := snapshotControlScriptRefusal(err)
		if missing {
			o.missing++
		}
		if denied {
			o.denied++
		}
		return err
	}
}
func TestSnapshotControlScriptCacheColdHotAndFlush(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT)")
	h.exec("INSERT INTO t VALUES(9007199254740993)")
	s := snapshotTestStore(t, raw)
	observer := &controlScriptWireObserver{}
	s.client.AddHook(observer) // installed before the heartbeat can share this client
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { observer.enabled.Store(false); pin.release() }()
	observer.enabled.Store(true)
	valid := func() {
		t.Helper()
		n, err := s.snapshotControl(ctx, "valid", pin.token, "", pin.root).Int()
		if err != nil || n != 1 {
			t.Fatal("cached control validation failed", err)
		}
	}
	if err := raw.ScriptFlush(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	valid()
	valid()
	if err := raw.ScriptFlush(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	valid()
	func() {
		observer.mu.Lock()
		defer observer.mu.Unlock()
		if !reflect.DeepEqual(observer.names, []string{"EVALSHA", "EVAL", "EVALSHA", "EVALSHA", "EVAL"}) ||
			observer.successes != 3 || observer.missing != 2 || observer.denied != 0 || observer.noRetry {
			t.Fatal("unexpected cold/hot/flush execution counts", observer.names, observer.successes, observer.missing)
		}
		for i, n := range observer.bodyBytes {
			want := len(snapshotControlDigest)
			if observer.names[i] == "EVAL" {
				want = len(snapshotControl)
			}
			if n != want {
				t.Fatal("unexpected script payload size")
			}
			if !reflect.DeepEqual(observer.suffixes[0], observer.suffixes[i]) {
				t.Fatal("cache fallback changed declared keys or arguments")
			}
		}
	}()
	h.expectRows("SELECT v FROM t", "9007199254740993")
}

func TestSnapshotControlScriptEvalOnlySoftware(t *testing.T) {
	uri := os.Getenv("REDIS_SCRIPT_EVAL_ONLY_URI")
	if uri == "" {
		t.Skip("requires an owned Software EVAL-only operator fixture")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT)")
	h.exec("INSERT INTO t VALUES(9007199254740993)")
	opts, err := goredis.ParseURL(uri)
	if err != nil || opts.Addr != raw.Options().Addr || opts.DB != raw.Options().DB || opts.Username == "" || raw.Options().TLSConfig == nil {
		t.Fatal("private operator endpoint differs from the owned strict-TLS fixture")
	}
	opts.TLSConfig = raw.Options().TLSConfig.Clone()
	opts.ContextTimeoutEnabled = true
	client := goredis.NewClient(opts)
	defer client.Close()
	client.AddHook(snapshotHook{})
	observer := &controlScriptWireObserver{}
	observer.enabled.Store(true)
	client.AddHook(observer)
	operatorStore := &store{client: client, snapshots: true}
	token := snapshotID()
	root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
	if err != nil {
		t.Fatal(err)
	}
	admin := snapshotTestStore(t, raw)
	defer func() {
		if err := admin.snapshotControl(h.ctx, "release", token, "", root).Err(); err != nil {
			t.Error("operator lease cleanup failed")
		}
	}()
	got, err := operatorStore.snapshotControl(h.ctx, "acquire", token, "", "").Text()
	if err != nil || got != root {
		t.Fatal("EVAL-only operator acquisition failed; inspect the private native refusal")
	}
	for _, op := range []string{"valid", "renew", "release"} {
		n, err := operatorStore.snapshotControl(h.ctx, op, token, "", root).Int()
		if err != nil || n != 1 {
			t.Fatal("EVAL-only operator lifecycle failed")
		}
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if !reflect.DeepEqual(observer.names, []string{"EVALSHA", "EVAL", "EVAL", "EVAL", "EVAL"}) ||
		observer.denied != 1 || observer.successes != 4 || observer.noRetry || !operatorStore.controlScript.evalOnly.Load() {
		t.Fatal("EVAL-only operator did not retain legacy one-execution behavior")
	}
}

// Denying HGETALL proves the lease operation uses point lookups. This operator
// is deliberately not a SQL reader; other catalog operations still need scans.
func TestSnapshotControlScriptPointLeaseSoftware(t *testing.T) {
	uri := os.Getenv("REDIS_SCRIPT_POINT_LEASE_URI")
	if uri == "" {
		t.Skip("requires an owned Software operator without HGETALL")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT)")
	admin := snapshotTestStore(t, raw)
	token, work := snapshotID(), snapshotID()
	root, err := admin.snapshotControl(h.ctx, "acquire", token, "", "").Text()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := admin.snapshotControl(h.ctx, "abort", token, work, root).Err(); err != nil {
			t.Error("private lease workspace cleanup failed")
		}
		if err := admin.snapshotControl(h.ctx, "release", token, "", root).Err(); err != nil {
			t.Error("private lease cleanup failed")
		}
	}()
	if n, err := admin.snapshotControl(h.ctx, "reserve", token, work, root).Int(); err != nil || n != 1 {
		t.Fatal("private lease workspace reservation failed")
	}
	opts, err := goredis.ParseURL(uri)
	if err != nil || opts.Addr != raw.Options().Addr || opts.DB != raw.Options().DB || opts.Username == "" || raw.Options().TLSConfig == nil {
		t.Fatal("point-lease operator differs from owned strict-TLS endpoint")
	}
	opts.TLSConfig = raw.Options().TLSConfig.Clone()
	opts.ContextTimeoutEnabled = true
	client := goredis.NewClient(opts)
	defer client.Close()
	client.AddHook(snapshotHook{})
	operator := &store{client: client, snapshots: true}
	if err := client.HGetAll(h.ctx, snapshotCatalogKey).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("point-lease fixture did not deny HGETALL")
	}
	check := func(op, id, base string, want int) {
		t.Helper()
		if n, err := operator.snapshotControl(h.ctx, op, token, id, base).Int(); err != nil || n != want {
			t.Fatalf("point-lease %s: got %d, want %d; native error: %v", op, n, want, err)
		}
	}
	before := func(field string) string {
		t.Helper()
		value, err := raw.HGet(h.ctx, snapshotCatalogKey, field).Result()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	pinBefore := before("p:" + token)
	check("valid", "", root, 1)
	check("valid", "", snapshotID(), 0)
	check("renew", work, snapshotID(), 0)
	if before("p:"+token) != pinBefore {
		t.Fatal("validation or wrong-root renewal changed the lease")
	}
	check("renew", work, root, 1)
	check("valid", "", root, 1)
	// Only mutate this fixture's own private writer record. An expired writer
	// must not be resurrected even while its base reader lease is still live.
	var writer map[string]any
	if err := json.Unmarshal([]byte(before("w:"+work)), &writer); err != nil {
		t.Fatal(err)
	}
	writer["expires"] = 0
	encoded, err := json.Marshal(writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+work, encoded).Err(); err != nil {
		t.Fatal(err)
	}
	check("renew", work, root, 0)
	if before("w:"+work) != string(encoded) {
		t.Fatal("expired private writer was renewed")
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+token, `{"root":"`+root+`","expires":0}`).Err(); err != nil {
		t.Fatal(err)
	}
	check("valid", "", root, 0)
	check("renew", "", root, 0)
	check("release", "", root, 1)
	check("release", "", root, 1) // preserve idempotent cleanup
}

func TestSnapshotControlScriptPointLeaseKeepsAdmissionBounds(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	var tokens []string
	defer func() {
		for _, token := range tokens {
			if err := s.snapshotControl(h.ctx, "release", token, "", "").Err(); err != nil {
				t.Error("fixture pin cleanup failed")
			}
		}
	}()
	acquire := func() (string, error) {
		token := snapshotID()
		tokens = append(tokens, token)
		return s.snapshotControl(h.ctx, "acquire", token, "", "").Text()
	}
	root, err := acquire()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 128; i++ {
		if got, err := acquire(); err != nil || got != root {
			t.Fatal("fixture did not fill the unchanged reader bound")
		}
	}
	full := func() {
		t.Helper()
		if _, err := acquire(); err == nil || !strings.Contains(err.Error(), "snapshot reader limit (128) reached") {
			t.Fatal("reader admission did not preserve capacity 128")
		}
	}
	full()
	for _, token := range tokens[1:3] {
		if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+token, `{"root":"`+root+`","expires":0}`).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []string{"valid", "renew"} {
		if n, err := s.snapshotControl(h.ctx, op, tokens[0], "", root).Int(); err != nil || n != 1 {
			t.Fatal("live point lease failed")
		}
	}
	for _, token := range tokens[1:3] {
		if exists, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Result(); err != nil || !exists {
			t.Fatal("point operation unexpectedly pruned an unrelated pin")
		}
	}
	for range 2 {
		if got, err := acquire(); err != nil || got != root {
			t.Fatal("admission did not reclaim expired reader slots")
		}
	}
	for _, token := range tokens[1:3] {
		if exists, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Result(); err != nil || exists {
			t.Fatal("admission retained expired pin records")
		}
	}
	full()
}
