// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type unmanagedPointReadHook struct {
	t        *testing.T
	rows     map[string]map[string]string
	commands []string
}

func (*unmanagedPointReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *unmanagedPointReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(context.Context, goredis.Cmder) error { return errors.New("unexpected scalar point command") }
}
func (h *unmanagedPointReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		var first error
		for _, wrapped := range cmds {
			cmd := wrapped
			if c, ok := cmd.(unmanagedProjectionCmd); ok {
				cmd = c.Cmder
			}
			switch cmd.Name() {
			case "hmget":
				h.commands = append(h.commands, "hmget")
				if n, ok := wrapped.(interface{ NoRetry() bool }); !ok || !n.NoRetry() {
					h.t.Fatal("point projection lost no-retry marker")
				}
				args := cmd.Args()
				key := args[1].(string)
				row := h.rows[key]
				values := make([]any, len(args)-2)
				for i, arg := range args[2:] {
					if v, ok := row[arg.(string)]; ok {
						values[i] = v
					}
				}
				cmd.(*goredis.SliceCmd).SetVal(values)
			case "hgetall":
				h.commands = append(h.commands, "hgetall")
				key := cmd.Args()[1].(string)
				copy := map[string]string{}
				for k, v := range h.rows[key] {
					copy[k] = v
				}
				cmd.(*goredis.MapStringStringCmd).SetVal(copy)
			case "hget":
				cmd.SetErr(goredis.Nil)
				if first == nil {
					first = goredis.Nil
				}
			case "sismember":
				cmd.(*goredis.BoolCmd).SetVal(true)
			default:
				h.t.Fatal("unexpected command", cmd.Args())
			}
		}
		return first
	}
}

func TestUnmanagedPointReadUsesSelectedNativeFields(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "materialized", true: "stream"}[stream], func(t *testing.T) {
			it, _ := unmanagedProjectionFixture()
			meta := it.meta
			// Both registered-but-unselected and completely unregistered source fields
			// must stay out of the transferred row image.
			payload := strings.Repeat("wide", 16384)
			key := "app:" + string([]byte{'a', 0, 'b'})
			h := &unmanagedPointReadHook{t: t, rows: map[string]map[string]string{key: {"stored_big": "9007199254740993", "stored_label": payload, "unregistered": payload}}}
			c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
			c.AddHook(h)
			defer c.Close()
			e := &executor{store: &store{client: c}}
			req := scanRequest{meta: meta, where: wherePlan{keys: []string{key}}, need: map[string]bool{"big": true}}
			var keys []string
			var rows []map[string]Value
			var err error
			if stream {
				it, openErr := e.openScan(context.Background(), req, nil)
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer it.close(context.Background())
				keys, rows, err = it.next(context.Background())
			} else {
				keys, rows, err = e.scan(context.Background(), req, nil)
			}
			if err != nil || len(keys) != 1 || len(rows) != 1 || rows[0]["big"].I != 9007199254740993 || rows[0][keyField].S != key {
				t.Fatal(keys, rows, err)
			}
			if !reflect.DeepEqual(h.commands, []string{"hmget"}) {
				t.Fatalf("point path transferred full application HASH: %v", h.commands)
			}
		})
	}
}
