// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// This fixture also runs against the pre-barrier source: the negative case
// proves why a durable incomplete-capture record must fence server mutation.
func snapshotClusterCaptureFixture(t *testing.T, count int) (*sqlHarness, *store, context.Context, *tableMeta, *snapshotImportSpool) {
	t.Helper()
	h, _, _ := snapshotClusterHarness(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(h.ctx) })
	s := st.(*statementImpl).conn.store
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pin.release)
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.abortSnapshot(pin, id) })
	meta := &tableMeta{Schema: "public", Name: "capture_barrier", SnapshotSourceKey: true,
		SnapshotImportSource: snapshotImportIdentity(&tableMeta{KeyPrefix: "capture:"}),
		Columns:              []columnMeta{{Name: "v", Type: typeInt64}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	spool := newSnapshotImportSpool(wctx, s, meta)
	var rows []map[string]Value
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("capture:%05d", i)
		if i == 0 {
			key = "\x00\xfforiginal-key"
		}
		rows = append(rows, map[string]Value{keyField: stringValue(key), "v": intValue(typeInt64, 9007199254740993)})
	}
	if err := spool.append(wctx, rows); err != nil {
		t.Fatal(err)
	}
	return h, s, wctx, meta, spool
}

func TestSnapshotClusterIncompleteImportCaptureCannotEmitOrPublish(t *testing.T) {
	for _, op := range []string{"output", "plain_output", "raw_output", "seal", "publish"} {
		t.Run(op, func(t *testing.T) {
			h, s, ctx, meta, _ := snapshotClusterCaptureFixture(t, 2)
			scope := snapshotOf(ctx)
			field := "w:" + scope.id
			var work map[string]any
			raw, err := s.client.HGet(h.ctx, snapshotCatalogKey, field).Bytes()
			if err != nil || json.Unmarshal(raw, &work) != nil {
				t.Fatal("private workspace absent", err)
			}
			work["import_capture"] = map[string]any{"state": "capturing", "data": snapshotDataID(meta.prefix())}
			raw, _ = json.Marshal(work)
			if err := s.client.HSet(h.ctx, snapshotCatalogKey, field, raw).Err(); err != nil {
				t.Fatal(err)
			}
			journal := &snapshotImportOutput{rows: snapshotWorkspace(scope.id) + "import:rows:" + scope.id, keys: snapshotWorkspace(scope.id) + "import:keys:" + scope.id}
			if op == "output" || op == "plain_output" {
				if op == "output" {
					ctx = context.WithValue(ctx, snapshotImportOutputKey{}, journal)
				}
				rows := []snapshotInsertRow{{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", snapshotSourceKeyField, "\x00\xfforiginal-key", "v", "9007199254740993"}}}
				err = s.insertSnapshotBatch(ctx, meta.prefix(), rows)
			} else if op == "raw_output" {
				err = s.client.HSet(ctx, meta.prefix()+"1", rowIDField, "1", "v", "9007199254740993").Err()
			} else {
				err = s.snapshotControl(ctx, op, scope.pin.token, scope.id, scope.pin.root).Err()
			}
			if err == nil || !strings.Contains(err.Error(), "import") {
				t.Fatalf("%s admitted incomplete capture: %v", op, err)
			}
			if s.client.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 ||
				s.client.Exists(h.ctx, meta.prefix()+"1").Val() != 0 ||
				s.client.HLen(h.ctx, journal.rows).Val() != 2 || s.client.ZCard(h.ctx, journal.keys).Val() != 2 ||
				s.client.HGet(h.ctx, snapshotCatalogKey, "root").Val() != scope.pin.root {
				t.Fatal("refused capture mutated output, input or committed root")
			}
		})
	}
}
