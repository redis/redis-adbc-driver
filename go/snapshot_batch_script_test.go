package redis

import (
	"context"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"reflect"
	"testing"
)

func TestSnapshotBatchFunctionsNeverFallBack(t *testing.T) {
	// Only the read-only point read is sent with FCALL_RO; writers keep FCALL.
	for _, script := range []struct{ body, name, command string }{{snapshotWriteBatchLua, snapshotWriteBatchDigest, "FCALL"}, {snapshotIndexNotifyLua, snapshotIndexNotifyDigest, "FCALL"}, {snapshotCompactionCopyLua, snapshotCompactionCopyDigest, "FCALL"}, {snapshotPointReadLua, snapshotPointReadDigest, "FCALL_RO"}} {
		for _, failure := range []error{nil, io.ErrUnexpectedEOF, controlScriptNativeError("ERR Function not found"), controlScriptNativeError("NOPERM fcall")} {
			args := []any{"FCALL", script.name, 2, snapshotCatalogKey, snapshotMetadataGraph, "opaque-token", string([]byte{'a', 0, 'b'}), "9007199254740993"}
			before := append([]any(nil), args...)
			before[0] = script.command
			p := &controlScriptProcessor{t: t, step: func(_ int, _ goredis.Cmder) error { return failure }}
			var runner snapshotControlExecutor
			result := runner.runScript(context.Background(), p, script.name, args)
			if result.Err() != failure || len(p.calls) != 1 || !reflect.DeepEqual(p.calls[0], before) {
				t.Fatal("function batch replayed or changed", p.calls, result.Err())
			}
		}
	}
}
