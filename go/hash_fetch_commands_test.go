// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestHashFetchCommandArgumentsAndOwnership(t *testing.T) {
	ctx := context.Background()
	fields := []string{"a", "a", "nul\x00λ", ""}
	keys := []string{"key\x00one", "", "key:two"}
	client := goredis.NewClient(&goredis.Options{})
	defer client.Close()
	pipe := client.Pipeline()
	got := queueHashFetchCommands(ctx, pipe, keys, fields)
	if len(got) != len(keys) || got[1] != nil || pipe.Len() != 2 {
		t.Fatal("blank key changed command alignment", got, pipe.Len())
	}
	for _, i := range []int{0, 2} {
		baselinePipe := client.Pipeline()
		want := baselinePipe.HMGet(ctx, keys[i], fields...)
		if !reflect.DeepEqual(got[i], want) || got[i].Name() != want.Name() || got[i].FullName() != want.FullName() {
			t.Fatal("command metadata/argument semantics differ", got[i].Args(), want.Args())
		}
		// Full command equality also checks the private first-key/step routing
		// defaults against HMGet; arguments alone would not cover those fields.
	}
	fields[0] = "changed source"
	got[0].Args()[2] = "changed hook argument"
	got[0].Args()[1] = "changed key"
	got[0].SetVal([]any{"first"})
	got[2].SetVal([]any{"second"})
	if got[2].Args()[2] != "a" || got[2].Args()[1] != keys[2] || got[2].Val()[0] != "second" {
		t.Fatal("commands share mutable argument/result slots")
	}
	got[0].Val()[0] = "changed result"
	if got[2].Val()[0] != "second" {
		t.Fatal("command result was overwritten")
	}
}

// A raw RESP peer checks actual encoded field/key bytes and ordered replies;
// this is a transport fixture, not a substitute Redis implementation benchmark.
func TestHashFetchCommandsWireAndPipelineErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			socket, server := net.Pipe()
			done := make(chan struct{})
			seen := make(chan []string, 2)
			go func() {
				defer close(done)
				defer server.Close()
				rd := bufio.NewReader(server)
				for {
					args, err := readRESPArray(rd)
					if err != nil {
						return
					}
					reply := "+OK\r\n"
					switch strings.ToUpper(args[0]) {
					case "HELLO":
						reply = "%1\r\n+proto\r\n:3\r\n"
					case "HMGET":
						seen <- args
						if fail {
							reply = "-ERR deliberate fetch failure\r\n"
						} else {
							reply = fmt.Sprintf("*%d\r\n", len(args)-2)
							for _, field := range args[2:] {
								reply += fmt.Sprintf("$%d\r\n%s\r\n", len(field), field)
							}
						}
					}
					if _, err := server.Write([]byte(reply)); err != nil {
						return
					}
				}
			}()
			client := goredis.NewClient(&goredis.Options{Addr: "hash-pipe:1", Protocol: 3, DisableIdentity: true, MaxRetries: -1,
				Dialer: func(context.Context, string, string) (net.Conn, error) { return socket, nil }})
			t.Cleanup(func() {
				_ = client.Close()
				_ = server.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("HASH RESP peer did not stop")
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			fields := []string{"repeat", "repeat", "nul\x00λ", ""}
			keys := []string{"key\x00one", "", "key:two"}
			cmds, err := (&store{client: client}).fetchHashCommands(ctx, keys, fields)
			if fail {
				if err == nil || !strings.Contains(err.Error(), "deliberate fetch failure") {
					t.Fatal("pipeline error lost", err)
				}
			} else {
				if err != nil || len(cmds) != 3 || cmds[1] != nil {
					t.Fatal("fetch failed or lost blank slot", err)
				}
				for _, i := range []int{0, 2} {
					values, err := cmds[i].Result()
					if err != nil || len(values) != len(fields) {
						t.Fatal("reply width/error differs", values, err)
					}
					for j := range fields {
						if values[j] != fields[j] {
							t.Fatal("reply order/value differs", values)
						}
					}
				}
			}
			for _, i := range []int{0, 2} {
				select {
				case args := <-seen:
					want := append([]string{"hmget", keys[i]}, fields...)
					if !reflect.DeepEqual(args, want) {
						t.Fatal("wire bytes/order differ", args, want)
					}
				case <-ctx.Done():
					t.Fatal("missing wire command", ctx.Err())
				}
			}
		})
	}
}

type hashFetchErrorHook struct{ err error }

func (h hashFetchErrorHook) DialHook(next goredis.DialHook) goredis.DialHook          { return next }
func (h hashFetchErrorHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook { return next }
func (h hashFetchErrorHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			cmd.SetErr(h.err)
		}
		return h.err
	}
}

func TestHashFetchCommandsPipelineHookError(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{})
	defer client.Close()
	want := errors.New("hook rejected batch")
	client.AddHook(hashFetchErrorHook{want})
	_, err := (&store{client: client}).fetchHashCommands(context.Background(), []string{"row"}, []string{"field"})
	if !strings.Contains(fmt.Sprint(err), want.Error()) {
		t.Fatal("hook error lost", err)
	}
}

func BenchmarkHashFetchCommandConstruction(b *testing.B) {
	for _, preboxed := range []bool{false, true} {
		b.Run(fmt.Sprintf("preboxed_%t", preboxed), func(b *testing.B) {
			client := goredis.NewClient(&goredis.Options{})
			defer client.Close()
			keys, fields := make([]string, 256), make([]string, 106)
			for i := range keys {
				keys[i] = fmt.Sprintf("row:%d", i)
			}
			for i := range fields {
				fields[i] = fmt.Sprintf("field:%d", i)
			}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				pipe := client.Pipeline()
				var cmds []*goredis.SliceCmd
				if preboxed {
					cmds = queueHashFetchCommands(ctx, pipe, keys, fields)
				} else {
					cmds = queueOriginalHashFetchCommands(ctx, pipe, keys, fields)
				}
				if len(cmds) != len(keys) || pipe.Len() != len(keys) {
					b.Fatal("lost queued command")
				}
				runtime.KeepAlive(cmds)
				pipe.Discard()
			}
		})
	}
}

// Original constructor loop, retained only as the allocation comparison control.
func queueOriginalHashFetchCommands(ctx context.Context, pipe goredis.Pipeliner, keys, fields []string) []*goredis.SliceCmd {
	cmds := make([]*goredis.SliceCmd, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			cmds = append(cmds, nil)
			continue
		}
		cmds = append(cmds, pipe.HMGet(ctx, key, fields...))
	}
	return cmds
}
