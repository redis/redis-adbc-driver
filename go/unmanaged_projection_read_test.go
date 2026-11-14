// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
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
			if n, ok := cmd.(interface{ NoRetry() bool }); !ok || !n.NoRetry() {
				h.t.Fatal("read lost no-retry marker")
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
	bytes := 64
	for _, field := range fields {
		bytes += len(field) + 16
	}
	return &unmanagedProjectionRead{store: &store{client: c}, fields: fields, bytes: bytes}, h
}
func setProjectionReply(cmd goredis.Cmder, value []any, err error) {
	cmd.(unmanagedProjectionCmd).Cmder.(*goredis.SliceCmd).SetVal(value)
	cmd.SetErr(err)
}
func setProjectionNative(cmd goredis.Cmder, value map[string]string, err error) {
	cmd.(unmanagedProjectionCmd).Cmder.(*goredis.MapStringStringCmd).SetVal(value)
	cmd.SetErr(err)
}

func TestUnmanagedProjectionReadExactNativeValuesAndAllNull(t *testing.T) {
	r, h := projectionReadUnit(t, "n", "text")
	r.prepared = true
	nul := string([]byte{'a', 0, 'b'})
	h.step = func(n int, cmds []goredis.Cmder) error {
		var first error
		for _, cmd := range cmds {
			key := cmd.Args()[1].(string)
			if n == 1 {
				if cmd.Name() != "hmget" || !reflect.DeepEqual(cmd.Args(), []any{"HMGET", key, "n", "text"}) {
					t.Fatal("not native projected command", cmd.Args())
				}
				switch key {
				case "app:one":
					setProjectionReply(cmd, []any{"9007199254740993", nul}, nil)
				case "app:empty":
					setProjectionReply(cmd, []any{"", nil}, nil)
				case "app:wrong":
					first = controlScriptNativeError("WRONGTYPE Operation against a key holding the wrong kind of value")
					setProjectionReply(cmd, nil, first)
				default:
					setProjectionReply(cmd, []any{nil, nil}, nil)
				}
			} else {
				if cmd.Name() != "hgetall" || key != "app:null" && key != "app:gone" {
					t.Fatal("nonnull value reread", cmd.Args())
				}
				value := map[string]string{}
				if key == "app:null" {
					value["unselected"] = "still a HASH"
				}
				setProjectionNative(cmd, value, nil)
			}
		}
		return first
	}
	got, err := r.read(context.Background(), []string{"app:one", "app:empty", "app:null", "app:gone", "app:wrong"})
	want := []aggRow{{keyField: "app:one", "n": "9007199254740993", "text": nul}, {keyField: "app:empty", "n": ""}, {keyField: "app:null", "unselected": "still a HASH"}, nil, nil}
	if err != nil || !reflect.DeepEqual(got, want) || h.calls != 2 || r.fallback {
		t.Fatal(got, err, h.calls)
	}
}
func TestUnmanagedProjectionReadLaterWholeImage(t *testing.T) {
	for _, mode := range []string{"updated", "deleted", "wrongtype", "still_null"} {
		t.Run(mode, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n", "text")
			h.step = func(n int, cmds []goredis.Cmder) error {
				if len(cmds) != 1 {
					t.Fatal(len(cmds))
				}
				if n == 1 {
					setProjectionReply(cmds[0], []any{nil, nil}, nil)
					return nil
				}
				switch mode {
				case "updated":
					setProjectionNative(cmds[0], map[string]string{"n": "9223372036854775807", "text": "new image", "__key": "application data"}, nil)
				case "deleted":
					setProjectionNative(cmds[0], map[string]string{}, nil)
				case "wrongtype":
					err := controlScriptNativeError("WRONGTYPE changed after HMGET")
					setProjectionNative(cmds[0], nil, err)
					return err
				case "still_null":
					setProjectionNative(cmds[0], map[string]string{"other": "keeps HASH alive"}, nil)
				}
				return nil
			}
			got, err := r.read(context.Background(), []string{"app:row"})
			if err != nil || len(got) != 1 || h.calls != 2 {
				t.Fatal(got, err, h.calls)
			}
			switch mode {
			case "updated":
				if !reflect.DeepEqual(got[0], aggRow{keyField: "app:row", "n": "9223372036854775807", "text": "new image"}) {
					t.Fatal("mixed temporal row image", got)
				}
			case "deleted", "wrongtype":
				if got[0] != nil {
					t.Fatal(got)
				}
			case "still_null":
				if !reflect.DeepEqual(got[0], aggRow{keyField: "app:row", "other": "keeps HASH alive"}) {
					t.Fatal(got)
				}
			}
		})
	}
}
func TestUnmanagedProjectionReadPermissionCompatibility(t *testing.T) {
	for _, message := range []string{"NOPERM this user has no permissions to run the 'hmget' command", "NOPERM User reader has no permissions to run the 'hmget' command"} {
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
						t.Fatal("retried denied HMGET", cmd.Name())
					}
					setProjectionNative(cmd, map[string]string{"n": "7"}, nil)
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
func TestUnmanagedProjectionReadSourceAddressWins(t *testing.T) {
	r, h := projectionReadUnit(t, keyField)
	h.step = func(_ int, cmds []goredis.Cmder) error {
		setProjectionReply(cmds[0], []any{"application data"}, nil)
		return nil
	}
	got, err := r.read(context.Background(), []string{"app:actual"})
	if err != nil || got[0][keyField] != "app:actual" {
		t.Fatal(got, err)
	}
}
func TestUnmanagedProjectionReadBoundsAndZeroFields(t *testing.T) {
	r, h := projectionReadUnit(t)
	h.step = func(_ int, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() != "hgetall" {
				t.Fatal("zero-field HMGET", cmd.Name())
			}
			setProjectionNative(cmd, map[string]string{"other": "present"}, nil)
		}
		return nil
	}
	got, err := r.read(context.Background(), []string{"app:row"})
	if err != nil || len(got) != 1 || got[0][keyField] != "app:row" {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.read(ctx, []string{"app:row"}); err == nil || h.calls != 1 {
		t.Fatal("canceled read reached transport")
	}
	if _, err = r.read(context.Background(), []string{metaKey("public", "secret")}); err == nil || h.calls != 1 {
		t.Fatal("driver storage reached source reader")
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
func TestUnmanagedProjectionReadUnknownDoesNotFallback(t *testing.T) {
	nativePermission := controlScriptNativeError("NOPERM this user has no permissions to run the 'hmget' command")
	nativeWrong := controlScriptNativeError("WRONGTYPE changed")
	cases := map[string]error{"lost_reply": io.ErrUnexpectedEOF, "whole_nil": goredis.Nil, "joined_nil": errors.Join(goredis.Nil, io.EOF), "wrapped_nil": fmt.Errorf("unknown: %w", goredis.Nil), "joined_permission": errors.Join(nativePermission, io.EOF), "wrapped_permission": fmt.Errorf("unknown: %w", nativePermission), "joined_wrongtype": errors.Join(nativeWrong, io.EOF), "wrapped_wrongtype": fmt.Errorf("unknown: %w", nativeWrong), "untyped_permission": errors.New(nativePermission.Error()), "untyped_wrongtype": errors.New(nativeWrong.Error()), "other_command_permission": controlScriptNativeError("NOPERM this user has no permissions to run the 'hget' command"), "ordinary_server_error": controlScriptNativeError("ERR failure")}
	for name, unknown := range cases {
		for _, pipelineOnly := range []bool{false, true} {
			t.Run(fmt.Sprint(name, "/pipeline=", pipelineOnly), func(t *testing.T) {
				r, h := projectionReadUnit(t, "n")
				r.prepared = true
				h.step = func(_ int, cmds []goredis.Cmder) error {
					for _, cmd := range cmds {
						setProjectionReply(cmd, []any{nil}, nil)
					}
					if !pipelineOnly {
						cmds[len(cmds)-1].SetErr(unknown)
					}
					return unknown
				}
				got, err := r.read(context.Background(), []string{"app:one", "app:two"})
				if err == nil || got != nil || h.calls != 1 || r.fallback {
					t.Fatal("unknown accepted or replayed", got, err, h.calls)
				}
				if !strings.Contains(err.Error(), unknown.Error()) {
					t.Fatal("unknown diagnostic lost", err, unknown)
				}
			})
		}
	}
}
func TestUnmanagedProjectionReadKnownRefusalDoesNotHideLaterUnknown(t *testing.T) {
	for _, known := range []error{controlScriptNativeError("WRONGTYPE changed"), controlScriptNativeError("NOPERM this user has no permissions to run the 'hmget' command"), controlScriptNativeError("NOPERM No permissions to access a key")} {
		t.Run(known.Error(), func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			r.prepared = true
			h.step = func(_ int, cmds []goredis.Cmder) error {
				setProjectionReply(cmds[0], nil, known)
				setProjectionReply(cmds[1], nil, errors.Join(goredis.Nil, io.ErrUnexpectedEOF))
				return known
			}
			if got, err := r.read(context.Background(), []string{"app:one", "app:two"}); err == nil || got != nil || h.calls != 1 {
				t.Fatal("known refusal hid later loss", got, err, h.calls)
			}
		})
	}
}
func TestUnmanagedProjectionReadMalformedDoesNotFallback(t *testing.T) {
	for _, values := range [][]any{nil, {}, {int64(7)}, {[]byte("7")}, {nil, nil}} {
		t.Run(fmt.Sprintf("%#v", values), func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			r.prepared = true
			h.step = func(_ int, cmds []goredis.Cmder) error {
				setProjectionReply(cmds[0], []any{nil}, nil)
				setProjectionReply(cmds[1], values, nil)
				return nil
			}
			if got, err := r.read(context.Background(), []string{"app:one", "app:two"}); err == nil || got != nil || h.calls != 1 {
				t.Fatal("malformed reply accepted or triggered all-null fallback", got, err, h.calls)
			}
		})
	}
}
func TestUnmanagedProjectionReadNativeFallbackUnknownDoesNotReplay(t *testing.T) {
	for _, pipelineOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(pipelineOnly), func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			unknown := errors.Join(controlScriptNativeError("WRONGTYPE changed"), io.ErrUnexpectedEOF)
			h.step = func(n int, cmds []goredis.Cmder) error {
				if n == 1 {
					setProjectionReply(cmds[0], []any{nil}, nil)
					return nil
				}
				setProjectionNative(cmds[0], map[string]string{"n": "new"}, nil)
				if !pipelineOnly {
					cmds[0].SetErr(unknown)
				}
				return unknown
			}
			if got, err := r.read(context.Background(), []string{"app:row"}); err == nil || got != nil || h.calls != 2 {
				t.Fatal("fallback unknown ignored/replayed", got, err, h.calls)
			}
		})
	}
}
func unmanagedProjectionRequestBytes(cmd goredis.Cmder) int {
	args := cmd.Args()
	n := len(strconv.Itoa(len(args))) + 3
	for _, arg := range args {
		value := fmt.Sprint(arg)
		n += len(value) + len(strconv.Itoa(len(value))) + 5
	}
	return n
}
func TestUnmanagedProjectionReadRequestByteBound(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	r.prepared = true
	keys := make([]string, pipelineChunk)
	for i := range keys {
		keys[i] = "app:" + strings.Repeat("k", 1200) + strconv.Itoa(i)
	}
	h.step = func(_ int, cmds []goredis.Cmder) error {
		bytes := 0
		for _, cmd := range cmds {
			bytes += unmanagedProjectionRequestBytes(cmd)
			setProjectionReply(cmd, []any{"7"}, nil)
		}
		if bytes > unmanagedProjectionReadBytes || len(cmds) > pipelineChunk {
			t.Fatal("unbounded request", bytes, len(cmds))
		}
		return nil
	}
	got, err := r.read(context.Background(), keys)
	if err != nil || len(got) != len(keys) || h.calls != 2 {
		t.Fatal(got, err, h.calls)
	}
}
func TestUnmanagedProjectionReadOversizedRequestKeepsNativeFallback(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	key := "app:" + strings.Repeat("k", unmanagedProjectionReadBytes)
	h.step = func(_ int, cmds []goredis.Cmder) error {
		if len(cmds) != 1 || cmds[0].Name() != "hgetall" || cmds[0].Args()[1] != key {
			t.Fatal("oversized key reached projection", cmds[0].Name())
		}
		setProjectionNative(cmds[0], map[string]string{"n": "7"}, nil)
		return nil
	}
	got, err := r.read(context.Background(), []string{key})
	if err != nil || len(got) != 1 || got[0]["n"] != "7" || h.calls != 1 {
		t.Fatal(got, err, h.calls)
	}
}

func TestUnmanagedProjectionReadAllNullFallbackDoesNotDisableProjection(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	h.step = func(n int, cmds []goredis.Cmder) error {
		switch n {
		case 1:
			setProjectionReply(cmds[0], []any{nil}, nil)
		case 2:
			setProjectionNative(cmds[0], map[string]string{"other": "present"}, nil)
		case 3:
			if cmds[0].Name() != "hmget" {
				t.Fatal("all-null row disabled later projection")
			}
			setProjectionReply(cmds[0], []any{"7"}, nil)
		default:
			t.Fatal("unexpected extra read", n)
		}
		return nil
	}
	if _, err := r.read(context.Background(), []string{"app:null"}); err != nil || r.fallback {
		t.Fatal(err, r.fallback)
	}
	got, err := r.read(context.Background(), []string{"app:next"})
	if err != nil || len(got) != 1 || got[0]["n"] != "7" || h.calls != 3 {
		t.Fatal(got, err, h.calls)
	}
}
func TestUnmanagedProjectionReadCancellationBeforeAllNullFallback(t *testing.T) {
	r, h := projectionReadUnit(t, "n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.step = func(_ int, cmds []goredis.Cmder) error {
		setProjectionReply(cmds[0], []any{nil}, nil)
		cancel()
		return nil
	}
	if got, err := r.read(ctx, []string{"app:null"}); err == nil || got != nil || h.calls != 1 {
		t.Fatal("canceled all-null read issued fallback", got, err, h.calls)
	}
}

func TestUnmanagedProjectionReadKeySelectorCompatibility(t *testing.T) {
	for _, message := range []string{"NOPERM No permissions to access a key", "NOPERM User reader has no permissions to access the 'app:selector' key"} {
		t.Run(message, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			r.prepared = true
			h.step = func(n int, cmds []goredis.Cmder) error {
				if n == 1 {
					setProjectionReply(cmds[0], []any{"1"}, nil)
					err := controlScriptNativeError(message)
					setProjectionReply(cmds[1], nil, err)
					setProjectionReply(cmds[2], []any{"3"}, nil)
					return err
				}
				if len(cmds) != 1 || cmds[0].Name() != "hgetall" || cmds[0].Args()[1] != "app:selector" {
					t.Fatal("selector fallback reread an authorized HMGET", len(cmds), cmds[0].Args())
				}
				setProjectionNative(cmds[0], map[string]string{"n": "2"}, nil)
				return nil
			}
			got, err := r.read(context.Background(), []string{"app:one", "app:selector", "app:three"})
			if err != nil || len(got) != 3 || got[0]["n"] != "1" || got[1]["n"] != "2" || got[2]["n"] != "3" || r.fallback || h.calls != 2 {
				t.Fatal(got, err, h.calls)
			}
		})
	}
}
func TestUnmanagedProjectionReadKeySelectorUnknownNeverFallsBack(t *testing.T) {
	for _, message := range []string{"NOPERM No permissions to access a key", "NOPERM User reader has no permissions to access the 'app:selector' key"} {
		for _, pipelineOnly := range []bool{false, true} {
			for _, joined := range []bool{false, true} {
				t.Run(fmt.Sprint(message, "/pipeline=", pipelineOnly, "/joined=", joined), func(t *testing.T) {
					r, h := projectionReadUnit(t, "n")
					native := controlScriptNativeError(message)
					unknown := error(fmt.Errorf("unknown: %w", native))
					if joined {
						unknown = errors.Join(native, io.EOF)
					}
					h.step = func(_ int, cmds []goredis.Cmder) error {
						setProjectionReply(cmds[0], nil, native)
						if !pipelineOnly {
							cmds[0].SetErr(unknown)
						}
						return unknown
					}
					if got, err := r.read(context.Background(), []string{"app:selector"}); err == nil || got != nil || h.calls != 1 {
						t.Fatal("key refusal extracted from unknown reply", got, err, h.calls)
					}
				})
			}
		}
	}
}
func TestUnmanagedProjectionReadOtherKeyOrChannelRefusalIsUnknown(t *testing.T) {
	for _, message := range []string{"NOPERM No permissions to access a channel", "NOPERM No permissions to access a key extra", "NOPERM User reader has no permissions to access the 'app:different' key", "NOPERM User reader has no permissions to access the 'app:selector' channel"} {
		t.Run(message, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n")
			h.step = func(_ int, cmds []goredis.Cmder) error {
				err := controlScriptNativeError(message)
				setProjectionReply(cmds[0], nil, err)
				return err
			}
			if got, err := r.read(context.Background(), []string{"app:selector"}); err == nil || got != nil || h.calls != 1 {
				t.Fatal("unrecognized refusal triggered fallback", got, err, h.calls)
			}
		})
	}
}

func TestUnmanagedProjectionReadKeyRefusalKeepsLaterHMGETEnabled(t *testing.T) {
	for _, message := range []string{"NOPERM No permissions to access a key", "NOPERM User reader has no permissions to access the 'app:hgetall_only' key"} {
		t.Run(message, func(t *testing.T) {
			r, h := projectionReadUnit(t, "n") // Exercise the first-key capability boundary.
			h.step = func(n int, cmds []goredis.Cmder) error {
				if len(cmds) != 1 {
					t.Fatal("unexpected boundary grouping", len(cmds))
				}
				switch n {
				case 1:
					if cmds[0].Name() != "hmget" || cmds[0].Args()[1] != "app:hgetall_only" {
						t.Fatal(cmds[0].Args())
					}
					err := controlScriptNativeError(message)
					setProjectionReply(cmds[0], nil, err)
					return err
				case 2:
					if cmds[0].Name() != "hgetall" || cmds[0].Args()[1] != "app:hgetall_only" {
						t.Fatal(cmds[0].Args())
					}
					setProjectionNative(cmds[0], map[string]string{"n": "2"}, nil)
				case 3:
					if cmds[0].Name() != "hmget" || cmds[0].Args()[1] != "app:hmget_only" {
						t.Fatal("key refusal disabled permitted HMGET on later key", cmds[0].Args())
					}
					setProjectionReply(cmds[0], []any{"3"}, nil)
				default:
					t.Fatal("unexpected extra read", n)
				}
				return nil
			}
			got, err := r.read(context.Background(), []string{"app:hgetall_only", "app:hmget_only"})
			if err != nil || len(got) != 2 || got[0]["n"] != "2" || got[1]["n"] != "3" || r.fallback || h.calls != 3 {
				t.Fatal(got, err, r.fallback, h.calls)
			}
		})
	}
}
