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
	"sort"
)

// scanBeyondCap reads HASHes when a lowered aggregate cap would truncate an
// otherwise unbounded cursor. Predicate evaluation and ordering stay in the
// driver; SCAN pages and HASH fetch pipelines remain bounded.
func (e *executor) scanBeyondCap(ctx context.Context, req scanRequest, params []Value) ([]string, []map[string]Value, error) {
	var keys []string
	var rows []map[string]Value
	err := e.scanBeyondCapPages(ctx, req, params, func(k []string, r []map[string]Value) error {
		keys, rows = append(keys, k...), append(rows, r...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !req.meta.KeyRows {
		idx := make([]int, len(rows))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return rows[idx[a]][rowIDField].I < rows[idx[b]][rowIDField].I })
		orderedKeys := make([]string, len(keys))
		orderedRows := make([]map[string]Value, len(rows))
		for i, j := range idx {
			orderedKeys[i] = keys[j]
			orderedRows[i] = rows[j]
		}
		keys, rows = orderedKeys, orderedRows
	}
	return keys, rows, nil
}

func (e *executor) scanBeyondCapPages(ctx context.Context, req scanRequest, params []Value, visit func([]string, []map[string]Value) error) error {
	it, fields, _ := e.newScanIter(ctx, req, params)
	ks, err := (&hashConn{client: e.store.client}).scanKeys(ctx, req.meta.prefix(), "hash")
	if err != nil {
		return err
	}
	fetch := e.store.fetchRows
	if req.meta.KeyRows {
		fetch = e.store.fetchKeyRows
	}
	seen := map[string]bool{}
	for {
		page, done, err := ks.next(ctx)
		if err != nil {
			return err
		}
		unique := page[:0]
		for _, key := range page {
			if seen[key] || req.meta.KeyRows && !isRowKey(req.meta.prefix(), key) {
				continue
			}
			seen[key] = true
			unique = append(unique, key)
		}
		fetched, err := fetch(ctx, unique, fields)
		if err != nil {
			return err
		}
		k, r, err := it.keep(unique, fetched)
		if err != nil {
			return err
		}
		if err := visit(k, r); err != nil {
			return err
		}
		if done {
			break
		}
	}
	return nil
}
