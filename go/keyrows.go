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

// Rows identified by their keys
//
// An adopted table's rows are an application's HASHes (see inplace.go),
// which have whatever keys the application gave them and none of the
// driver's fields. So such a table (tableMeta.KeyRows) identifies its rows
// by their keys, which the system column __key holds, rather than by
// __rowid:
//
//   - Its rows are the HASHes under its key prefix with no ':' after it
//     (isRowKey), which its index's FILTER keeps (rowKeyFilter): the HASHes
//     of another collection under the prefix (user:42:prefs under user:)
//     aren't rows. A HASH the application writes is a row once the index
//     has it.
//   - WHERE __key = '…' reads that HASH (fetchKeyRows). Scans read rows
//     whole (LOAD *), since no field is in every row: a row deleted after
//     the index matched it has none (scanIter.exists).
//   - Without ORDER BY, rows come in the index's order: there is no
//     insertion order.
//   - UPDATE runs keyRowUpdateScript on each HASH, so that it doesn't make
//     a HASH that the application deleted meanwhile again, and doesn't
//     delete a HASH's last field (Redis deletes an empty HASH).

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// isRowKey reports whether a key under a KeyRows table's prefix is one of
// its rows: the key has no ':' after the prefix, so that the HASHes of
// another collection under it (user:42:prefs under user:) aren't. The
// index's FILTER says the same (rowKeyFilter).
func isRowKey(prefix, key string) bool {
	rest, ok := strings.CutPrefix(key, prefix)
	return ok && !strings.Contains(rest, ":")
}

// rowKeyFilter is the FILTER expression of a KeyRows table's index: its
// documents are the keys isRowKey accepts. RediSearch's substr counts bytes,
// as len does.
func rowKeyFilter(prefix string) string {
	return fmt.Sprintf(`contains(substr(@__key, %d, -1), ":") == 0`, len(prefix))
}

// errAdoptedByRowID refuses a table that redis-adbc adopt made before rows
// were identified by their keys (v0.0.11): its index only holds the HASHes
// that adopt wrote __rowid into. DROP TABLE still drops it (store.dropTable
// doesn't come through executor.loadTable), and leaves the HASHes.
func errAdoptedByRowID(meta *tableMeta) error {
	return errorf(adbc.StatusInvalidState, "%q.%q was adopted by redis-adbc v0.0.11, whose adopted tables this driver no longer reads: DROP TABLE %q.%q (its HASHes stay) and run redis-adbc adopt again",
		displaySchema(meta.Schema), meta.Name, displaySchema(meta.Schema), meta.Name)
}

// fetchKeyRows is fetchRows for a table with KeyRows, whose rows have no
// field that each one has: it reads each HASH whole, with pipelined HGETALL.
// A key that doesn't exist, or isn't a HASH, is no row. The rows hold their
// keys under __key, as rows read through the index do.
func (s *store) fetchKeyRows(ctx context.Context, keys []string, _ []string) ([]aggRow, error) {
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); start += pipelineChunk {
		end := min(start+pipelineChunk, len(keys))
		pipe := s.redis(ctx).Pipeline()
		cmds := make([]*goredis.MapStringStringCmd, 0, end-start)
		for _, k := range keys[start:end] {
			cmds = append(cmds, pipe.HGetAll(ctx, k))
		}
		_, _ = pipe.Exec(ctx) // each command's error is checked below
		for i, cmd := range cmds {
			vals, err := cmd.Result()
			if isWrongType(err) || len(vals) == 0 && err == nil {
				continue
			}
			if err != nil {
				return nil, wrapRedis(err, "failed to read rows")
			}
			row := aggRow(vals)
			row["__key"] = keys[start+i]
			out[start+i] = row
		}
	}
	return out, nil
}

// keyRowUpdateScript applies a rowChange to a row HASH of a table with
// KeyRows. ARGV is the number of fields to set, those fields and their
// values, then the fields to delete. A HASH that the application deleted
// since the scan isn't made again: 0. A change that would delete the HASH's
// last field is refused, and changes nothing: -1 (Redis deletes an empty
// HASH, and with it the row). Otherwise it applies the change: 1.
var keyRowUpdateScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local n = tonumber(ARGV[1])
local del = {}
for i = 2 + 2 * n, #ARGV do del[#del + 1] = ARGV[i] end
if n == 0 then
  local left = redis.call('HLEN', KEYS[1])
  for _, f in ipairs(del) do left = left - redis.call('HEXISTS', KEYS[1], f) end
  if left <= 0 then return -1 end
end
if n > 0 then redis.call('HSET', KEYS[1], unpack(ARGV, 2, 1 + 2 * n)) end
if #del > 0 then redis.call('HDEL', KEYS[1], unpack(del)) end
return 1
`)

// keyRowArgs is keyRowUpdateScript's ARGV for a change.
func keyRowArgs(ch rowChange) []any {
	args := make([]any, 0, 1+len(ch.set)+len(ch.del))
	args = append(args, len(ch.set)/2)
	args = append(args, ch.set...)
	for _, f := range ch.del {
		args = append(args, f)
	}
	return args
}

// errEmptiesRow is the error for an UPDATE that would set every field of a
// row HASH of a table with KeyRows to NULL.
func errEmptiesRow(meta *tableMeta, key string) error {
	return errorf(adbc.StatusIntegrity, "UPDATE would leave the HASH %s of %q.%q without fields, and Redis deletes a HASH without fields (and so the row): set a column to a value, or DELETE the row",
		key, displaySchema(meta.Schema), meta.Name)
}

// checkKeyRowsKept refuses, before anything is written, an UPDATE of a
// table with KeyRows that would delete the last fields of a row HASH: one
// that sets columns to NULL and nothing else, on a HASH that has no other
// field (keyRowUpdateScript refuses one that lost its other fields since).
func (s *store) checkKeyRowsKept(ctx context.Context, meta *tableMeta, changes []rowChange) error {
	var only []rowChange
	for _, ch := range changes {
		if len(ch.set) == 0 && len(ch.del) > 0 {
			only = append(only, ch)
		}
	}
	for start := 0; start < len(only); start += pipelineChunk {
		chunk := only[start:min(start+pipelineChunk, len(only))]
		pipe := s.redis(ctx).Pipeline()
		lens := make([]*goredis.IntCmd, len(chunk))
		vals := make([]*goredis.SliceCmd, len(chunk))
		for i, ch := range chunk {
			lens[i] = pipe.HLen(ctx, ch.key)
			vals[i] = pipe.HMGet(ctx, ch.key, ch.del...)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return wrapRedis(err, "failed to read rows")
		}
		for i, ch := range chunk {
			left := lens[i].Val()
			if left == 0 {
				continue // deleted since the scan: the script skips it
			}
			for _, v := range vals[i].Val() {
				if v != nil {
					left--
				}
			}
			if left <= 0 {
				return errEmptiesRow(meta, ch.key)
			}
		}
	}
	return nil
}

// updateKeyRows applies a pipeline of changes to a table with KeyRows, with
// keyRowUpdateScript.
func (s *store) updateKeyRows(ctx context.Context, meta *tableMeta, changes []rowChange) error {
	pipe := s.redis(ctx).Pipeline()
	cmds := make([]*goredis.Cmd, len(changes))
	for i, ch := range changes {
		cmds[i] = keyRowUpdateScript.EvalSha(ctx, pipe, []string{ch.key}, keyRowArgs(ch)...)
	}
	_, _ = pipe.Exec(ctx) // each command's error is checked below
	for i, cmd := range cmds {
		n, err := cmd.Int64()
		if err != nil && strings.HasPrefix(err.Error(), "NOSCRIPT") {
			// A shard that lost its scripts (a failover, say).
			n, err = keyRowUpdateScript.Eval(ctx, s.redis(ctx), []string{changes[i].key}, keyRowArgs(changes[i])...).Int64()
		}
		if err != nil {
			return wrapRedis(err, "failed to update rows")
		}
		if n == -1 {
			return errEmptiesRow(meta, changes[i].key)
		}
	}
	return nil
}
