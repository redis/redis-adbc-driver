// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
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
