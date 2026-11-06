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

// Observer-only logical view. The actual command, digest and declared keys
// reaching Redis remain unchanged; raw-wire executor tests do not use this.
func snapshotObservedControlArgs(cmd goredis.Cmder) []any {
	args := cmd.Args()
	if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "EVALSHA") && args[1] == snapshotControlDigest {
		logical := append([]any(nil), args...)
		logical[0], logical[1] = "EVAL", snapshotControl
		return logical
	}
	return args
}
func snapshotObservedControlName(cmd goredis.Cmder) string {
	args := cmd.Args()
	if len(args) > 1 && cmd.Name() == "evalsha" && args[1] == snapshotControlDigest {
		return "eval"
	}
	return cmd.Name()
}

func TestSnapshotControlScriptObservationPreservesWire(t *testing.T) {
	ctx := context.Background()
	cmd := goredis.NewCmd(ctx, "EVALSHA", snapshotControlDigest, 1, snapshotCatalogKey, "publish")
	before := append([]any(nil), cmd.Args()...)
	logical := snapshotObservedControlArgs(cmd)
	if logical[0] != "EVAL" || logical[1] != snapshotControl || snapshotObservedControlName(cmd) != "eval" {
		t.Fatal("digest control command was not observed")
	}
	logical[3] = "changed-observation"
	if !reflect.DeepEqual(before, cmd.Args()) {
		t.Fatal("observer changed native digest/keys")
	}
	other := goredis.NewCmd(ctx, "EVALSHA", goredis.NewScript("return 1").Hash(), 0)
	if snapshotObservedControlName(other) != "evalsha" || !reflect.DeepEqual(snapshotObservedControlArgs(other), other.Args()) {
		t.Fatal("observer reclassified another script")
	}
}

type controlScriptNativeError string

func (e controlScriptNativeError) Error() string { return string(e) }
func (controlScriptNativeError) RedisError()     {}

type controlScriptProcessor struct {
	t     *testing.T
	calls [][]any
	step  func(int, goredis.Cmder) error
}

func (p *controlScriptProcessor) Process(_ context.Context, cmd goredis.Cmder) error {
	if marker, ok := cmd.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
		p.t.Fatal("control script lost its no-retry marker")
	}
	p.calls = append(p.calls, append([]any(nil), cmd.Args()...))
	err := p.step(len(p.calls), cmd)
	cmd.SetErr(err)
	return err
}

func TestSnapshotControlScriptColdThenHot(t *testing.T) {
	p := &controlScriptProcessor{t: t}
	p.step = func(n int, cmd goredis.Cmder) error {
		if n == 1 {
			return controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")
		}
		cmd.(onceCmd).SetVal(int64(n))
		return nil
	}
	var runner snapshotControlExecutor
	keys := []any{snapshotCatalogKey, snapshotMetadataGraph}
	argv := []any{"renew", "token", "root", "base", int64(120000), "", "[]"}
	if result := runner.run(context.Background(), p, keys, argv...); result.Err() != nil || result.Val() != int64(2) {
		t.Fatal(result.Err(), result.Val())
	}
	if result := runner.run(context.Background(), p, keys, argv...); result.Err() != nil || result.Val() != int64(3) {
		t.Fatal(result.Err(), result.Val())
	}
	if len(p.calls) != 3 || p.calls[0][0] != "EVALSHA" || p.calls[1][0] != "EVAL" || p.calls[2][0] != "EVALSHA" {
		t.Fatal("unexpected cold/hot commands")
	}
	if p.calls[0][1] != snapshotControlDigest || p.calls[1][1] != snapshotControl || p.calls[2][1] != snapshotControlDigest {
		t.Fatal("script identity differs")
	}
	if !reflect.DeepEqual(p.calls[0][2:], p.calls[1][2:]) || !reflect.DeepEqual(p.calls[0][2:], p.calls[2][2:]) {
		t.Fatal("fallback changed keys/arguments")
	}
	t.Logf("control script bytes=%d; hot digest bytes=%d", len(snapshotControl), len(snapshotControlDigest))
}

func TestSnapshotControlScriptEvalOnlyCompatibility(t *testing.T) {
	for i, message := range []string{
		"NOPERM this user has no permissions to run the 'evalsha' command",
		"NOPERM User private-user has no permissions to run the 'evalsha' command",
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p := &controlScriptProcessor{t: t}
			executions := 0
			p.step = func(n int, cmd goredis.Cmder) error {
				if n == 1 {
					return controlScriptNativeError(message)
				}
				executions++
				cmd.(onceCmd).SetVal(int64(executions))
				return nil
			}
			var runner snapshotControlExecutor
			for range 2 {
				if err := runner.run(context.Background(), p, []any{snapshotCatalogKey}, "acquire").Err(); err != nil {
					t.Fatal(err)
				}
			}
			if len(p.calls) != 3 || executions != 2 || p.calls[1][0] != "EVAL" || p.calls[2][0] != "EVAL" {
				t.Fatal("EVAL-only role paid another denied digest attempt")
			}
		})
	}
}

func TestSnapshotControlScriptUnknownReplyDoesNotReplay(t *testing.T) {
	for i, err := range []error{
		io.ErrUnexpectedEOF,
		fmt.Errorf("unknown result: %w", controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")),
		errors.Join(io.ErrUnexpectedEOF, controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")),
		errors.New("NOSCRIPT No matching script. Please use EVAL."),
		errors.New("NOPERM this user has no permissions to run the 'evalsha' command"),
		controlScriptNativeError("ERR Error running script: NOPERM User private-user has no permissions to run the 'hset' command"),
		controlScriptNativeError("NOPERM No permissions to access a key"),
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p := &controlScriptProcessor{t: t}
			effects := 0
			p.step = func(_ int, _ goredis.Cmder) error { effects++; return err }
			var runner snapshotControlExecutor
			result := runner.run(context.Background(), p, []any{snapshotCatalogKey}, "publish")
			if !errors.Is(result.Err(), err) || len(p.calls) != 1 || effects != 1 || runner.evalOnly.Load() {
				t.Fatal("unknown/runtime error was resent or changed")
			}
		})
	}
}

func TestSnapshotControlScriptLostFallbackReplyDoesNotReplay(t *testing.T) {
	p := &controlScriptProcessor{t: t}
	effects := 0
	p.step = func(n int, _ goredis.Cmder) error {
		if n == 1 {
			return controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")
		}
		effects++
		return io.ErrUnexpectedEOF
	}
	var runner snapshotControlExecutor
	result := runner.run(context.Background(), p, []any{snapshotCatalogKey}, "reserve")
	if !errors.Is(result.Err(), io.ErrUnexpectedEOF) || len(p.calls) != 2 || effects != 1 {
		t.Fatal("uncertain full-body attempt was replayed")
	}
}

func TestSnapshotControlScriptCancellationBeforeFallback(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &controlScriptProcessor{t: t}
			p.step = func(_ int, _ goredis.Cmder) error {
				cancel()
				return controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")
			}
			if before {
				cancel()
			}
			var runner snapshotControlExecutor
			result := runner.run(ctx, p, []any{snapshotCatalogKey}, "publish")
			want := 1
			if before {
				want = 0
			}
			if !errors.Is(result.Err(), context.Canceled) || len(p.calls) != want {
				t.Fatal("canceled operation started a script")
			}
		})
	}
}

func TestSnapshotControlScriptRefusalClassification(t *testing.T) {
	for _, tc := range []struct {
		err             error
		missing, denied bool
	}{
		{nil, false, false},
		{controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL."), true, false},
		{controlScriptNativeError("NOPERM this user has no permissions to run the 'evalsha' command"), false, true},
		{controlScriptNativeError("NOPERM User private-user has no permissions to run the 'evalsha' command"), false, true},
		{errors.New("NOSCRIPT No matching script. Please use EVAL."), false, false},
		{fmt.Errorf("wrapped: %w", controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")), false, false},
		{errors.Join(io.ErrUnexpectedEOF, controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")), false, false},
		{errors.New("NOPERM this user has no permissions to run the 'evalsha' command"), false, false},
		{controlScriptNativeError("NOPERM No permissions to access a key"), false, false},
		{controlScriptNativeError("NOPERM User private-user has no permissions to run the 'hset' command"), false, false},
		{controlScriptNativeError("ERR Error running script: NOPERM User private-user has no permissions to run the 'evalsha' command"), false, false},
		{controlScriptNativeError("ERR NOSCRIPT inside Lua"), false, false},
		{io.ErrUnexpectedEOF, false, false},
		{context.Canceled, false, false},
	} {
		missing, denied := snapshotControlScriptRefusal(tc.err)
		if missing != tc.missing || denied != tc.denied {
			t.Fatalf("classification differs: %v %v", missing, denied)
		}
	}
}
