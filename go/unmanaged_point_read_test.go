// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type unmanagedPointReadHook struct {
	t                      *testing.T
	rows                   map[string]map[string]string
	commands               []string
	hmgetError, outerError error
}

func (*unmanagedPointReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *unmanagedPointReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(context.Context, goredis.Cmder) error { return errors.New("unexpected scalar point command") }
}
func (h *unmanagedPointReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		var first error
		source := false
		for _, wrapped := range cmds {
			cmd := wrapped
			if c, ok := cmd.(unmanagedProjectionCmd); ok {
				cmd = c.Cmder
			}
			switch cmd.Name() {
			case "hmget":
				source = true
				h.commands = append(h.commands, "hmget")
				if n, ok := wrapped.(interface{ NoRetry() bool }); !ok || !n.NoRetry() {
					h.t.Fatal("point projection lost no-retry marker")
				}
				args := cmd.Args()
				if h.hmgetError != nil {
					cmd.SetErr(h.hmgetError)
					if first == nil {
						first = h.hmgetError
					}
					continue
				}
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
				source = true
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
		if source && h.outerError != nil {
			return h.outerError
		}
		return first
	}
}

func TestUnmanagedPointReadExactFallbacksAndErrors(t *testing.T) {
	permission := controlScriptNativeError("NOPERM this user has no permissions to run the 'hmget' command")
	for _, test := range []struct {
		name                string
		row                 map[string]string
		hmget, outer        error
		wantRows            int
		wantNull, wantError bool
		commands            []string
	}{
		{"all-null-existing", map[string]string{"unregistered": "present"}, nil, nil, 1, true, false, []string{"hmget", "hgetall"}},
		{"missing", nil, nil, nil, 0, false, false, []string{"hmget", "hgetall"}},
		{"native-command-permission", map[string]string{"stored_big": "9223372036854775807"}, permission, nil, 1, false, false, []string{"hmget", "hgetall"}},
		{"joined-outer-permission", map[string]string{"stored_big": "1"}, permission, errors.Join(permission, io.ErrUnexpectedEOF), 0, false, true, []string{"hmget"}},
		{"lost-success-reply", map[string]string{"stored_big": "1"}, nil, io.ErrUnexpectedEOF, 0, false, true, []string{"hmget"}},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/materialized", true: "/stream"}[stream], func(t *testing.T) {
				fixture, _ := unmanagedProjectionFixture()
				meta := fixture.meta
				h := &unmanagedPointReadHook{t: t, rows: map[string]map[string]string{"app:one": test.row}, hmgetError: test.hmget, outerError: test.outer}
				c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
				c.AddHook(h)
				defer c.Close()
				e := &executor{store: &store{client: c}}
				req := scanRequest{meta: meta, where: wherePlan{keys: []string{"app:one"}}, need: map[string]bool{"big": true}}
				var rows []map[string]Value
				var err error
				if stream {
					it, openErr := e.openScan(context.Background(), req, nil)
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer it.close(context.Background())
					_, rows, err = it.next(context.Background())
				} else {
					_, rows, err = e.scan(context.Background(), req, nil)
				}
				if (err != nil) != test.wantError || len(rows) != test.wantRows || !reflect.DeepEqual(h.commands, test.commands) {
					t.Fatal(rows, err, h.commands)
				}
				if test.wantRows == 1 && (rows[0]["big"].Null != test.wantNull || !test.wantNull && rows[0]["big"].I != 9223372036854775807) {
					t.Fatal("changed exact fallback value", rows)
				}
			})
		}
	}
}

func TestUnmanagedPointReadEligibility(t *testing.T) {
	for _, mode := range []string{"managed", "foreign", "foreign-descriptor", "flex", "rowid", "missing-value-markers", "eligible"} {
		t.Run(mode, func(t *testing.T) {
			it, _ := unmanagedProjectionFixture()
			meta := it.meta
			s := &store{}
			switch mode {
			case "managed":
				meta.Unmanaged = false
			case "foreign":
				meta.Foreign = true
			case "foreign-descriptor":
				meta.UnmanagedForeign = true
			case "flex":
				s.flex = true
			case "rowid":
				meta.KeyRows = false
			case "missing-value-markers":
				meta.Columns[0].MissingThrough = 1
			}
			if got := newUnmanagedPointRead(s, meta, map[string]bool{"big": true}); (got != nil) != (mode == "eligible") {
				t.Fatal("point eligibility changed", mode, got)
			}
		})
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

func TestUnmanagedPointReadKeepsResidualDependencies(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, value := range []string{"9007199254740993", "9007199254740991"} {
			t.Run(map[bool]string{false: "materialized/", true: "stream/"}[stream]+value, func(t *testing.T) {
				fixture, _ := unmanagedProjectionFixture()
				meta := fixture.meta
				label := string([]byte{'a', 0, 'b'})
				h := &unmanagedPointReadHook{t: t, rows: map[string]map[string]string{"app:one": {"stored_big": value, "stored_label": label}}}
				c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
				c.AddHook(h)
				defer c.Close()
				e := &executor{store: &store{client: c}}
				req := scanRequest{meta: meta, where: wherePlan{keys: []string{"app:one"}, residual: filteredComparison(">", "big", 9007199254740992)}, need: map[string]bool{"label": true}}
				var rows []map[string]Value
				var err error
				if stream {
					it, openErr := e.openScan(context.Background(), req, nil)
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer it.close(context.Background())
					_, rows, err = it.next(context.Background())
				} else {
					_, rows, err = e.scan(context.Background(), req, nil)
				}
				want := 0
				if value == "9007199254740993" {
					want = 1
				}
				if err != nil || len(rows) != want || want == 1 && rows[0]["label"].S != label || !reflect.DeepEqual(h.commands, []string{"hmget"}) {
					t.Fatal("projection lost exact residual/dependency/native bytes", rows, err, h.commands)
				}
			})
		}
	}
}

func TestUnmanagedPointPlannerKeepsPrefixAndForeignRules(t *testing.T) {
	for _, test := range []struct {
		key                   string
		foreign, none, direct bool
	}{
		{"app:one", false, false, true},
		{"app:one:nested", false, true, false},
		{"other:one", false, true, false},
		{"app:one", true, false, false},
	} {
		fixture, _ := unmanagedProjectionFixture()
		meta := fixture.meta
		meta.Foreign = test.foreign
		meta.UnmanagedForeign = test.foreign
		predicate := &Binary{Op: "=", L: &ColumnRef{Name: keyField}, R: &Literal{V: stringValue(test.key)}}
		e := &executor{store: &store{}}
		plan, err := e.planWhere(context.Background(), predicate, meta, nil)
		if err != nil || plan.none != test.none || (plan.keys != nil) != test.direct {
			t.Fatal("changed prefix/foreign membership planning", test, plan, err)
		}
	}
}
