// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type unmanagedProjectionReadHook struct {
	t     *testing.T
	calls int
	names []string
	step  func(int, []goredis.Cmder) error
}

func (*unmanagedProjectionReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *unmanagedProjectionReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(context.Context, goredis.Cmder) error {
		return errors.New("unexpected nonpipeline projection command")
	}
}
func (h *unmanagedProjectionReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		h.calls++
		for _, cmd := range cmds {
			h.names = append(h.names, cmd.Name())
			if strings.HasPrefix(cmd.Name(), "eval") {
				if n, ok := cmd.(interface{ NoRetry() bool }); !ok || !n.NoRetry() {
					h.t.Fatal("projection read lost no-retry marker")
				}
			}
		}
		return h.step(h.calls, cmds)
	}
}
func projectionReadUnit(t *testing.T, fields ...string) (*unmanagedProjectionRead, *unmanagedProjectionReadHook) {
	h := &unmanagedProjectionReadHook{t: t}
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	c.AddHook(h)
	t.Cleanup(func() { _ = c.Close() })
	return &unmanagedProjectionRead{store: &store{client: c}, fields: fields, bytes: len(unmanagedProjectionReadLua) + 64}, h
}
func setProjectionReply(cmd goredis.Cmder, value any, err error) {
	cmd.(onceCmd).SetVal(value)
	cmd.SetErr(err)
}
func TestUnmanagedProjectionReadExactAndColdCache(t *testing.T) {
	r, h := projectionReadUnit(t, "n", "text")
	h.step = func(n int, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if n == 1 {
				err := controlScriptNativeError("NOSCRIPT missing")
				setProjectionReply(cmd, nil, err)
				return err
			}
			want := "evalsha_ro"
			if n == 2 {
				want = "eval_ro"
			}
			if cmd.Name() != want {
				t.Fatal(cmd.Name(), want)
			}
			if cmd.Args()[2] != 1 {
				t.Fatal("multiple keys", cmd.Args())
			}
			key := cmd.Args()[3].(string)
			if key == "app:null" {
				setProjectionReply(cmd, []any{nil, nil}, nil)
			} else if key == "app:gone" {
				setProjectionReply(cmd, nil, goredis.Nil)
			} else {
				setProjectionReply(cmd, []any{"9007199254740993", string([]byte{'a', 0, 'b'})}, nil)
			}
		}
		return nil
	}
	got, err := r.read(context.Background(), []string{"app:one", "app:null", "app:gone"})
	want := []aggRow{{keyField: "app:one", "n": "9007199254740993", "text": string([]byte{'a', 0, 'b'})}, {keyField: "app:null"}, nil}
	if err != nil || !reflect.DeepEqual(got, want) || h.calls != 3 || r.fallback {
		t.Fatal(got, err, h.calls)
	}
}
func TestUnmanagedProjectionReadPermissionCompatibility(t *testing.T) {
	for _, message := range []string{"ADBC_UNMANAGED_PROJECTION_TYPE_DENIED", "ADBC_UNMANAGED_PROJECTION_HMGET_DENIED", "NOPERM this user has no permissions to run the 'evalsha_ro' command"} {
		t.Run(message, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			h.step = func(n int, cmds []goredis.Cmder) error {
				if n == 1 {
					err := controlScriptNativeError(message)
					setProjectionReply(cmds[0], nil, err)
					return err
				}
				for _, cmd := range cmds {
					if cmd.Name() != "hgetall" {
						t.Fatal("retried denied script", cmd.Name())
					}
					cmd.(*goredis.MapStringStringCmd).SetVal(map[string]string{"n": "7"})
				}
				return nil
			}
			got, err := r.read(context.Background(), []string{"app:1", "app:2"})
			if err != nil || len(got) != 2 || got[1]["n"] != "7" || !r.fallback || h.calls != 3 {
				t.Fatal(got, err, h.calls, r.fallback)
			}
		})
	}
}
func TestUnmanagedProjectionReadUnknownDoesNotFallback(t *testing.T) {
	for _, kind := range []string{"unknown", "mixed_unknown_and_noscript", "pipeline_error_after_replies", "unaccounted_native_pipeline_error", "untyped_permission", "ordinary_server_error", "malformed", "body_missing_script"} {
		t.Run(kind, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			r.prepared = true
			h.step = func(n int, cmds []goredis.Cmder) error {
				for _, cmd := range cmds {
					setProjectionReply(cmd, []any{"7"}, nil)
				}
				var err error
				switch kind {
				case "unknown":
					err = io.ErrUnexpectedEOF
					cmds[0].SetErr(err)
				case "mixed_unknown_and_noscript":
					setProjectionReply(cmds[0], nil, controlScriptNativeError("NOSCRIPT missing"))
					err = io.ErrUnexpectedEOF
					cmds[1].SetErr(err)
				case "pipeline_error_after_replies":
					err = io.ErrUnexpectedEOF
				case "unaccounted_native_pipeline_error":
					setProjectionReply(cmds[0], nil, goredis.Nil)
					err = controlScriptNativeError("NOSCRIPT unaccounted pipeline error")
				case "untyped_permission":
					err = errors.New("ADBC_UNMANAGED_PROJECTION_TYPE_DENIED")
					cmds[0].SetErr(err)
				case "ordinary_server_error":
					err = controlScriptNativeError("ERR unexpected script failure")
					cmds[0].SetErr(err)
				case "malformed":
					setProjectionReply(cmds[0], []any{int64(7)}, nil)
				case "body_missing_script":
					err = controlScriptNativeError("NOSCRIPT missing")
					cmds[0].SetErr(err)
				}
				return err
			}
			var err error
			if kind == "body_missing_script" {
				_, _, err = r.batch(context.Background(), []string{"app:1", "app:2"}, true)
			} else {
				_, err = r.read(context.Background(), []string{"app:1", "app:2"})
			}
			if err == nil || h.calls != 1 || r.fallback {
				t.Fatal("unknown result replayed or accepted", kind, err, h.calls)
			}
		})
	}
}
func TestUnmanagedProjectionReadBoundsAndZeroFields(t *testing.T) {
	r, h := projectionReadUnit(t)
	h.step = func(_ int, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			setProjectionReply(cmd, []any{}, nil)
		}
		return nil
	}
	rows, err := r.read(context.Background(), []string{"app:empty-projection"})
	if err != nil || len(rows) != 1 || rows[0][keyField] != "app:empty-projection" {
		t.Fatal(rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.read(ctx, []string{"app:1"}); err == nil || h.calls != 1 {
		t.Fatal("canceled read reached transport")
	}
	if _, err = r.read(context.Background(), []string{metaKey("public", "secret")}); err == nil || h.calls != 1 {
		t.Fatal("driver storage reached source script")
	}
	it, _ := unmanagedProjectionFixture()
	need := map[string]bool{}
	for i := 0; i < 257; i++ {
		name := fmt.Sprint("f", i)
		it.meta.Columns = append(it.meta.Columns, columnMeta{Name: name})
		need[name] = true
	}
	if newUnmanagedProjectionRead(r.store, it.meta, need) != nil {
		t.Fatal("unbounded field list optimized")
	}
}

func TestUnmanagedProjectionReadRequestByteBound(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	r.prepared = true
	r.bytes = unmanagedProjectionReadBytes/2 + 1
	h.step = func(_ int, cmds []goredis.Cmder) error {
		if len(cmds) != 1 {
			t.Fatal("projection pipeline exceeded request byte bound", len(cmds))
		}
		setProjectionReply(cmds[0], []any{"7"}, nil)
		return nil
	}
	got, err := r.read(context.Background(), []string{"app:1", "app:2", "app:3"})
	if err != nil || len(got) != 3 || h.calls != 3 {
		t.Fatal(got, err, h.calls)
	}
}

func TestUnmanagedProjectionReadColdBodyPermissionFallback(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	h.step = func(n int, cmds []goredis.Cmder) error {
		switch n {
		case 1:
			err := controlScriptNativeError("NOSCRIPT missing")
			setProjectionReply(cmds[0], nil, err)
			return err
		case 2:
			if cmds[0].Name() != "eval_ro" {
				t.Fatal("cold fallback did not use read-only full body")
			}
			err := controlScriptNativeError("NOPERM this user has no permissions to run the 'eval_ro' command")
			setProjectionReply(cmds[0], nil, err)
			return err
		default:
			cmds[0].(*goredis.MapStringStringCmd).SetVal(map[string]string{"n": "7"})
			return nil
		}
	}
	got, err := r.read(context.Background(), []string{"app:one"})
	if err != nil || len(got) != 1 || got[0]["n"] != "7" || !r.fallback || h.calls != 3 {
		t.Fatal(got, err, h.calls, r.fallback)
	}
}
func TestUnmanagedProjectionReadSourceAddressWins(t *testing.T) {
	r, h := projectionReadUnit(t, keyField)
	h.step = func(_ int, cmds []goredis.Cmder) error {
		setProjectionReply(cmds[0], []any{"application owned"}, nil)
		return nil
	}
	got, err := r.read(context.Background(), []string{"app:real"})
	if err != nil || got[0][keyField] != "app:real" {
		t.Fatal("application field shadowed source address", got, err)
	}
}

func TestUnmanagedProjectionReadWrappedAbsenceIsUnknown(t *testing.T) {
	for _, kind := range []string{"command_joined_nil", "command_wrapped_nil", "pipeline_joined_nil", "pipeline_wrapped_nil", "noscript_then_joined_nil", "permission_then_joined_nil", "wrapped_native_permission", "joined_native_noscript", "pipeline_joined_native_noscript", "pipeline_wrapped_native_permission"} {
		t.Run(kind, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			r.prepared = true
			unknown := error(errors.Join(goredis.Nil, io.EOF))
			if strings.Contains(kind, "wrapped_nil") {
				unknown = fmt.Errorf("wrapped missing marker: %w", goredis.Nil)
			}
			h.step = func(n int, cmds []goredis.Cmder) error {
				if n > 1 {
					for _, cmd := range cmds {
						cmd.SetErr(io.ErrUnexpectedEOF)
					}
					return io.ErrUnexpectedEOF
				}
				for _, cmd := range cmds {
					setProjectionReply(cmd, []any{"7"}, nil)
				}
				switch kind {
				case "command_joined_nil", "command_wrapped_nil":
					cmds[0].SetErr(unknown)
					return unknown
				case "pipeline_joined_nil", "pipeline_wrapped_nil":
					cmds[0].SetErr(goredis.Nil)
					return unknown
				case "noscript_then_joined_nil":
					known := controlScriptNativeError("NOSCRIPT known refusal")
					cmds[0].SetErr(known)
					cmds[1].SetErr(unknown)
					return known
				case "permission_then_joined_nil":
					known := controlScriptNativeError("ADBC_UNMANAGED_PROJECTION_TYPE_DENIED")
					cmds[0].SetErr(known)
					cmds[1].SetErr(unknown)
					return known
				case "pipeline_joined_native_noscript":
					known := controlScriptNativeError("NOSCRIPT known refusal")
					cmds[0].SetErr(known)
					unknown = errors.Join(known, io.EOF)
					return unknown
				case "pipeline_wrapped_native_permission":
					known := controlScriptNativeError("ADBC_UNMANAGED_PROJECTION_TYPE_DENIED")
					cmds[0].SetErr(known)
					unknown = fmt.Errorf("unknown result wrapping native permission: %w", known)
					return unknown
				case "wrapped_native_permission":
					unknown = fmt.Errorf("unknown result wrapping native permission: %w", controlScriptNativeError("ADBC_UNMANAGED_PROJECTION_TYPE_DENIED"))
					cmds[0].SetErr(unknown)
					return unknown
				case "joined_native_noscript":
					unknown = errors.Join(controlScriptNativeError("NOSCRIPT known refusal"), io.EOF)
					cmds[0].SetErr(unknown)
					return unknown
				}
				t.Fatal("missing unknown outcome fixture")
				return nil
			}
			rows, err := r.read(context.Background(), []string{"app:one", "app:two"})
			if err == nil || h.calls != 1 || r.fallback || rows != nil {
				t.Fatalf("unknown outcome accepted/replayed: rows=%v error=%v calls=%d fallback=%v", rows, err, h.calls, r.fallback)
			}
			if !strings.Contains(err.Error(), unknown.Error()) {
				t.Fatalf("original unknown diagnostic lost: got=%v want=%v", err, unknown)
			}
		})
	}
}
