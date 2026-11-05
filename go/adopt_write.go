// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"slices"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// insertMeta adds an input-only column for an adopted row's key. It is never
// persisted and never participates in implicit target lists or RETURNING *.
func insertMeta(meta *tableMeta) *tableMeta {
	if !meta.Adopted || !meta.KeyRows {
		return meta
	}
	if _, ok := meta.resolve(keyField); ok {
		return meta
	}
	m := *meta
	m.Columns = append(slices.Clone(meta.Columns), columnMeta{Name: keyField, Type: typeString})
	return &m
}

func (s *store) prepareKeyRows(ctx context.Context, meta *tableMeta, rows [][]Value) error {
	ki, ok := meta.resolve(keyField)
	if !meta.KeyRows || !ok {
		return errorf(adbc.StatusInvalidArgument, "INSERT into adopted table %q requires __key", meta.Name)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row[ki].Null {
			return notNullErr(keyField, meta.Name)
		}
		key := row[ki].S
		if !isRowKey(meta.prefix(), key) {
			return errorf(adbc.StatusInvalidArgument, "__key %q must be a row key under prefix %q (no nested ':')", key, meta.prefix())
		}
		if seen[key] {
			return stateErrorf(stateUnique, "duplicate __key %q", key)
		}
		seen[key] = true
		fields := 0
		for i, c := range meta.Columns {
			if i == ki {
				continue
			}
			if row[i].Null && !c.Nullable {
				return notNullErr(c.Name, meta.Name)
			}
			if !row[i].Null {
				fields++
			}
		}
		if fields == 0 {
			return errorf(adbc.StatusIntegrity, "INSERT would leave HASH %s without fields", key)
		}
	}
	if err := s.checkKeys(ctx, meta, true); err != nil {
		return err
	}
	// Preflight the complete batch; the script also checks atomically at write
	// time, since application writers do not coordinate with this driver.
	for start := 0; start < len(rows); start += pipelineChunk {
		pipe := s.client.Pipeline()
		var cmds []*goredis.IntCmd
		for _, row := range rows[start:min(start+pipelineChunk, len(rows))] {
			cmds = append(cmds, pipe.Exists(ctx, row[ki].S))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "failed to check new row keys")
		}
		for i, c := range cmds {
			if c.Val() != 0 {
				return stateErrorf(stateUnique, "__key %q already exists", rows[start+i][ki].S)
			}
		}
	}
	return nil
}

var keyRowInsertScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
redis.call('HSET', KEYS[1], unpack(ARGV))
return 1
`)

func (s *store) writeKeyRows(ctx context.Context, meta *tableMeta, rows [][]Value) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	if err := s.raiseNeeds(ctx, meta, rowNeeds(meta, rows)); err != nil {
		return 0, err
	}
	ki, _ := meta.resolve(keyField)
	var written int64
	for _, row := range rows {
		if err := s.checkWritten(ctx, meta); err != nil {
			return written, err
		}
		fields := make([]any, 0, 2*(len(meta.Columns)-1))
		for i, c := range meta.Columns {
			if i != ki && !row[i].Null {
				fields = append(fields, c.field(), c.encode(row[i], c.TextFormat))
			}
		}
		n, err := keyRowInsertScript.Run(ctx, s.client, []string{row[ki].S}, fields...).Int()
		if err != nil {
			return written, wrapRedis(err, "failed to insert adopted row")
		}
		if n == 0 {
			return written, stateErrorf(stateUnique, "__key %q already exists", row[ki].S)
		}
		written++
	}
	return written, s.checkKeys(ctx, meta, true)
}

// Check the type at deletion time: a key can change since SCAN TYPE hash.
var truncateKeyRowScript = goredis.NewScript(`
if redis.call('TYPE', KEYS[1]).ok == 'hash' then return redis.call('UNLINK', KEYS[1]) end
return 0
`)

func (s *store) truncateAdoptedRows(ctx context.Context, meta *tableMeta) error {
	if !meta.KeyRows {
		return errAdoptedByRowID(meta)
	}
	ks, err := (&hashConn{client: s.client}).scanKeys(ctx, meta.prefix(), "hash")
	if err != nil {
		return err
	}
	for {
		if err := s.checkKeys(ctx, meta, true); err != nil {
			return err
		}
		keys, done, err := ks.next(ctx)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !isRowKey(meta.prefix(), key) {
				continue
			}
			if err := truncateKeyRowScript.Run(ctx, s.client, []string{key}).Err(); err != nil {
				return wrapRedis(err, "failed to truncate adopted rows")
			}
		}
		if done {
			return nil
		}
	}
}
