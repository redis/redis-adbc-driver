// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotAdmissionObserved struct {
	snapshotHook
	seen  atomic.Bool
	ready chan struct{}
}

func (h *snapshotAdmissionObserved) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil && strings.Contains(err.Error(), "snapshot compaction publication pending") && h.seen.CompareAndSwap(false, true) {
			close(h.ready)
		}
		return err
	}
}
func TestSnapshotWriteAdmissionWaitsForCompactionPublication(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	for i := 0; i < 3; i++ {
		h.exec("UPDATE t SET v=v+1")
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	defer s.releaseSnapshotCompaction(context.Background(), token)
	deadline := time.Now().Add(3 * time.Second)
	for {
		j, err := s.readSnapshotCompaction(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if j != nil && j.Phase == "install" {
			break
		}
		if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("compaction did not reach its publication gate")
		}
	}
	observed := &snapshotAdmissionObserved{ready: make(chan struct{})}
	s.client.AddHook(observed)
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Second)
	defer cancel()
	type answer struct {
		pin *snapshotPin
		err error
	}
	done := make(chan answer, 1)
	go func() { _, pin, err := s.acquireSnapshot(ctx, true); done <- answer{pin, err} }()
	select {
	case <-observed.ready:
	case <-ctx.Done():
		t.Fatal("writer did not reach the active compaction gate")
	}
	select {
	case got := <-done:
		if got.pin != nil {
			got.pin.release()
		}
		t.Fatalf("ordinary writer must wait before execution: %v", got.err)
	case <-time.After(30 * time.Millisecond):
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		got.pin.release()
	case <-ctx.Done():
		t.Fatal("writer did not resume after publication admission reopened")
	}
	// Cancellation while waiting must return a timeout and must not allocate a pin.
	if _, err := s.snapshotControl(h.ctx, "compact_acquire", token, "", "").Text(); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(h.ctx, 30*time.Millisecond)
	defer stop()
	_, pin, err := s.acquireSnapshot(short, true)
	if pin != nil {
		pin.release()
		t.Fatal("cancelled admission allocated a pin")
	}
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusTimeout {
		t.Fatalf("waiting admission timeout: %v", err)
	}
	entries, err := raw.HKeys(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range entries {
		if strings.HasPrefix(field, "p:") {
			t.Fatal("admission leaked a pin")
		}
	}
}

func TestSnapshotSQLAdmissionWaitsForFullLineageWithoutReplay(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,256) AS g(i)")
	for i := 0; i < snapshotMaxSegments-1; i++ {
		h.exec("UPDATE t SET v=v+1 WHERE id=1")
	}
	s := snapshotTestStore(t, raw)
	counter := &snapshotDeltaWrites{prefixes: map[string]bool{}, indexes: map[string]bool{}}
	s.client.AddHook(counter)
	s.startSnapshotCompactor(newClientConfig(raw.Options()), timeouts{read: 5 * time.Second, write: 5 * time.Second, dial: 5 * time.Second})
	defer s.stopSnapshotCompactor()
	parsed, err := ParseScript("UPDATE t SET v=v+1 WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	ctx, cancel := context.WithTimeout(h.ctx, 20*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		res, err := e.execute(ctx, parsed[0], nil, nil, nil)
		if err != nil {
			t.Fatalf("ordinary write %d required caller retry: %v", i, err)
		}
		if res.affected != 1 {
			t.Fatal("incorrect affected rows", res.affected)
		}
	}
	if counter.rows != 20 {
		t.Fatalf("admission replayed input or wrote extra rows: %d", counter.rows)
	}
	h.expectRows("SELECT v FROM t WHERE id=1", "28")
	h.expectRows("SELECT COUNT(*) FROM t", "256")
}

// Internal stores can run without the client error hook. Acquisition must still
// distinguish a context deadline from an independent socket timeout, without retry.
type snapshotAdmissionSocketTimeout struct {
	snapshotHook
	deadline bool
	calls    atomic.Int64
}

func (h *snapshotAdmissionSocketTimeout) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := snapshotObservedControlArgs(cmd)
		if len(args) > 3 && strings.EqualFold(fmt.Sprint(args[0]), "EVAL") {
			n, _ := strconv.Atoi(fmt.Sprint(args[2]))
			if n >= 0 && len(args) > 3+n && fmt.Sprint(args[3+n]) == "acquire" {
				h.calls.Add(1)
				if h.deadline {
					<-ctx.Done()
				}
				return &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
			}
		}
		return next(ctx, cmd)
	}
}
func TestSnapshotAdmissionSocketTimeoutClassification(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("context_deadline_%t", deadline), func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			s := snapshotTestStore(t, raw)
			hook := &snapshotAdmissionSocketTimeout{deadline: deadline}
			s.client.AddHook(hook)
			ctx, cancel := context.WithTimeout(h.ctx, 2*time.Second)
			defer cancel()
			_, pin, err := s.acquireSnapshot(ctx, true)
			if pin != nil {
				pin.release()
				t.Fatal("failed acquisition allocated a pin")
			}
			want := adbc.StatusIO
			if deadline {
				want = adbc.StatusTimeout
			}
			var ae adbc.Error
			if hook.calls.Load() != 1 || !errors.As(err, &ae) || ae.Code != want {
				t.Fatalf("acquisition error/replay: calls=%d err=%v", hook.calls.Load(), err)
			}
			fields, err := raw.HKeys(h.ctx, snapshotCatalogKey).Result()
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range fields {
				if strings.HasPrefix(field, "p:") {
					t.Fatal("failed acquisition leaked a pin")
				}
			}
		})
	}
}
