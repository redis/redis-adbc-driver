// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotPointReadObserver struct {
	mu                    sync.Mutex
	calls, hashes, proofs int
	keys                  []string
	refuse                error
}

func (*snapshotPointReadObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *snapshotPointReadObserver) record(cmd goredis.Cmder, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if compactionProfileCategory(cmd) == "point_image_read" {
		if missing, denied := snapshotControlScriptRefusal(err); missing || denied {
			return
		}
		h.calls++
		a := cmd.Args()
		if len(a) > 4 {
			h.keys = append(h.keys, fmt.Sprint(a[3]))
		}
	} else {
		switch cmd.Name() {
		case "hgetall", "hmget":
			h.hashes++
		case "zcard", "zscore":
			h.proofs++
		}
	}
}
func (h *snapshotPointReadObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if compactionProfileCategory(cmd) == "point_image_read" {
			if marker, ok := cmd.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
				return errors.New("point read lost no-retry marker")
			}
			h.mu.Lock()
			refuse := h.refuse
			h.mu.Unlock()
			if refuse != nil {
				cmd.SetErr(refuse)
				h.record(cmd, refuse)
				return refuse
			}
		}
		err := next(ctx, cmd)
		h.record(cmd, err)
		return err
	}
}
func (h *snapshotPointReadObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			h.record(cmd, cmd.Err())
		}
		return err
	}
}
func (h *snapshotPointReadObserver) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = 0
	h.hashes = 0
	h.proofs = 0
	h.keys = nil
}

func snapshotPointWriteFixture(t *testing.T) (*sqlHarness, *goredis.Client, *store, context.Context, *tableMeta, *snapshotPointReadObserver) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,label VARCHAR)")
	h.exec("INSERT INTO t VALUES(1,9007199254740993,'first'),(2,NULL,'second'),(3,30,'third')")
	h.exec("UPDATE t SET v=22 WHERE __rowid=2")
	h.exec("DELETE FROM t WHERE __rowid=3")
	s := snapshotTestStore(t, raw)
	base, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	work, id, err := s.beginSnapshotWrite(base, pin)
	if err != nil {
		pin.release()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.abortSnapshot(pin, id); pin.release() })
	if err := s.prepareSnapshotTable(work, "public", "t"); err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(work, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	observer := &snapshotPointReadObserver{}
	s.client.AddHook(observer)
	return h, raw, s, work, meta, observer
}
func TestSnapshotPointWriteReadUsesExactSegmentProof(t *testing.T) {
	h, _, s, ctx, m, observer := snapshotPointWriteFixture(t)
	for _, full := range []bool{false, true} {
		var fields []string
		if !full {
			fields = []string{"id", "v", "label"}
		}
		for _, tc := range []struct {
			id    int
			value string
			calls int
		}{{1, "9007199254740993", 4}, {2, "22", 3}, {3, "", 2}, {999, "", 4}} {
			observer.reset()
			rows, err := s.fetchSnapshotVisible(ctx, m, []string{fmt.Sprint(m.prefix(), tc.id)}, fields)
			if err != nil || len(rows) != 1 {
				t.Fatal("point read failed", full, tc.id, err)
			}
			if tc.value == "" {
				if rows[0] != nil {
					t.Fatal("tombstone/absence exposed row", tc.id, rows)
				}
			} else if rows[0] == nil || rows[0]["v"] != tc.value {
				t.Fatal("point read changed exact value", tc.id, rows)
			}
			if observer.calls != tc.calls || observer.hashes != 0 || observer.proofs != 0 {
				t.Fatal("point path missed fusion or read past newest winner", full, tc.id, observer.calls, observer.hashes, observer.proofs)
			}
		}
	}
	// A point UPDATE first reads old values, then its complete image. It must
	// subsequently read the private replacement rather than retain a stale image.
	snapshotExecuteTest(t, s, ctx, "UPDATE t SET v=44 WHERE __rowid=1")
	observer.reset()
	rows, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1"}, nil)
	if err != nil || rows[0]["v"] != "44" || observer.calls != 1 {
		t.Fatal("private image not visible", rows, err, observer.calls)
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|9007199254740993", "2|22")
}
func TestSnapshotPointWriteReadFailuresDoNotReplay(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		fallback bool
	}{
		{"top_level_script_acl", controlScriptNativeError("NOPERM injected script key selector refusal"), true},
		{"inner_data_acl", controlScriptNativeError("ERR snapshot point read native command failed: NOPERM injected HASH refusal"), false},
		{"unknown_reply", io.ErrUnexpectedEOF, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, s, ctx, m, observer := snapshotPointWriteFixture(t)
			observer.refuse = tc.err
			rows, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1"}, nil)
			if tc.fallback {
				if err != nil || len(rows) != 1 || rows[0]["v"] != "9007199254740993" || observer.hashes == 0 {
					t.Fatal("definite selector refusal lost native fallback", rows, err, observer.hashes)
				}
			} else if err == nil || observer.hashes != 0 || observer.proofs != 0 {
				t.Fatal("unknown/inner refusal was replayed", err, observer.hashes, observer.proofs)
			}
			if observer.calls != 1 {
				t.Fatal("point script attempted more than once", observer.calls)
			}
		})
	}
}
func TestSnapshotPointWriteReadRejectsCorruptNewerImages(t *testing.T) {
	for _, kind := range []string{"missing_row", "missing_row_and_directory", "wrong_row_id", "missing_row_id", "wrong_hash_type", "wrong_directory_type"} {
		t.Run(kind, func(t *testing.T) {
			_, raw, s, ctx, m, observer := snapshotPointWriteFixture(t)
			segment := m.snapshotSegments()[len(m.snapshotSegments())-2] // Published tombstone for row 3.
			key := segment.Prefix + "3"
			directory := snapshotMembers(snapshotDataID(segment.Prefix))
			image, err := raw.HGetAll(ctx, key).Result()
			if err != nil {
				t.Fatal(err)
			}
			members, err := raw.ZRangeWithScores(ctx, directory, 0, -1).Result()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = raw.Del(ctx, key, directory).Err()
				if len(image) > 0 {
					_ = raw.HSet(ctx, key, image).Err()
				}
				if len(members) > 0 {
					_ = raw.ZAdd(ctx, directory, members...).Err()
				}
			}()
			switch kind {
			case "missing_row":
				err = raw.Del(ctx, key).Err()
			case "missing_row_and_directory":
				err = raw.Del(ctx, key, directory).Err()
			case "wrong_row_id":
				err = raw.HSet(ctx, key, rowIDField, "2").Err()
			case "missing_row_id":
				err = raw.HDel(ctx, key, rowIDField).Err()
			case "wrong_hash_type":
				if err = raw.Del(ctx, key).Err(); err == nil {
					err = raw.Set(ctx, key, "wrong type", 0).Err()
				}
			case "wrong_directory_type":
				if err = raw.Del(ctx, key, directory).Err(); err == nil {
					err = raw.Set(ctx, directory, "wrong type", 0).Err()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, fields := range [][]string{nil, {"id", "v"}} {
				observer.reset()
				_, err = s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "3"}, fields)
				if err == nil {
					t.Fatal("newer corruption exposed an older row", kind, fields)
				}
				if observer.calls != 2 || observer.hashes != 0 || observer.proofs != 0 {
					t.Fatal("corrupt native read selected fallback or older segment", kind, observer.calls, observer.hashes, observer.proofs)
				}
			}
		})
	}
}
func TestSnapshotPointReadEligibilityRetainsExistingPaths(t *testing.T) {
	_, _, s, ctx, m, observer := snapshotPointWriteFixture(t)
	seg := m.snapshotSegments()[0]
	for _, fields := range [][]string{make([]string, 255), {strings.Repeat("x", snapshotWriteBatchBytes+1)}} {
		observer.reset()
		_, handled, err := s.fetchSnapshotPoint(ctx, seg, seg.Prefix+"1", fields)
		if handled || err != nil || observer.calls != 0 {
			t.Fatal("oversized arguments entered Lua", handled, err, observer.calls)
		}
	}
	observer.reset()
	strict := context.WithValue(ctx, snapshotDirectReadKey{}, true)
	rows, err := s.fetchSnapshotVisible(strict, m, []string{m.prefix() + "1"}, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != "9007199254740993" || observer.calls != 0 {
		t.Fatal("strict direct proof changed or entered write specialization", rows, err, observer.calls)
	}
	observer.reset()
	rows, err = s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + "1", m.prefix() + "2"}, nil)
	if err != nil || len(rows) != 2 || rows[0]["v"] != "9007199254740993" || rows[1]["v"] != "22" || observer.calls != 0 {
		t.Fatal("multi-row native path changed", rows, err, observer.calls)
	}
}

func TestSnapshotPointWriteReadKeepsLargeIDsAndBinaryValues(t *testing.T) {
	_, _, s, ctx, m, observer := snapshotPointWriteFixture(t)
	const id = "9223372036854775807"
	label := string([]byte{'a', 0, 'b'})
	opaque := string([]byte{0, 0xff, '\n'})
	if !strings.ContainsRune(label, 0) || opaque[0] != 0 {
		t.Fatal("fixture lacks actual zero bytes")
	}
	image := snapshotInsertRow{key: m.prefix() + id, member: "09223372036854775807", fields: []any{rowIDField, id, "id", "9007199254740993", "v", "9007199254740993", "label", label, "opaque", opaque}}
	if err := s.writeSnapshotImages(ctx, m.prefix(), []snapshotInsertRow{image}, true); err != nil {
		t.Fatal(err)
	}
	for _, fields := range [][]string{nil, {"id", "v", "label"}, {}} {
		observer.reset()
		rows, err := s.fetchSnapshotVisible(ctx, m, []string{m.prefix() + id}, fields)
		if err != nil || len(rows) != 1 || rows[0] == nil || rows[0][rowIDField] != id || observer.calls != 1 {
			t.Fatal("large row ID rounded or missed", rows, err, observer.calls)
		}
		if fields == nil || len(fields) > 0 {
			if rows[0]["v"] != "9007199254740993" || rows[0]["label"] != label {
				t.Fatal("stored value changed", rows)
			}
		}
		if fields == nil && rows[0]["opaque"] != opaque {
			t.Fatal("full image changed opaque bytes")
		}
	}
}
