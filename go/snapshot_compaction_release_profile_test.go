// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type compactionReleaseIO struct {
	reads, writes, readNS, writeNS atomic.Int64
}

type compactionReleaseConn struct {
	net.Conn
	io *compactionReleaseIO
}

// Preserve the native TLS transport's NetConn chain for go-redis socket peeks.
// Like tls.Conn, this wrapper deliberately does not implement syscall.Conn.
func (c *compactionReleaseConn) NetConn() net.Conn { return c.Conn }
func (c *compactionReleaseConn) Read(b []byte) (int, error) {
	start := time.Now()
	n, err := c.Conn.Read(b)
	c.io.reads.Add(1)
	c.io.readNS.Add(time.Since(start).Nanoseconds())
	return n, err
}
func (c *compactionReleaseConn) Write(b []byte) (int, error) {
	start := time.Now()
	n, err := c.Conn.Write(b)
	c.io.writes.Add(1)
	c.io.writeNS.Add(time.Since(start).Nanoseconds())
	return n, err
}

type compactionReleaseHook struct {
	io      *compactionReleaseIO
	samples []map[string]any // release calls are serial; caller joins heartbeat
	phase   func() string
	client  *goredis.Client
}

func (*compactionReleaseHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*compactionReleaseHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *compactionReleaseHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if compactionProfileCategory(cmd) != "control_release" {
			return next(ctx, cmd)
		}
		phase := ""
		if h.phase != nil {
			phase = h.phase()
			if phase == "" {
				return next(ctx, cmd)
			}
		}
		var misses, timeouts uint32
		if h.client != nil {
			before := h.client.PoolStats()
			misses, timeouts = before.Misses, before.Timeouts
		}
		reads, writes := h.io.reads.Load(), h.io.writes.Load()
		readNS, writeNS := h.io.readNS.Load(), h.io.writeNS.Load()
		start := time.Now()
		err := next(ctx, cmd)
		row := map[string]any{
			"hook_nanoseconds": time.Since(start).Nanoseconds(),
			"read_nanoseconds": h.io.readNS.Load() - readNS, "write_nanoseconds": h.io.writeNS.Load() - writeNS,
			"reads": h.io.reads.Load() - reads, "writes": h.io.writes.Load() - writes,
			"success": err == nil,
		}
		if h.client != nil {
			after := h.client.PoolStats()
			row["phase"], row["pool_misses"], row["pool_timeouts"] = phase, after.Misses-misses, after.Timeouts-timeouts
		}
		h.samples = append(h.samples, row)
		return err
	}
}

func newCompactionReleaseProfileClient(t *testing.T, raw *goredis.Client, io *compactionReleaseIO) *goredis.Client {
	t.Helper()
	options := *raw.Options()
	options.ContextTimeoutEnabled = true
	dial := goredis.NewDialer(&options)
	options.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &compactionReleaseConn{Conn: conn, io: io}, nil
	}
	client := goredis.NewClient(&options)
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(snapshotHook{})
	return client
}

// Separate attribution only. Never substitute these small empty-catalog probes
// for the six unchanged dense profiles, or interpret socket reads as server CPU.
func TestSnapshotCompactionReleaseProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_COMPACTION_RELEASE_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_COMPACTION_RELEASE_PROFILE for owned-fixture attribution")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v BIGINT)")
	io := &compactionReleaseIO{}
	client := newCompactionReleaseProfileClient(t, raw, io)
	observer := &compactionReleaseHook{io: io}
	client.AddHook(observer)
	s := &store{client: client, snapshots: true}
	if err := client.Ping(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	for repeat := range 32 {
		for offset := range 2 {
			delegated := (repeat+offset)%2 == 0
			var pin *snapshotPin
			if delegated {
				_, acquired, err := s.acquireCompactionCatalog(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				pin = acquired
			} else {
				token := snapshotID()
				root, err := s.snapshotControl(h.ctx, "acquire", token, "", "").Text()
				if err != nil {
					t.Fatal(err)
				}
				pin = &snapshotPin{store: s, token: token, root: root}
				if err := s.validateSnapshotMetadata(h.ctx, root); err != nil {
					_ = pin.control(h.ctx, "release", "").Err()
					t.Fatal(err)
				}
			}
			before := client.PoolStats()
			start, samples := time.Now(), len(observer.samples)
			if delegated {
				pin.release()
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := pin.control(ctx, "release", "").Err()
				cancel()
				if err != nil {
					t.Fatal(err)
				}
			}
			elapsed := time.Since(start)
			after := client.PoolStats()
			present, err := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.token).Result()
			if err != nil || present || len(observer.samples) != samples+1 {
				t.Fatal("release missing/duplicated or pin retained", present, err)
			}
			row := observer.samples[samples]
			if row["success"] != true {
				t.Fatal("release failed")
			}
			row["repeat"], row["delegated"] = repeat, delegated
			row["caller_nanoseconds"] = elapsed.Nanoseconds()
			row["pool_misses"] = after.Misses - before.Misses
			row["pool_timeouts"] = after.Timeouts - before.Timeouts
			results = append(results, row)
		}
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compaction_release_profile %s", encoded)
}
