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

import "context"

// lateProjection separates output fields from the ordered residual scan.
// LIMIT is counted only after a selected HASH still exists at fetch time.
type lateProjection struct {
	need          map[string]bool
	fields        []string
	offset, limit int64
}

func (it *scanIter) materialize(ctx context.Context, keys []string) ([]string, []map[string]Value, error) {
	skip := min(it.skip, int64(len(keys)))
	it.skip -= skip
	keys = keys[skip:]
	outKeys := make([]string, 0)
	out := make([]map[string]Value, 0)
	for len(keys) > 0 && it.left > 0 {
		n := min(int64(len(keys)), it.left)
		fetched, err := it.store.fetchRows(ctx, keys[:n], it.late.fields)
		if err != nil {
			return nil, nil, err
		}
		for i, raw := range fetched {
			if raw == nil {
				continue
			}
			if err := cancelled(ctx); err != nil {
				return nil, nil, err
			}
			vals, err := decodeRow(it.meta, raw, it.late.need)
			if err != nil {
				return nil, nil, err
			}
			outKeys = append(outKeys, keys[i])
			out = append(out, vals)
			it.left--
		}
		keys = keys[n:]
	}
	if it.left == 0 {
		it.close(ctx)
	}
	return outKeys, out, nil
}
