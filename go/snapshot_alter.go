// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Definition-only DDL shares immutable row/index segments. Changes requiring
// conversion, fields or validation rewrite privately. Neither path alters rows
// or indexes reachable by an older manifest.
// Ordinary DML continues to append deltas without copying existing rows.
func (e *executor) alterSnapshotTable(ctx context.Context, source *tableMeta, cmds []AlterCmd) error {
	in := &alterInputs{}
	if err := e.viewDependencies(ctx, source, cmds, in); err != nil {
		return err
	}
	target := cloneMeta(source)
	res, err := e.applyAlter(ctx, target, cmds, in)
	if err != nil {
		return err
	}
	if res.dropped {
		owned, err := e.store.ownedSequences(ctx, source.Schema, source.Name, tableOID(source))
		if err != nil {
			return err
		}
		for _, seq := range owned {
			field := seq.OwnerField
			if field == "" {
				if i, ok := source.resolve(seq.Column); ok {
					field = source.Columns[i].field()
				}
			}
			if field != "" && !slices.ContainsFunc(target.Columns, func(c columnMeta) bool { return c.field() == field }) {
				ref := TableName{Schema: seq.Schema, Name: seq.Name}
				if !slices.Contains(res.droppedSequences, ref) {
					res.droppedSequences = append(res.droppedSequences, ref)
				}
			}
		}
	}
	// Retain the existing guarded descriptor pipeline below for both paths.
	// resolveSnapshotMetadata makes its destination private and retains the
	// shared row-reference edges; executeSnapshot still seals and CAS-publishes
	// the workspace. No committed descriptor is written directly.
	// A nil action list means explicit rekeying and must allocate new data.
	definitionOnly := len(cmds) > 0 && !res.rewrite && !res.dropped && len(res.added) == 0 &&
		!res.validates() && len(res.rechecked) == 0 && len(res.droppedIndexes) == 0 && len(res.droppedSequences) == 0
	if !definitionOnly {
		names, err := e.store.claimNames(ctx, source.Schema, source.Name, source.SnapshotAccessID)
		if err != nil {
			return err
		}
		target.KeyPrefix, target.IndexName, target.PrefixGen = names.prefix, names.index, 0
		target.SnapshotLineage = snapshotDataID(names.prefix)
		target.SnapshotSegments, target.SnapshotRows, target.SnapshotOverlay = nil, nil, false
		deferred := deferSnapshotIndex(ctx, target)
		rw := &rewrite{start: source, target: target, res: res}
		if err := rw.prepare(ctx, e); err != nil {
			return err
		}
		scan := newSnapshotDeltaScan(e.store, source, nil)
		for {
			_, rows, err := scan.next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			images := make([]snapshotInsertRow, 0, len(rows))
			for _, row := range rows {
				fields, err := rw.convert(row)
				if err != nil {
					return err
				}
				if target.SnapshotSourceKey {
					if key, ok := row[snapshotSourceKeyField]; ok {
						fields = append(fields, snapshotSourceKeyField, key)
					}
				}
				key := target.prefix() + row[rowIDField]
				images = append(images, snapshotInsertRow{key: key, member: snapshotDirectoryMember(key), fields: fields})
			}
			if err := e.store.writeSnapshotImages(ctx, target.prefix(), images, false); err != nil {
				return err
			}
		}
		target = rw.next(source)
		if !deferred {
			if err := e.store.searchDo(ctx, target.index(), snapshotScannedIndexArgs(target)...).Err(); err != nil {
				return err
			}
		}
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	pipe := e.store.redis(ctx).Pipeline()
	pipe.Set(ctx, metaKey(source.Schema, source.Name), raw, 0)
	for _, name := range res.droppedIndexes {
		pipe.Del(ctx, sqlIndexKey(source.Schema, name))
	}
	if _, err = pipe.Exec(ctx); err != nil {
		return err
	}
	for _, ref := range res.droppedSequences {
		seq, err := e.store.getSequence(ctx, ref.Schema, ref.Name)
		var ae adbc.Error
		if err != nil && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
			continue
		}
		if err != nil {
			return err
		}
		if (seq.OwnerOID != 0 && seq.OwnerOID == tableOID(source)) || (seq.Owned.Schema == source.Schema && seq.Owned.Name == source.Name) {
			if err := e.mutateSequence(ctx, ref.Schema, ref.Name, func(*sequenceMeta) error { return nil }, true, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// Snapshot rekeying allocates a new immutable physical identity under the
// catalog tag, then renames its private descriptor in the same publication.
// It never moves or deletes rows still referenced by an older manifest.
func (e *executor) rekeySnapshotTable(ctx context.Context, source *tableMeta, to TableName) error {
	if err := e.alterSnapshotTable(ctx, source, nil); err != nil {
		return err
	}
	next, err := e.store.getTable(ctx, source.Schema, source.Name)
	if err != nil {
		return err
	}
	exec := *e
	exec.rekey = false
	return exec.renameTable(ctx, next, to)
}
