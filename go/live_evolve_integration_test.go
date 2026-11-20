// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestLiveEvolutionSourcePreservation(t *testing.T) {
	f := newHashFixture(t, "it:live:evolve:")
	h := f.h
	key := f.prefix + string([]byte{'b', 0, 'k'})
	if !strings.ContainsRune(key, 0) {
		t.Fatal("binary fixture lacks zero byte")
	}
	f.hset(key, "n", "9007199254740993", "old", "keep")
	reg, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_evolve", Types: map[string]string{"n": "BIGINT"}, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_evolve"); err != nil {
			t.Error(err)
		}
	})
	f.hset(key, "label", "new", "unknown", string([]byte{0, 255}))
	if err := f.raw.Expire(h.ctx, key, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	before := f.raw.Dump(h.ctx, key).Val()
	expiry := f.raw.PExpireTime(h.ctx, key).Val()
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	o := LiveHashEvolutionOptions{HashAdoptOptions: HashAdoptOptions{Prefix: f.prefix, Table: "it_live_evolve", IndexColumns: []string{"label"}}, Columns: []string{"n", "label"}, ExpectedIndex: reg.Index, ExclusiveQuiescent: true}
	preview, err := EvolveLiveHashes(h.ctx, f.opts, o)
	if err != nil || preview.Applied || preview.Index != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	h.expectRows("SELECT old FROM redis_live.it_live_evolve", "keep")
	wrong := o
	wrong.ExpectedIndex = "wrong"
	wrong.Apply = true
	if _, err := EvolveLiveHashes(h.ctx, f.opts, wrong); err == nil {
		t.Fatal("accepted wrong index")
	}
	wrong = o
	wrong.ExclusiveQuiescent = false
	if _, err := EvolveLiveHashes(h.ctx, f.opts, wrong); err == nil {
		t.Fatal("accepted active writers")
	}
	o.Apply = true
	evolved, err := EvolveLiveHashes(h.ctx, f.opts, o)
	if err != nil || !evolved.Applied || evolved.Index == reg.Index {
		t.Fatalf("evolve: %+v %v", evolved, err)
	}
	h.expectRows("SELECT __key,n,label FROM redis_live.it_live_evolve WHERE label='new'", key+"|9007199254740993|new")
	h.expectError("SELECT old FROM redis_live.it_live_evolve", "old")
	if f.raw.Dump(h.ctx, key).Val() != before || f.raw.PExpireTime(h.ctx, key).Val() != expiry {
		t.Fatal("evolution changed source bytes or expiry")
	}
	if err := f.raw.Do(h.ctx, "FT.INFO", reg.Index).Err(); !isUnknownIndex(err) {
		t.Fatalf("old index retained: %v", err)
	}
	// Incompatible values must be rejected before any transition/index change.
	f.hset(key, "n", "bad")
	o.ExpectedIndex = evolved.Index
	if _, err := EvolveLiveHashes(h.ctx, f.opts, o); err == nil || !strings.Contains(err.Error(), "source values") {
		t.Fatalf("accepted incompatible value: %v", err)
	}
	f.hset(key, "n", "9007199254740993")
	h.expectRows("SELECT n,label FROM redis_live.it_live_evolve", "9007199254740993|new")
	// Renaming the SQL column leaves the underlying field intact.
	o.Columns = []string{"number", "label"}
	o.Renames = map[string]string{"n": "number"}
	next, err := EvolveLiveHashes(h.ctx, f.opts, o)
	if err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT number,label FROM redis_live.it_live_evolve", "9007199254740993|new")
	if next.Index == evolved.Index || f.raw.HGet(h.ctx, key, "old").Val() != "keep" {
		t.Fatal("rename/remove changed source")
	}
}

func TestLiveEvolutionResumeCancelAndIdentity(t *testing.T) {
	f := newHashFixture(t, "it:live:evolve-state:")
	h := f.h
	f.hset(f.prefix+"one", "n", "7")
	reg, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_evolve_state", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_evolve_state"); err != nil {
			t.Error(err)
		}
	})
	c, err := dialHashes(h.ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(h.ctx)
	stage := func() (string, *liveRecord) {
		raw, r, err := c.st.readLiveRecord(h.ctx, "it_live_evolve_state")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(r.Meta)
		var next tableMeta
		if err := json.Unmarshal(encoded, &next); err != nil {
			t.Fatal(err)
		}
		next.IndexName = liveIndexPrefix + snapshotID()
		next.LiveVersion = snapshotID()
		pending := &liveRecord{State: "evolving", Meta: r.Meta, Pending: &next}
		raw, err = c.st.swapLiveEvolution(h.ctx, raw, pending)
		if err != nil {
			t.Fatal(err)
		}
		return raw, pending
	}
	raw, pending := stage()
	if err := c.st.checkLiveIdentity(h.ctx, pending.Meta); err == nil {
		t.Fatal("old execution accepted transition")
	}
	if err := ResumeLiveHashes(h.ctx, f.opts, "it_live_evolve_state"); err == nil {
		t.Fatal("missing CREATE was replayed")
	}
	if err := CancelLiveHashEvolution(h.ctx, f.opts, "it_live_evolve_state", reg.Index); err == nil {
		t.Fatal("forgot unobserved CREATE identity")
	}
	if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_evolve_state"); err == nil {
		t.Fatal("unregister bypassed transition")
	}
	if err := c.st.searchDo(h.ctx, pending.Pending.index(), liveIndexArgs(pending.Pending)...).Err(); err != nil {
		t.Fatal(err)
	}
	if err := CancelLiveHashEvolution(h.ctx, f.opts, "it_live_evolve_state", reg.Index); err != nil {
		t.Fatal(err)
	}
	if err := c.st.checkLiveIdentity(h.ctx, pending.Meta); err != nil {
		t.Fatal(err)
	}
	// A direct CREATE ACL refusal is the only absent-candidate cancellation proof.
	_, pending = stage()
	pending.CreateRefused = true
	current, _, err := c.st.readLiveRecord(h.ctx, "it_live_evolve_state")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.st.swapLiveEvolution(h.ctx, current, pending); err != nil {
		t.Fatal(err)
	}
	if err := CancelLiveHashEvolution(h.ctx, f.opts, "it_live_evolve_state", reg.Index); err != nil {
		t.Fatal(err)
	}
	raw, pending = stage()
	if err := c.st.searchDo(h.ctx, pending.Pending.index(), liveIndexArgs(pending.Pending)...).Err(); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(h.ctx)
	cancel()
	if err := c.st.resumeLiveEvolution(canceled, raw, pending); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err := ResumeLiveHashes(h.ctx, f.opts, "it_live_evolve_state"); err != nil {
		t.Fatal(err)
	}
	if err := c.st.checkLiveIdentity(h.ctx, pending.Meta); err == nil {
		t.Fatal("stale definition accepted")
	}
	if err := c.st.searchDo(h.ctx, reg.Index, "FT.INFO", reg.Index).Err(); !isUnknownIndex(err) {
		t.Fatalf("old index not retired: %v", err)
	}
}

// A pipeline-level lost reply must not supply apparently valid schema facts.
type liveEvolutionLostPipeline struct{}

func (liveEvolutionLostPipeline) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (liveEvolutionLostPipeline) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (liveEvolutionLostPipeline) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() == "hgetall" {
				for _, c := range cmds {
					if typed, ok := c.(unmanagedProjectionCmd); ok {
						if value, ok := typed.Cmder.(*goredis.MapStringStringCmd); ok {
							value.SetVal(map[string]string{"n": "7"})
						}
					}
				}
				return io.ErrUnexpectedEOF
			}
		}
		return next(ctx, cmds)
	}
}
func TestLiveEvolutionRejectsPipelineReplyLoss(t *testing.T) {
	f := newHashFixture(t, "it:live:evolve-lost:")
	f.hset(f.prefix+"one", "n", "7")
	c, err := dialHashes(f.h.ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(f.h.ctx)
	client, ok := c.client.(*goredis.Client)
	if !ok {
		t.Skip("proxy client fault injection")
	}
	client.AddHook(liveEvolutionLostPipeline{})
	a := &adopter{c: c, o: HashAdoptOptions{Prefix: f.prefix}, res: &HashAdoption{Report: &HashReport{}}}
	if err := a.readLiveEvolution(f.h.ctx, []string{"n"}); err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("accepted outer pipeline loss: %v", err)
	}
}

type liveEvolutionNativeRefusal string

func (e liveEvolutionNativeRefusal) Error() string { return string(e) }
func (liveEvolutionNativeRefusal) RedisError()     {}

type liveEvolutionIndexFault struct {
	mode  string
	calls int
}

func (*liveEvolutionIndexFault) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*liveEvolutionIndexFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (f *liveEvolutionIndexFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if f.mode == "create-denied" && cmd.Name() == "ft.create" || f.mode == "drop-denied" && cmd.Name() == "ft.dropindex" {
			f.calls++
			err := liveEvolutionNativeRefusal("NOPERM this user has no permissions to run the '" + cmd.Name() + "' command")
			cmd.SetErr(err)
			return err
		}
		if f.mode == "drop-lost" && cmd.Name() == "ft.dropindex" {
			f.calls++
			if err := next(ctx, cmd); err != nil {
				return err
			}
			cmd.SetErr(io.ErrUnexpectedEOF)
			return io.ErrUnexpectedEOF
		}
		return next(ctx, cmd)
	}
}

func TestLiveEvolutionIndexRefusalAndLostCleanup(t *testing.T) {
	f := newHashFixture(t, "it:live:evolve-fault:")
	h := f.h
	f.hset(f.prefix+"one", "n", "7")
	reg, err := RegisterLiveHashes(h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_evolve_fault", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(h.ctx, f.opts, "it_live_evolve_fault"); err != nil {
			t.Error(err)
		}
	})
	c, err := dialHashes(h.ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close(h.ctx)
	client, ok := c.client.(*goredis.Client)
	if !ok {
		t.Skip("proxy client fault injection")
	}
	fault := &liveEvolutionIndexFault{mode: "create-denied"}
	client.AddHook(fault)
	o := LiveHashEvolutionOptions{HashAdoptOptions: HashAdoptOptions{Prefix: f.prefix, Table: "it_live_evolve_fault", Apply: true}, Columns: []string{"n"}, ExpectedIndex: reg.Index, ExclusiveQuiescent: true}
	if _, err := evolveLiveHashes(h.ctx, c, o); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatalf("CREATE refusal: %v", err)
	}
	_, record, err := c.st.readLiveRecord(h.ctx, o.Table)
	if err != nil || record.State != "evolving" || !record.CreateRefused || fault.calls != 1 {
		t.Fatalf("refusal lost: %+v %v calls=%d", record, err, fault.calls)
	}
	if err := CancelLiveHashEvolution(h.ctx, f.opts, o.Table, reg.Index); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"drop-denied", "drop-lost"} {
		fault.mode = mode
		fault.calls = 0
		if _, err := evolveLiveHashes(h.ctx, c, o); err == nil {
			t.Fatalf("ignored %s", mode)
		}
		_, record, err = c.st.readLiveRecord(h.ctx, o.Table)
		if err != nil || record.State != "retiring" || record.Pending.index() != o.ExpectedIndex || fault.calls != 1 {
			t.Fatalf("lost retirement identity: %+v %v calls=%d", record, err, fault.calls)
		}
		if err := ResumeLiveHashes(h.ctx, f.opts, o.Table); err != nil {
			t.Fatal(err)
		}
		if f.raw.HGet(h.ctx, f.prefix+"one", "n").Val() != "7" {
			t.Fatal("cleanup modified HASH")
		}
		o.ExpectedIndex = record.Meta.index()
	}
}
