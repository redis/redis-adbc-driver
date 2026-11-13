// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotBatchScriptWireAndRefusals(t *testing.T) {
	for _, script := range []struct{ name, body, digest string }{
		{"rows", snapshotWriteBatchLua, snapshotWriteBatchDigest},
		{"index", snapshotIndexNotifyLua, snapshotIndexNotifyDigest},
		{"compaction", snapshotCompactionCopyLua, snapshotCompactionCopyDigest},
	} {
		for _, mode := range []string{"cached", "missing", "denied", "lost", "lost_fallback", "runtime"} {
			t.Run(script.name+"/"+mode, func(t *testing.T) {
				args := []any{"EVAL", script.body, 2, snapshotCatalogKey, snapshotMetadataGraph, "opaque-token", "\x00binary", "9007199254740993"}
				before := append([]any(nil), args...)
				p := &controlScriptProcessor{t: t}
				p.step = func(n int, cmd goredis.Cmder) error {
					if n == 1 {
						switch mode {
						case "missing", "lost_fallback":
							return controlScriptNativeError("NOSCRIPT No matching script. Please use EVAL.")
						case "denied":
							return controlScriptNativeError("NOPERM this user has no permissions to run the 'evalsha' command")
						case "lost":
							return io.ErrUnexpectedEOF
						case "runtime":
							return controlScriptNativeError("ERR Error running script: NOPERM HSET")
						}
					}
					if mode == "lost_fallback" {
						return io.ErrUnexpectedEOF
					}
					cmd.(onceCmd).SetVal(int64(1))
					return nil
				}
				var runner snapshotControlExecutor
				result := runner.runScript(context.Background(), p, script.digest, args)
				want := 1
				if mode == "missing" || mode == "denied" || mode == "lost_fallback" {
					want = 2
				}
				if len(p.calls) != want || !reflect.DeepEqual(before[2:], args[2:]) {
					t.Fatal("wrong attempt count or mutated keys/arguments")
				}
				if p.calls[0][0] != "EVALSHA" || p.calls[0][1] != script.digest || !reflect.DeepEqual(before[2:], p.calls[0][2:]) {
					t.Fatal("digest attempt changed script identity or arguments")
				}
				if want == 2 && !reflect.DeepEqual(before, p.calls[1]) {
					t.Fatal("fallback changed original command")
				}
				if (mode == "lost" || mode == "lost_fallback") && !errors.Is(result.Err(), io.ErrUnexpectedEOF) || mode == "runtime" && result.Err() == nil {
					t.Fatal("lost/runtime reply was hidden")
				}
				if mode == "denied" {
					if err := runner.runScript(context.Background(), p, script.digest, append([]any(nil), before...)).Err(); err != nil || p.calls[2][0] != "EVAL" || p.calls[2][1] != script.body {
						t.Fatal("EVAL-only principal lost compatibility", err)
					}
				}
			})
		}
	}
}
