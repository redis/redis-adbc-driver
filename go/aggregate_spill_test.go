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
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTupleSpillMergeMultiplePasses(t *testing.T) {
	ctx := context.Background()
	spill := tupleSpill{dir: t.TempDir(), prefix: "test"}
	// More than fan-in squared runs exercises both intermediate merge passes.
	for run := 0; run < spillFanIn*spillFanIn+1; run++ {
		for i := 9; i >= 0; i-- {
			row := spillTuple{key: fmt.Sprintf("%02d", i), raw: []string{"a\x00b", "", ""}, null: []bool{false, false, true}, count: 1}
			if err := spill.add(ctx, row); err != nil {
				t.Fatal(err)
			}
		}
		if err := spill.flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var rows []spillTuple
	if err := spill.merge(ctx, func(row spillTuple) error { rows = append(rows, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 {
		t.Fatalf("rows=%d", len(rows))
	}
	for i, row := range rows {
		if row.key != fmt.Sprintf("%02d", i) || row.count != spillFanIn*spillFanIn+1 || !reflect.DeepEqual(row.raw, []string{"a\x00b", "", ""}) || !reflect.DeepEqual(row.null, []bool{false, false, true}) {
			t.Fatalf("row=%+v", row)
		}
	}
	files, err := filepath.Glob(filepath.Join(spill.dir, "*"))
	if err != nil || len(files) > spillFanIn {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
}

func TestTupleSpillPerQueryTuning(t *testing.T) {
	ctx := context.Background()
	spill := tupleSpill{dir: t.TempDir(), prefix: "tuned", bufferBytes: 1 << 20, bufferRows: 2, fanIn: 2}
	for run := 0; run < 5; run++ {
		for _, key := range []string{"b", "a"} {
			if err := spill.add(ctx, spillTuple{key: key, count: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(spill.runs) != 5 {
		t.Fatalf("runs=%d", len(spill.runs))
	}
	var rows []spillTuple
	if err := spill.merge(ctx, func(row spillTuple) error { rows = append(rows, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].key != "a" || rows[0].count != 5 || rows[1].key != "b" || rows[1].count != 5 {
		t.Fatalf("rows=%+v", rows)
	}
	files, err := filepath.Glob(filepath.Join(spill.dir, "*"))
	if err != nil || len(files) > 2 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
}

func TestAggregateSpillShard(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("group-%d", i)
		shard := aggregateSpillShard(key)
		if shard < 0 || shard >= aggregateMergeShards || shard != aggregateSpillShard(key) {
			t.Fatalf("shard %d for %q", shard, key)
		}
		seen[shard] = true
	}
	if len(seen) != aggregateMergeShards {
		t.Fatalf("used %d of %d shards", len(seen), aggregateMergeShards)
	}
}

func TestTupleSpillCancellationAndTruncation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := tupleSpill{dir: t.TempDir(), prefix: "cancel"}
	if err := s.add(ctx, spillTuple{}); err == nil {
		t.Fatal("cancelled add succeeded")
	}
	path := filepath.Join(s.dir, "broken")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	// Even a fully written empty key followed by EOF is a truncated record,
	// rather than a clean end of the stream.
	keyOnly := filepath.Join(s.dir, "key-only")
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], 0)
	if err := os.WriteFile(keyOnly, header[:], 0600); err != nil {
		t.Fatal(err)
	}
	if err := mergeSpillRuns(context.Background(), []string{keyOnly}, func(spillTuple) error { return nil }); err == nil {
		t.Fatal("truncated record accepted")
	}
	if err := mergeSpillRuns(context.Background(), []string{path}, func(spillTuple) error { return nil }); err == nil {
		t.Fatal("truncated run accepted")
	}
}

func TestWeightedIntegerAggregates(t *testing.T) {
	for _, name := range []string{"SUM", "AVG"} {
		a := newAccumulator(&Func{Name: name})
		addWeightedInteger(a, intValue(typeInt64, math.MaxInt64), 3)
		addWeightedInteger(a, intValue(typeInt64, -math.MaxInt64), 3)
		addWeightedInteger(a, intValue(typeInt64, 9007199254740993), 7)
		addWeightedInteger(a, nullValue(typeInt64), 19)
		if a.count != 13 || a.sum.n != 13 || a.sum.sum.String() != "63050394783186951" {
			t.Fatalf("%s count=%d sum=%s", name, a.count, a.sum.sum.String())
		}
	}
}

func TestAggregateGroupLimitClassification(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		{"SEARCH_LIMIT_OVER Aggregate GROUPBY exceeded MAX_AGGREGATE_GROUPS limit of 10 groups", true},
		{"socket timeout", false}, {"NOAUTH Authentication required", false}, {"MAX_AGGREGATE_GROUPS unavailable", false},
	} {
		if got := aggregateGroupLimit(fmt.Errorf("%s", tc.msg)); got != tc.want {
			t.Fatalf("%q=%v", tc.msg, got)
		}
	}
}
