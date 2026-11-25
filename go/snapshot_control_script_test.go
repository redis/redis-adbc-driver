// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Observer-only source view for existing lifecycle/fault-injection checks.
// FCALL names and declared keys on the actual wire remain unchanged; raw-wire
// contract tests inspect cmd.Args directly.
func snapshotObservedControlArgs(cmd goredis.Cmder) []any {
	args := cmd.Args()
	if body := snapshotObservedScriptBody(cmd); body != "" {
		logical := append([]any(nil), args...)
		logical[0], logical[1] = "EVAL", body
		return logical
	}
	return args
}
func snapshotObservedControlName(cmd goredis.Cmder) string {
	if snapshotObservedScriptBody(cmd) != "" {
		return "eval"
	}
	return cmd.Name()
}

func snapshotObservedScriptBody(cmd goredis.Cmder) string {
	args := cmd.Args()
	if len(args) < 2 {
		return ""
	}
	command := strings.ToUpper(fmt.Sprint(args[0]))
	if command != "FCALL" && command != "FCALL_RO" {
		return ""
	}
	for _, body := range redisFunctionBodies {
		if args[1] == redisFunctionName(body) {
			// The other command is not the driver's call: FCALL_RO refuses an
			// unflagged function, and FCALL of a no-writes body is denied under
			// OOM and needs write key permissions. Fakes then see an unknown call.
			if command != redisFunctionCommand(redisFunctionName(body)) {
				return ""
			}
			return body
		}
	}

	return ""
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

func snapshotControlScriptRefusal(err error) (bool, bool) {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return false, false
	}
	message := native.Error()
	denied := message == "NOPERM this user has no permissions to run the 'fcall' command" ||
		strings.HasPrefix(message, "NOPERM User ") && strings.HasSuffix(message, " has no permissions to run the 'fcall' command")
	return message == "ERR Function not found", denied
}

func TestSnapshotFunctionsNeverFallBack(t *testing.T) {
	for _, failure := range []error{nil, controlScriptNativeError("ERR Function not found"), controlScriptNativeError("NOPERM this user has no permissions to run the 'fcall' command"), fmt.Errorf("lost reply")} {
		p := &controlScriptProcessor{t: t}
		p.step = func(_ int, c goredis.Cmder) error { return failure }
		var runner snapshotControlExecutor
		result := runner.run(context.Background(), p, []any{snapshotCatalogKey}, "valid", "token")
		if result.Err() != failure || len(p.calls) != 1 || p.calls[0][0] != "FCALL" || p.calls[0][1] != redisFunctionName(snapshotControl) {
			t.Fatal("function operation replayed or changed", p.calls, result.Err())
		}
	}
}
