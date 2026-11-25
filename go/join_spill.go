// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Partitioned (Grace) spill for a streamed snowflake join whose retained
// dimension rows exceed snowflakeBranchRows (#654). The dimension being read
// when the cap is crossed is written to query-local partition files by a hash
// of its join key instead of being retained; the fact is then written to the
// same partitions by its key for that dimension, and each partition joins on
// its own: its dimension rows are loaded as that dimension's buckets and its
// fact rows run through the unchanged probe and residual. Other dimensions
// stay in memory under the cap. Rows are written with spillRowCodec. Only one
// dimension spills; if another would also exceed the cap, or one partition
// would, the join falls back to the eager join before any fact row is read,
// as it did before.

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
)

// joinSpillPartitions is how many partitions a spilled dimension and its
// fact are split into. Tests lower it.
var joinSpillPartitions = 64

// testNoJoinSpill restores the cap fallback without spilling, for tests.
var testNoJoinSpill bool

// joinSpillStats counts spilled joins and their written rows; tests read it.
// spills counts started spills, joined those that went on to join by
// partition rather than fall back.
var joinSpillStats struct {
	spills, joined, dimRows, factRows atomic.Int64
}

// testHookJoinSpillDir, if set, sees each spilled join's directory.
var testHookJoinSpillDir func(string)

type joinPartWriter struct {
	file *os.File
	w    *bufio.Writer
	rows int64
}

type joinSpill struct {
	spillRowCodec
	exec    *executor
	dir     string
	dim     int // index of the spilled dimension in shape.dims
	parts   []joinPartWriter
	dimRows []int64 // rows written to each dimension partition
}

func (e *executor) newJoinSpill(dim int) (*joinSpill, error) {
	dir, err := os.MkdirTemp("", "redis-adbc-join-*")
	if err != nil {
		return nil, err
	}
	if testHookJoinSpillDir != nil {
		testHookJoinSpillDir(dir)
	}
	s := &joinSpill{spillRowCodec: newSpillRowCodec(), exec: e, dir: dir, dim: dim}
	if err := s.open("dim"); err != nil {
		s.remove()
		return nil, err
	}
	joinSpillStats.spills.Add(1)
	return s, nil
}

func (s *joinSpill) partition(key string) int {
	h := fnv.New64a()
	_, _ = io.WriteString(h, key)
	return int(h.Sum64() % uint64(len(s.parts)))
}

func (s *joinSpill) path(kind string, p int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s-%04d", kind, p))
}

func (s *joinSpill) open(kind string) error {
	n := max(joinSpillPartitions, 1)
	s.parts = make([]joinPartWriter, n)
	for p := range s.parts {
		f, err := os.Create(s.path(kind, p))
		if err != nil {
			return err
		}
		s.parts[p] = joinPartWriter{file: f, w: bufio.NewWriterSize(spillWriter(f, s.exec.aggregateMetrics), 32<<10)}
	}
	return nil
}

// close flushes and closes the open partition files, returning their row
// counts.
func (s *joinSpill) close() ([]int64, error) {
	var first error
	rows := make([]int64, len(s.parts))
	for p := range s.parts {
		part := &s.parts[p]
		if part.file == nil {
			continue
		}
		if err := part.w.Flush(); err != nil && first == nil {
			first = err
		}
		if err := part.file.Close(); err != nil && first == nil {
			first = err
		}
		part.file = nil
		rows[p] = part.rows
	}
	return rows, first
}

// write appends a row and its partition key to the key's partition.
func (s *joinSpill) write(key string, row map[string]Value) error {
	part := &s.parts[s.partition(key)]
	if err := writeSpillString(part.w, key); err != nil {
		return err
	}
	part.rows++
	return s.writeFields(part.w, row)
}

// each reads one partition file's rows in order.
func (s *joinSpill) each(kind string, p int, visit func(key string, row map[string]Value) error) error {
	f, err := os.Open(s.path(kind, p))
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(spillReader(f, s.exec.aggregateMetrics), 64<<10)
	for {
		key, err := readSpillString(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		row, err := s.readFields(r)
		if err != nil {
			return err
		}
		if err := visit(key, row); err != nil {
			return err
		}
	}
}

// loadDim reads a dimension partition as buckets by key.
func (s *joinSpill) loadDim(p int) (map[string][]map[string]Value, error) {
	buckets := map[string][]map[string]Value{}
	err := s.each("dim", p, func(key string, row map[string]Value) error {
		buckets[key] = append(buckets[key], row)
		return nil
	})
	return buckets, err
}

// remove closes any open files and deletes the directory; nil-safe.
func (s *joinSpill) remove() {
	if s == nil {
		return
	}
	_, _ = s.close()
	_ = os.RemoveAll(s.dir)
}

// joinSpilledFacts writes the fact's rows to the spilled dimension's
// partitions by their key for it (a NULL key cannot join and is dropped, as
// the probe drops it), then joins partition by partition: the partition's
// dimension rows become that dimension's buckets, and its fact rows go through
// probe. stopped reports that the consumer failed, which ends the reading.
func (e *executor) joinSpilledFacts(ctx context.Context, shape *starShape, spill *joinSpill, scanItem func(int, func([]map[string]Value) error) error, probe func(int, map[string]Value) error, stopped func() bool) error {
	dim := &shape.dims[spill.dim]
	joinSpillStats.joined.Add(1)
	if err := spill.open("fact"); err != nil {
		return err
	}
	err := scanItem(shape.fact, func(rows []map[string]Value) error {
		for _, r := range rows {
			if err := cancelled(ctx); err != nil {
				return err
			}
			if k, ok := starRowKey(r, dim.factCols); ok {
				if err := spill.write(k, r); err != nil {
					return err
				}
			}
		}
		return nil
	})
	factRows, closeErr := spill.close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	for _, n := range factRows {
		joinSpillStats.factRows.Add(n)
	}
	defer func() { dim.buckets = nil }()
	for p := range factRows {
		if factRows[p] == 0 {
			continue // nothing in this partition can produce a row
		}
		buckets, err := spill.loadDim(p)
		if err != nil {
			return err
		}
		if len(buckets) == 0 {
			continue
		}
		dim.buckets = buckets
		err = spill.each("fact", p, func(_ string, row map[string]Value) error {
			if stopped() {
				return errJoinSpillStopped
			}
			return probe(0, row)
		})
		dim.buckets = nil
		if err == errJoinSpillStopped || stopped() {
			return nil // the consumer's error is reported by the caller
		}
		if err != nil {
			return err
		}
	}
	return nil
}

var errJoinSpillStopped = fmt.Errorf("spilled join stopped after consumer failure")
