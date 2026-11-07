// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotClusterHealthyProofReply struct{}

func (snapshotClusterHealthyProofReply) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (snapshotClusterHealthyProofReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (snapshotClusterHealthyProofReply) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			switch typed := cmd.(type) {
			case *goredis.SliceCmd:
				typed.SetVal([]any{"0", "1"})
			case *goredis.IntCmd:
				typed.SetVal(0)
			case *goredis.StringCmd:
				typed.SetVal(strings.Repeat("1", 32))
			default:
				return fmt.Errorf("unexpected proof command type")
			}
		}
		return nil
	}
}

// Attachment precedes all execution commands. Multiple maintenance hooks may
// encounter the same transport, but must not install duplicate observers.
func TestSnapshotProofObserverExecutionAttachmentOnce(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", Protocol: 3})
	defer client.Close()
	observer := &snapshotClusterProofObserver{}
	transport := &executionTransport{client: client}
	ctx := context.WithValue(context.Background(), transportContextKey{}, transport)
	ctx = context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: strings.Repeat("2", 32)})
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			_ = observer.ProcessHook(func(context.Context, goredis.Cmder) error { return nil })(ctx, goredis.NewCmd(ctx, "ping"))
		})
	}
	group.Wait()
	client.AddHook(snapshotClusterHealthyProofReply{})
	for range 16 {
		group.Go(func() {
			pipe := client.Pipeline()
			owner := strings.Repeat("1", 32)
			pipe.HMGet(ctx, snapshotMetadataGraph, "n:"+owner+":00", "b:"+owner+":00")
			pipe.HLen(ctx, snapshotMetadataBucket(owner, "00"))
			pipe.ZCard(ctx, snapshotMetadataDirectory(owner, "00"))
			pipe.HGet(ctx, snapshotMetadataBucket(owner, "00"), "table:public:probe")
			if _, err := pipe.Exec(ctx); err != nil {
				t.Errorf("healthy simulated proof failed: %v", err)
			}
		})
	}
	group.Wait()
	if observer.executionProofs.Load() != 16 || observer.recognized.Load() != 16 || observer.captured.Load() != 0 {
		t.Fatal("execution proof observer duplicated or missed replies")
	}
}

// The first maintenance call of a managed statement precedes its execution
// pool's first command. Inject only into that pool, so a maintenance-only
// observer cannot accidentally satisfy the regression.
type snapshotClusterExecutionProofFault struct {
	attached atomic.Bool
	fault    snapshotClusterReplyFault
}

func (*snapshotClusterExecutionProofFault) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (f *snapshotClusterExecutionProofFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if transport, _ := ctx.Value(transportContextKey{}).(*executionTransport); transport != nil && f.attached.CompareAndSwap(false, true) {
			transport.client.AddHook(&f.fault)
		}
		return next(ctx, cmd)
	}
}
func (*snapshotClusterExecutionProofFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func TestSnapshotClusterMetadataProofObserverCoversSQLExecution(t *testing.T) {
	h, _, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE snapshot_execution_proof(v INTEGER)")
	h.exec("INSERT INTO snapshot_execution_proof VALUES (7)")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	var report string
	observer := &snapshotClusterProofObserver{directory: t.TempDir(), logf: func(format string, args ...any) { report = fmt.Sprintf(format, args...) }}
	fault := &snapshotClusterExecutionProofFault{}
	client := stmt.(*statementImpl).conn.store.client
	client.AddHook(observer)
	client.AddHook(fault)
	if err := stmt.SetSqlQuery(h.ctx, "SELECT v FROM snapshot_execution_proof"); err != nil {
		t.Fatal(err)
	}
	reader, _, err := stmt.ExecuteQuery(h.ctx)
	if reader != nil {
		reader.Release()
	}
	if err == nil || !strings.Contains(err.Error(), "expected=999") || !fault.attached.Load() || !fault.fault.injected.Load() {
		t.Fatal("execution-only proof fault was not returned unchanged", err)
	}
	if observer.captured.Load() != 1 || observer.executionProofs.Load() != 1 || !strings.Contains(report, "fresh_healthy=true") || !strings.Contains(report, "private_record_saved=true") {
		t.Fatal("observer missed the failed SQL execution proof")
	}
}
