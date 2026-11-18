// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type unmanagedParallelReadHook struct {
	step func(context.Context, []goredis.Cmder) error
}

func (*unmanagedParallelReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*unmanagedParallelReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *unmanagedParallelReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return h.step
}

func parallelReadUnit(t *testing.T, step func(context.Context, []goredis.Cmder) error) *unmanagedProjectionRead {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	c.AddHook(&unmanagedParallelReadHook{step: step})
	t.Cleanup(func() { _ = c.Close() })
	return &unmanagedProjectionRead{store: &store{client: c}, fields: []string{"n"}, bytes: 100, prepared: true, workers: 2}
}

func TestUnmanagedParallelProjectionKeepsExactImages(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	r := parallelReadUnit(t, func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			key := cmd.Args()[1].(string)
			mu.Lock()
			calls[cmd.Name()+":"+key]++
			mu.Unlock()
			if cmd.Name() == "hmget" {
				var value any = "9007199254740993"
				if key == "app:999" {
					value = nil
				}
				setProjectionReply(cmd, []any{value}, nil)
			} else {
				// The later complete image is authoritative after an all-NULL read.
				setProjectionNative(cmd, map[string]string{"n": "9223372036854775807", "extra": "later image"}, nil)
			}
		}
		return nil
	})
	keys := make([]string, 3001)
	for i := range keys {
		keys[i] = "app:" + strconv.Itoa(i)
	}
	rows, err := r.read(context.Background(), keys)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range keys {
		want := "9007199254740993"
		if key == "app:999" {
			want = "9223372036854775807"
		}
		if rows[i][keyField] != key || rows[i]["n"] != want || calls["hmget:"+key] != 1 {
			t.Fatal("missing, duplicated or rounded live image", i, rows[i], calls["hmget:"+key])
		}
	}
	if rows[999]["extra"] != "later image" || calls["hgetall:app:999"] != 1 {
		t.Fatal("all-NULL fallback lost its complete image")
	}
}

func TestUnmanagedParallelProjectionJoinsAfterUnknownReply(t *testing.T) {
	entered := make(chan struct{})
	var active, finished atomic.Int32
	var requests atomic.Int32
	loss := errors.New("unknown projection reply loss")
	r := parallelReadUnit(t, func(ctx context.Context, cmds []goredis.Cmder) error {
		active.Add(1)
		defer active.Add(-1)
		defer finished.Add(1)
		requests.Add(int32(len(cmds)))
		var err error
		if cmds[0].Args()[1] == "app:0" {
			<-entered
			err = loss
		} else {
			close(entered)
			<-ctx.Done()
			err = ctx.Err()
		}
		for _, cmd := range cmds {
			cmd.SetErr(err)
		}
		return err
	})
	keys := make([]string, 2000)
	for i := range keys {
		keys[i] = fmt.Sprintf("app:%d", i)
	}
	rows, err := r.read(context.Background(), keys)
	if rows != nil || err == nil || !strings.Contains(err.Error(), loss.Error()) {
		t.Fatal("unknown outcome was replaced or returned rows", err)
	}
	if active.Load() != 0 || finished.Load() != 2 || requests.Load() != 2000 {
		t.Fatal("workers outlived return or input was replayed", active.Load(), finished.Load(), requests.Load())
	}
}
