// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotImportPage = 128

type snapshotImportSpoolKey struct{}
type snapshotImportOutputKey struct{}
type snapshotImportOutput struct{ rows, keys string }
type snapshotImportSpool struct {
	store      *store
	meta       *tableMeta
	rows, keys string
	begun      bool
}

// The journal is ordinary reference-owned private metadata. No application
// index can capture it, and abort/expiry reclaims it through existing value GC.
func newSnapshotImportSpool(ctx context.Context, s *store, meta *tableMeta) *snapshotImportSpool {
	id := snapshotOf(ctx).id
	return &snapshotImportSpool{store: s, meta: meta, rows: metaPrefix + "import:rows:" + id, keys: metaPrefix + "import:keys:" + id}
}

func (p *snapshotImportSpool) unseen(ctx context.Context, keys []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for start := 0; start < len(keys); start += snapshotImportPage {
		page := keys[start:min(start+snapshotImportPage, len(keys))]
		values, err := p.store.redis(ctx).HMGet(ctx, p.rows, page...).Result()
		if err != nil {
			return nil, err
		}
		for i, key := range page {
			if values[i] == nil && !seen[key] {
				out = append(out, key)
				seen[key] = true
			}
		}
	}
	return out, nil
}

func snapshotImportUnseen(ctx context.Context, keys []string) ([]string, error) {
	spool, _ := ctx.Value(snapshotImportSpoolKey{}).(*snapshotImportSpool)
	if spool == nil {
		return nil, errorf(adbc.StatusInternal, "import capture has no private journal")
	}
	return spool.unseen(ctx, keys)
}

// Native stored-value strings preserve records/arrays, exact decimals and raw
// string/key bytes. Length zero means NULL; length one means a non-NULL empty
// value. No JSON Unicode replacement or floating-point conversion is involved.
func encodeSnapshotImportRow(meta *tableMeta, row map[string]Value) string {
	data := binary.AppendUvarint(nil, uint64(len(meta.Columns)))
	for _, col := range meta.Columns {
		value, ok := row[col.Name]
		if !ok || value.Null {
			data = binary.AppendUvarint(data, 0)
			continue
		}
		stored := encodeStored(value)
		data = binary.AppendUvarint(data, uint64(len(stored))+1)
		data = append(data, stored...)
	}
	return string(data)
}

func decodeSnapshotImportRow(meta *tableMeta, data string) ([]Value, error) {
	count, n := binary.Uvarint([]byte(data))
	if n <= 0 || count != uint64(len(meta.Columns)) {
		return nil, errorf(adbc.StatusIO, "invalid import journal schema")
	}
	data = data[n:]
	row := make([]Value, len(meta.Columns))
	for i, col := range meta.Columns {
		size, n := binary.Uvarint([]byte(data))
		if n <= 0 {
			return nil, errorf(adbc.StatusIO, "invalid import journal value length")
		}
		data = data[n:]
		if size == 0 {
			row[i] = nullValue(col.Type)
			continue
		}
		size--
		if size > uint64(len(data)) {
			return nil, errorf(adbc.StatusIO, "truncated import journal value")
		}
		value, err := decodeStored(data[:int(size)], col.Type)
		if err != nil {
			return nil, err
		}
		row[i] = value
		data = data[int(size):]
	}
	if len(data) != 0 {
		return nil, errorf(adbc.StatusIO, "trailing import journal data")
	}
	return row, nil
}

// Bound native journal commands independently of the statement's cumulative
// metadata counter. The normal server fence and native-value registration still
// apply, and uncertain replies fail capture rather than replaying it.
func (p *snapshotImportSpool) mutate(ctx context.Context, command, key string, args ...any) error {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || len(args) > snapshotImportPage*2 {
		return errorf(adbc.StatusInvalidState, "invalid import journal mutation")
	}
	bytes := len(key) + 1024
	for _, arg := range args {
		bytes += len(fmt.Sprint(arg))
	}
	if bytes > snapshotMaxBytes || len(args) > 2 && bytes > snapshotWriteBatchBytes {
		return errorf(adbc.StatusIO, "import journal page exceeds byte limit")
	}
	physical, err := resolveSnapshotMetadata(ctx, key[len(metaPrefix):], true, command)
	if err != nil {
		return err
	}
	fields := []any{command, physical}
	fields = append(fields, args...)
	wrapped := fenceSnapshotCommand(ctx, goredis.NewCmd(ctx, fields...))
	return once(ctx, p.store.client, wrapped.Args()...).Err()
}

func (p *snapshotImportSpool) append(ctx context.Context, rows []map[string]Value) error {
	if err := p.begin(ctx); err != nil {
		return err
	}
	// A SCAN page can repeat keys within itself. Keep only this bounded page's
	// first occurrence; earlier pages are deduplicated by the private journal.
	for start := 0; start < len(rows); {
		var fields, members []any
		bytes := 1024
		seen := map[string]bool{}
		for start < len(rows) && len(fields) < snapshotImportPage*2 {
			row := rows[start]
			key, ok := row[keyField]
			if !ok || key.Null || key.T.Kind != KindString {
				return errorf(adbc.StatusIO, "import source row has no original key")
			}
			payload := encodeSnapshotImportRow(p.meta, row)
			size := len(key.S) + len(payload) + 32
			if len(fields) > 0 && bytes+size > snapshotWriteBatchBytes {
				break
			}
			start++
			if seen[key.S] {
				continue
			}
			seen[key.S] = true
			fields = append(fields, key.S, payload)
			members = append(members, 0, key.S)
			bytes += size
		}
		if len(fields) == 0 {
			continue
		}
		// Earlier chunks of one callback may already have captured a repeated
		// key. Preserve the first observed image, including across chunk edges.
		keyNames := make([]string, 0, len(fields)/2)
		for i := 0; i < len(fields); i += 2 {
			keyNames = append(keyNames, fields[i].(string))
		}
		prior, err := p.store.redis(ctx).HMGet(ctx, p.rows, keyNames...).Result()
		if err != nil {
			return err
		}
		freshFields, freshMembers := fields[:0:0], members[:0:0]
		for i, old := range prior {
			if old == nil {
				freshFields = append(freshFields, fields[2*i:2*i+2]...)
				freshMembers = append(freshMembers, members[2*i:2*i+2]...)
			}
		}
		fields, members = freshFields, freshMembers
		if len(fields) == 0 {
			continue
		}
		if err := p.mutate(ctx, "hset", p.rows, fields...); err != nil {
			return err
		}
		if err := p.mutate(ctx, "zadd", p.keys, members...); err != nil {
			return err
		}
	}
	return nil
}

func (p *snapshotImportSpool) apply(ctx context.Context) error {
	remaining, err := p.ready(ctx)
	if err != nil {
		return err
	}
	count, err := p.store.redis(ctx).HLen(ctx, p.rows).Result()
	if err != nil {
		return err
	}
	if count != remaining {
		return errorf(adbc.StatusIO, "import capture remaining count is inconsistent")
	}
	directory, err := resolveSnapshotMetadata(ctx, p.keys[len(metaPrefix):], false)
	if err != nil {
		return err
	}
	if n, err := p.store.client.ZCard(ctx, directory).Result(); err != nil || n != count {
		return errorf(adbc.StatusIO, "import journal directory is incomplete")
	}
	min := "-"
	var applied int64
	physicalRows, err := resolveSnapshotMetadata(ctx, p.rows[len(metaPrefix):], false)
	if err != nil {
		return err
	}
	for {
		keys, err := p.store.client.ZRangeByLex(ctx, directory, &goredis.ZRangeBy{Min: min, Max: "+", Count: snapshotImportPage}).Result()
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			break
		}
		// Inspect lengths before fetching payloads: a page of wide rows must
		// not create an unbounded aggregate HMGET/decoded-row allocation.
		pipe := p.store.client.Pipeline()
		sizes := make([]*goredis.IntCmd, len(keys))
		for i, key := range keys {
			sizes[i] = pipe.HStrLen(ctx, physicalRows, key)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for start := 0; start < len(keys); {
			end, bytes := start, int64(0)
			for end < len(keys) {
				n := sizes[end].Val()
				if n < 1 || n > snapshotMaxBytes {
					return errorf(adbc.StatusIO, "invalid import journal payload size")
				}
				if end > start && bytes+n > snapshotWriteBatchBytes {
					break
				}
				bytes += n
				end++
			}
			if err := p.applyPage(ctx, keys[start:end]); err != nil {
				return err
			}
			start = end
		}
		applied += int64(len(keys))
		min = "(" + keys[len(keys)-1]
	}
	if applied != count {
		return errorf(adbc.StatusIO, "import journal rows were not fully applied")
	}
	return p.store.redis(ctx).Del(ctx, p.rows, p.keys).Err()
}

func (p *snapshotImportSpool) applyPage(ctx context.Context, keys []string) error {
	payloads, err := p.store.redis(ctx).HMGet(ctx, p.rows, keys...).Result()
	if err != nil {
		return err
	}
	rows := make([][]Value, len(keys))
	for i, payload := range payloads {
		text, ok := payload.(string)
		if !ok {
			return errorf(adbc.StatusIO, "import journal row is missing")
		}
		rows[i], err = decodeSnapshotImportRow(p.meta, text)
		if err != nil {
			return err
		}
	}
	capture, err := p.captureOutcome(ctx)
	if err != nil {
		return err
	}
	if capture.Remaining < int64(len(rows)) {
		return errorf(adbc.StatusIO, "invalid import row ID reservation")
	}
	for _, row := range rows {
		for i, col := range p.meta.Columns {
			if row[i].Null && !col.Nullable {
				return notNullErr(col.Name, p.meta.Name)
			}
		}
	}
	if err := p.store.checkWritable(ctx, p.meta); err != nil {
		return err
	}
	allocation := rowAlloc{first: capture.Total - capture.Remaining + 1}
	allocation.sourceKeys = make([]Value, len(keys))
	for i, key := range keys {
		allocation.sourceKeys[i] = stringValue(key)
	}
	physicalRows, err := resolveSnapshotMetadata(ctx, p.rows[len(metaPrefix):], false)
	if err != nil {
		return err
	}
	physicalKeys, err := resolveSnapshotMetadata(ctx, p.keys[len(metaPrefix):], false)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, snapshotImportOutputKey{}, &snapshotImportOutput{rows: physicalRows, keys: physicalKeys})
	if _, err := p.store.writeRows(ctx, p.meta, allocation, rows); err != nil {
		return err
	}
	return nil
}
