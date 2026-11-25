// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisFunctionManifest(t *testing.T) {
	_, code := redisFunctionLibrary()
	for _, p := range []struct{ name, body string }{{snapshotIndexNotifyDigest, snapshotIndexNotifyLua}, {snapshotControlDigest, snapshotControl}, {snapshotWriteBatchDigest, snapshotWriteBatchLua}, {snapshotCompactionCopyDigest, snapshotCompactionCopyLua}, {snapshotPointReadDigest, snapshotPointReadLua}} {
		if !redisFunctionRegistered(code, p.name) {
			t.Fatal("entry point absent from installed library", p.name)
		}
		if p.name != redisFunctionName(p.body) {
			t.Fatalf("initialization mismatch: %s want %s", p.name, redisFunctionName(p.body))
		}
	}
	for _, body := range redisFunctionBodies {
		if !redisFunctionRegistered(code, redisFunctionName(body)) {
			t.Fatal("missing entry point")
		}
	}
}

func TestRedisFunctionsInitializationRefusals(t *testing.T) {
	name, _ := redisFunctionLibrary()
	missing := controlScriptNativeError("ERR Function not found")
	collision := controlScriptNativeError("ERR Library '" + name + "' already exists")
	lost := io.ErrUnexpectedEOF
	for _, tc := range []struct {
		name    string
		replies []error
		wantErr bool
	}{
		{"present", []error{nil}, false},
		{"install", []error{missing, nil, nil}, false},
		{"concurrent_install", []error{missing, collision, nil}, false},
		{"unknown_probe", []error{lost}, true},
		{"wrapped_missing", []error{fmt.Errorf("unknown: %w", missing)}, true},
		{"denied_probe", []error{controlScriptNativeError("NOPERM fcall")}, true},
		{"unsupported", []error{controlScriptNativeError("ERR unknown command 'FCALL'")}, true},
		{"unknown_load", []error{missing, lost}, true},
		{"denied_load", []error{missing, controlScriptNativeError("NOPERM function")}, true},
		{"missing_after_load", []error{missing, nil, missing}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &controlScriptProcessor{t: t}
			p.step = func(n int, cmd goredis.Cmder) error {
				if n > len(tc.replies) {
					t.Fatal("unexpected retry")
				}
				// The probe is a no-writes entry point, so it still answers
				// over maxmemory; FUNCTION LOAD itself is denied under OOM.
				want := "FCALL_RO"
				if n == 2 {
					want = "FUNCTION"
				}
				if cmd.Args()[0] != want || (want == "FCALL_RO" && cmd.Args()[1] != newReadOnlyRedisFunction("return 1").Name()) {
					t.Fatal("legacy execution", cmd.Args()[0])
				}
				if n == 2 && (cmd.Args()[1] != "LOAD" || len(cmd.Args()) != 3) {
					t.Fatal("load replaces existing library")
				}
				if tc.replies[n-1] == nil {
					cmd.(onceCmd).SetVal(int64(1))
				}
				return tc.replies[n-1]
			}
			err := ensureRedisFunctions(context.Background(), p)
			if (err != nil) != tc.wantErr || len(p.calls) != len(tc.replies) {
				t.Fatal("wrong initialization result", err, len(p.calls))
			}
		})
	}
}

func TestRedisRequiresExplicitACLUsername(t *testing.T) {
	for _, uri := range []string{"redis://localhost:1/0", "redis://:secret@localhost:1/0", "redis://user@localhost:1/0"} {
		d := &databaseImpl{uri: uri}
		_, _, _, _, err := d.dial(context.Background())
		if !isStatus(err, adbc.StatusInvalidArgument) || !strings.Contains(err.Error(), "ACL username and password are required") || strings.Contains(err.Error(), "secret") {
			t.Fatal("anonymous or password-only connection admitted", err)
		}
	}
}

func TestRedisFunctionsRejectEvalOnlySoftware(t *testing.T) {
	uri := os.Getenv("REDIS_FUNCTIONS_DENIED_URI")
	if uri == "" {
		t.Skip("requires an owned Software operator denied FCALL")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	opts, err := goredis.ParseURL(uri)
	if err != nil || opts.Addr != raw.Options().Addr || opts.DB != raw.Options().DB || opts.Username == "" || raw.Options().TLSConfig == nil {
		t.Fatal("operator differs from owned strict-TLS fixture")
	}
	opts.TLSConfig = raw.Options().TLSConfig.Clone()
	operator := goredis.NewClient(opts)
	defer operator.Close()
	if err := operator.Ping(h.ctx).Err(); err != nil {
		t.Fatal("operator cannot authenticate", err)
	}
	if n, err := operator.Eval(h.ctx, "return 1", nil).Int64(); err != nil || n != 1 {
		t.Fatal("fixture does not permit EVAL", err)
	}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: uri, OptionStringTelemetryEnabled: "false"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	c, err := db.Open(h.ctx)
	if c != nil {
		c.Close(h.ctx)
	}
	if err == nil {
		t.Fatal("EVAL-only operator admitted")
	}
	if !strings.Contains(err.Error(), "Redis Functions are required") {
		t.Fatal("missing Functions requirement diagnostic", err)
	}
}

type functionPipelineLoss struct{ calls int }

func (*functionPipelineLoss) DialHook(next goredis.DialHook) goredis.DialHook          { return next }
func (*functionPipelineLoss) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook { return next }
func (h *functionPipelineLoss) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		h.calls++
		for _, cmd := range cmds {
			cmd.(onceCmd).SetVal(int64(1))
		}
		return io.ErrUnexpectedEOF
	}
}
func TestRedisFunctionRowPipelinePreservesGlobalLoss(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	defer client.Close()
	hook := &functionPipelineLoss{}
	client.AddHook(hook)
	s := &store{client: client}
	err := s.updateKeyRows(context.Background(), &tableMeta{KeyRows: true}, []rowChange{{key: "app:one"}})
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") || hook.calls != 1 {
		t.Fatal("pipeline loss hidden or replayed", err, hook.calls)
	}
}

func TestRedisFunctionRefusalClassification(t *testing.T) {
	for _, tc := range []struct {
		err             error
		missing, denied bool
	}{
		{controlScriptNativeError("ERR Function not found"), true, false},
		{controlScriptNativeError("NOPERM this user has no permissions to run the 'fcall' command"), false, true},
		{controlScriptNativeError("NOPERM injected function key selector refusal"), false, false},
		{fmt.Errorf("lost: %w", controlScriptNativeError("ERR Function not found")), false, false},
		{io.ErrUnexpectedEOF, false, false},
	} {
		m, d := snapshotControlScriptRefusal(tc.err)
		if m != tc.missing || d != tc.denied {
			t.Fatal("wrong native refusal classification", tc.err, m, d)
		}
	}
}
