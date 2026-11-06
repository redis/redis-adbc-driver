// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adbc-drivers/redis/go/internal/tlstrust"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotClusterProof struct {
	Parsed                                bool
	Expected, References, Entries, Fields int64
}

func (p snapshotClusterProof) healthy() bool {
	return p.Parsed && p.References > 0 && p.Expected == p.Entries && p.Expected == p.Fields
}
func snapshotClusterTuple(values []any, entries, fields int64) snapshotClusterProof {
	p := snapshotClusterProof{Entries: entries, Fields: fields}
	if len(values) != 2 {
		return p
	}
	var e, r error
	p.Expected, e = strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
	p.References, r = strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
	p.Parsed = e == nil && r == nil
	return p
}
func snapshotClusterFreshClient() (*goredis.ClusterClient, error) {
	opts, extra, _, err := parseURI(os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"))
	if err != nil {
		return nil, fmt.Errorf("diagnostic connection options unavailable")
	}
	opts.Protocol, opts.OnConnect, opts.DisableIdentity = 3, requireRESP3, true
	opts.ContextTimeoutEnabled = true
	if opts.TLSConfig != nil {
		opts.TLSConfig.RootCAs, err = tlstrust.Roots()
		if err != nil {
			return nil, fmt.Errorf("diagnostic trust unavailable")
		}
	}
	cluster := clusterOptions(opts)
	if extra != nil {
		cluster.Addrs = append(cluster.Addrs, extra.Addrs[1:]...)
	}
	return goredis.NewClusterClient(cluster), nil
}

// snapshotClusterProofObserver compares a failed native proof with a fresh read.
// It is test-only: command replies and the original error are never changed.
// A later healthy read is diagnostic evidence, not proof of the failure cause.
type snapshotClusterProofObserver struct {
	logf       func(string, ...any)
	captured   atomic.Int64
	recognized atomic.Int64
	directory  string
}

func (*snapshotClusterProofObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotClusterProofObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (o *snapshotClusterProofObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		originalErr := next(ctx, cmds)
		if originalErr != nil && !errors.Is(originalErr, goredis.Nil) {
			return originalErr
		}
		scope := snapshotOf(ctx)
		if scope == nil || len(cmds) != 4 {
			return originalErr
		}
		proof, ok := cmds[0].(*goredis.SliceCmd)
		if !ok || proof.Name() != "hmget" {
			return originalErr
		}
		count, cok := cmds[1].(*goredis.IntCmd)
		fields, fok := cmds[2].(*goredis.IntCmd)
		args := proof.Args()
		if !cok || !fok || count.Name() != "hlen" || fields.Name() != "zcard" || len(args) != 4 || fmt.Sprint(args[1]) != snapshotMetadataGraph {
			return originalErr
		}
		identity := strings.TrimPrefix(fmt.Sprint(args[2]), "n:")
		pieces := strings.Split(identity, ":")
		if len(pieces) != 2 || !snapshotMetadataID(pieces[0]) || len(pieces[1]) != 2 || fmt.Sprint(args[3]) != "b:"+identity {
			return originalErr
		}
		bucket, directory := snapshotMetadataBucket(pieces[0], pieces[1]), snapshotMetadataDirectory(pieces[0], pieces[1])
		if len(count.Args()) != 2 || len(fields.Args()) != 2 || fmt.Sprint(count.Args()[1]) != bucket || fmt.Sprint(fields.Args()[1]) != directory {
			return originalErr
		}
		o.recognized.Add(1)
		observed := snapshotClusterTuple(proof.Val(), count.Val(), fields.Val())
		if observed.healthy() {
			return originalErr
		}
		attempt := o.captured.Add(1)
		if attempt > 3 {
			return originalErr
		}
		fresh, freshErr := snapshotClusterFreshClient()
		if freshErr != nil {
			o.logf("metadata proof differential: fresh connection unavailable")
			return originalErr
		}
		defer fresh.Close()
		probe, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		pipe := fresh.Pipeline()
		p := pipe.HMGet(probe, snapshotMetadataGraph, fmt.Sprint(args[2]), fmt.Sprint(args[3]))
		n := pipe.HLen(probe, bucket)
		d := pipe.ZCard(probe, directory)
		root := pipe.HGet(probe, snapshotCatalogKey, "root")
		_, freshErr = pipe.Exec(probe)
		independent := snapshotClusterTuple(p.Val(), n.Val(), d.Val())
		current := root.Val() == scope.id
		saved := false
		if o.directory != "" {
			record := map[string]any{"utc": time.Now().UTC().Format(time.RFC3339Nano), "scope": scope.id, "current_root": root.Val(), "bucket": bucket, "directory": directory, "observed": observed, "fresh": independent, "fresh_error": freshErr != nil}
			if raw, err := json.Marshal(record); err == nil {
				// Never follow or overwrite an existing artifact, or inherit its mode.
				f, err := os.OpenFile(filepath.Join(o.directory, fmt.Sprintf("metadata-proof-%d.json", attempt)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					_, writeErr := f.Write(raw)
					closeErr := f.Close()
					saved = writeErr == nil && closeErr == nil
				}
			}
		}
		o.logf("metadata proof differential: original=%+v fresh=%+v fresh_error=%t fresh_healthy=%t scope_is_current=%t private_record_saved=%t", observed, independent, freshErr != nil, freshErr == nil && independent.healthy(), current, saved)
		// Diagnostic only: preserve every command result/error and never replay it.
		return originalErr
	}
}

type snapshotClusterReplyFault struct{ injected atomic.Bool }

func (*snapshotClusterReplyFault) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotClusterReplyFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (f *snapshotClusterReplyFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		if err != nil && !errors.Is(err, goredis.Nil) {
			return err
		}
		if len(cmds) != 4 {
			return err
		}
		proof, ok := cmds[0].(*goredis.SliceCmd)
		if !ok || proof.Name() != "hmget" || len(proof.Args()) != 4 || fmt.Sprint(proof.Args()[1]) != snapshotMetadataGraph {
			return err
		}
		if !strings.HasPrefix(fmt.Sprint(proof.Args()[2]), "n:") || len(proof.Val()) != 2 || !f.injected.CompareAndSwap(false, true) {
			return err
		}
		proof.SetVal([]any{"999", proof.Val()[1]})
		return err
	}
}

func TestSnapshotClusterMetadataProofObserverIsReadOnly(t *testing.T) {
	h, _, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE snapshot_proof_check(v INTEGER)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	s := st.(*statementImpl).conn.store
	var report string
	observer := &snapshotClusterProofObserver{directory: t.TempDir(), logf: func(format string, args ...any) { report = fmt.Sprintf(format, args...) }}
	fault := &snapshotClusterReplyFault{}
	s.client.AddHook(observer)
	s.client.AddHook(fault)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	_, err = s.getTable(ctx, "public", "snapshot_proof_check")
	if err == nil || !strings.Contains(err.Error(), "expected=999") {
		t.Fatal("diagnostic suppressed or repaired the original proof error")
	}
	if !fault.injected.Load() || observer.recognized.Load() != 1 || observer.captured.Load() != 1 || !strings.Contains(report, "fresh_healthy=true") || !strings.Contains(report, "private_record_saved=true") {
		t.Fatal("independent proof diagnostic did not observe the native pipeline")
	}
	info, err := os.Stat(filepath.Join(observer.directory, "metadata-proof-1.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("proof identities were not saved with private permissions")
	}
}
