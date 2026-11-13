// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type compactionCatalogObservation struct{}

type compactionCatalogObserver struct {
	mu                           sync.Mutex
	combined, separate, releases int
	pin                          string
	lose, denyCleanup            bool
}

func (*compactionCatalogObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *compactionCatalogObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *compactionCatalogObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		if len(a) < 4 || a[1] != snapshotControl {
			return next(ctx, cmd)
		}
		n, err := strconv.Atoi(fmt.Sprint(a[2]))
		if err != nil || len(a) <= n+5 {
			return next(ctx, cmd)
		}
		op := fmt.Sprint(a[n+3])
		if op == "acquire" && ctx.Value(compactionCatalogObservation{}) == true {
			h.mu.Lock()
			h.separate++
			h.mu.Unlock()
			err = errors.New("separate catalog acquire must be combined with the job lease")
			cmd.SetErr(err)
			return err
		}
		if op == "release" {
			h.mu.Lock()
			owned := fmt.Sprint(a[n+4]) == h.pin
			deny := owned && h.denyCleanup
			if owned {
				h.releases++
			}
			h.mu.Unlock()
			if deny {
				err = errors.New("catalog pin cleanup unavailable")
				cmd.SetErr(err)
				return err
			}
		}
		err = next(ctx, cmd)
		missing, denied := snapshotControlScriptRefusal(err)
		if missing || denied {
			return err
		}
		if err == nil && op == "compact_acquire_catalog" {
			h.mu.Lock()
			h.combined++
			h.pin = fmt.Sprint(a[n+5])
			lose := h.lose
			h.lose = false
			h.mu.Unlock()
			if lose {
				err = errors.New("lost combined compaction lease/pin reply")
				cmd.SetErr(err)
			}
		}
		return err
	}
}

func newCompactionControlJob(t *testing.T) (*sqlHarness, *goredis.Client, *store, string, *snapshotCompactionJob) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,257) AS g(i)")
	for range 3 {
		h.exec("UPDATE t SET v=v+1 WHERE id<=128")
	}
	s := snapshotTestStore(t, raw)
	token := snapshotID()
	if started, err := s.startSnapshotCompaction(h.ctx, token); err != nil || !started {
		t.Fatal("start compaction", started, err)
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil {
		t.Fatal("read job", err)
	}
	t.Cleanup(func() { _ = s.releaseSnapshotCompaction(context.Background(), token) })
	return h, raw, s, token, job
}

func TestSnapshotCompactionCombinedCatalogAcquire(t *testing.T) {
	h, raw, s, token, job := newCompactionControlJob(t)
	observer := &compactionCatalogObserver{}
	s.client.AddHook(observer)
	ctx := context.WithValue(h.ctx, compactionCatalogObservation{}, true)
	if progressed, err := s.snapshotCompactionStep(ctx, token); err != nil || !progressed {
		t.Fatal("combined step", progressed, err)
	}
	observer.mu.Lock()
	combined, separate, releases, pin := observer.combined, observer.separate, observer.releases, observer.pin
	observer.mu.Unlock()
	if combined != 1 || separate != 0 || releases != 1 {
		t.Fatal("control counts", combined, separate, releases)
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin).Val() {
		t.Fatal("step retained catalog pin")
	}
	durable, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || durable == nil || durable.After <= job.After {
		t.Fatal("bounded copy checkpoint", err)
	}
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != snapshotCompactionBatchRows {
		t.Fatal("copy bound changed")
	}
	if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	snapshotDrainCompactionTest(t, s)
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|9007199254740993|9007199254740996")
}

func TestSnapshotCompactionCombinedReplyLoss(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprint(deny), func(t *testing.T) {
			h, raw, s, token, job := newCompactionControlJob(t)
			observer := &compactionCatalogObserver{lose: true, denyCleanup: deny}
			s.client.AddHook(observer)
			if progressed, err := s.snapshotCompactionStep(h.ctx, token); err == nil || progressed {
				t.Fatal("unknown acquisition accepted", progressed, err)
			}
			observer.mu.Lock()
			combined, releases, pin := observer.combined, observer.releases, observer.pin
			observer.denyCleanup = false
			observer.mu.Unlock()
			if combined != 1 || releases != 1 {
				t.Fatal("unknown combined mutation replayed or cleanup repeated", combined, releases)
			}
			durable, err := s.readSnapshotCompaction(h.ctx)
			if err != nil || durable == nil || durable.After != job.After || raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 0 {
				t.Fatal("unknown acquisition advanced copy", err)
			}
			if exists := raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin).Val(); exists != deny {
				t.Fatal("unknown pin cleanup", exists, deny)
			}
			if deny {
				var p struct {
					Root    string `json:"root"`
					Expires int64  `json:"expires"`
				}
				if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "p:"+pin).Val()), &p); err != nil || p.Root == "" || p.Expires <= time.Now().UnixMilli() || p.Expires > time.Now().Add(snapshotLease).UnixMilli() {
					t.Fatal("unseen pin has no bounded expiry", err)
				}
				// Expire only the test-created unknown pin, then let the ordinary
				// next acquisition prune it through its existing server-time policy.
				p.Expires = 1
				b, _ := json.Marshal(p)
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+pin, b).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if progressed, err := s.snapshotCompactionStep(h.ctx, token); err != nil || !progressed {
				t.Fatal("durable resume", progressed, err)
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin).Val() {
				t.Fatal("expired unknown pin remained")
			}
			if err := s.releaseSnapshotCompaction(h.ctx, token); err != nil {
				t.Fatal(err)
			}
			snapshotDrainCompactionTest(t, s)
			h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "257|9007199254740993|9007199254740996")
		})
	}
}

func TestSnapshotCompactionCombinedRefusalIsUnchanged(t *testing.T) {
	for _, mode := range []string{"foreign_owner", "reader_limit"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, s, token, job := newCompactionControlJob(t)
			before := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Val()
			if mode == "reader_limit" {
				root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
				for range 128 {
					b, _ := json.Marshal(map[string]any{"root": root, "expires": time.Now().Add(snapshotLease).UnixMilli()})
					if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+snapshotID(), b).Err(); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				token = snapshotID()
			}
			got, _, pin, err := s.acquireCompactionStepCatalog(h.ctx, token)
			if pin != nil {
				pin.release()
				t.Fatal("refused acquisition created pin")
			}
			if got != nil {
				t.Fatal("refused acquisition returned job")
			}
			if mode == "reader_limit" && (err == nil || !strings.Contains(err.Error(), "reader limit")) {
				t.Fatal("reader refusal lost", err)
			}
			if mode == "foreign_owner" && err != nil {
				t.Fatal("foreign ownership refusal changed", err)
			}
			if after := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Val(); after != before {
				t.Fatal("refusal changed compaction lease")
			}
		})
	}
}

func TestSnapshotCompactionCombinedRetainsMetadataProof(t *testing.T) {
	h, raw, s, token, job := newCompactionControlJob(t)
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := raw.ZAdd(h.ctx, snapshotMetadataKeys(root), goredis.Z{Score: 0, Member: "corrupt-test-entry"}).Err(); err != nil {
		t.Fatal(err)
	}
	if progressed, err := s.snapshotCompactionStep(h.ctx, token); err == nil || progressed {
		t.Fatal("corrupt catalog accepted", progressed, err)
	}
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != 0 {
		t.Fatal("corrupt proof copied rows")
	}
	fields, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		if strings.HasPrefix(key, "p:") {
			t.Fatal("failed metadata proof leaked a read pin")
		}
	}
	if err := raw.ZRem(h.ctx, snapshotMetadataKeys(root), "corrupt-test-entry").Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCompactionCombinedEvalOnlySoftware(t *testing.T) {
	uri := os.Getenv("REDIS_SCRIPT_EVAL_ONLY_URI")
	if uri == "" {
		t.Skip("requires an owned Software EVAL-only operator fixture")
	}
	h, raw, _, token, original := newCompactionControlJob(t)
	opts, err := goredis.ParseURL(uri)
	if err != nil || opts.Addr != raw.Options().Addr || opts.DB != raw.Options().DB || opts.Username == "" || raw.Options().TLSConfig == nil {
		t.Fatal("operator differs from owned strict-TLS fixture")
	}
	opts.TLSConfig = raw.Options().TLSConfig.Clone()
	opts.ContextTimeoutEnabled = true
	client := goredis.NewClient(opts)
	defer client.Close()
	client.AddHook(snapshotHook{})
	observer := &controlScriptWireObserver{}
	observer.enabled.Store(true)
	client.AddHook(observer)
	s := &store{client: client, snapshots: true}
	job, ctx, pin, err := s.acquireCompactionStepCatalog(h.ctx, token)
	if err != nil || pin == nil || job == nil || job.ID != original.ID {
		t.Fatal("EVAL-only combined acquire failed")
	}
	defer pin.release()
	if err := s.validateSnapshotMetadata(ctx, pin.root); err != nil {
		t.Fatal("EVAL-only metadata proof failed")
	}
	if err := pin.valid(ctx); err != nil {
		t.Fatal("EVAL-only read pin invalid")
	}
	pin.release()
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.token).Val() {
		t.Fatal("EVAL-only read pin leaked")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.denied != 1 || observer.successes != 3 || observer.noRetry || !s.controlScript.evalOnly.Load() {
		t.Fatal("combined acquire lost one-refusal EVAL-only compatibility", observer.denied, observer.successes)
	}
}
