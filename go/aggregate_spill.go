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

// Sorted runs keep exact tuple equality without a cardinality-sized map.
// Each worker owns a bounded buffer; merge passes open at most spillFanIn
// files. Query-local files are removed on every exit, including cancellation.
import (
	"bufio"
	"container/heap"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	spillBufferBytes = 8 << 20
	spillBufferRows  = 65536
	spillFanIn       = 32
)

type spillTuple struct {
	key   string
	raw   []string
	null  []bool
	count int64
}

type tupleSpill struct {
	dir, prefix string
	rows        []spillTuple
	bytes       int
	runs        []string
	serial      int
	metrics     *partitionMetrics
	combine     func(*spillTuple, spillTuple) error
}

func (s *tupleSpill) add(ctx context.Context, row spillTuple) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	s.rows = append(s.rows, row)
	s.bytes += 64 + len(row.key) + len(row.raw)*24
	for _, v := range row.raw {
		s.bytes += len(v)
	}
	if s.bytes >= spillBufferBytes || len(s.rows) >= spillBufferRows {
		return s.flush(ctx)
	}
	return nil
}

func (s *tupleSpill) path() string {
	s.serial++
	return filepath.Join(s.dir, fmt.Sprintf("%s-%08d", s.prefix, s.serial))
}

func (s *tupleSpill) flush(ctx context.Context) error {
	if len(s.rows) == 0 {
		return nil
	}
	var sorted func()
	if s.metrics != nil {
		sorted = s.metrics.spillSort.start()
	}
	sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].key < s.rows[j].key })
	if sorted != nil {
		sorted()
	}
	path := s.path()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(spillWriter(f, s.metrics), 64<<10)
	for i := 0; i < len(s.rows); {
		if err = cancelled(ctx); err != nil {
			break
		}
		row := s.rows[i]
		j := i + 1
		for j < len(s.rows) && s.rows[j].key == row.key {
			if err = combineSpillTuple(s.combine, &row, s.rows[j]); err != nil {
				break
			}
			j++
		}
		if err != nil {
			break
		}
		if err = writeSpillTuple(w, row); err != nil {
			break
		}
		i = j
	}
	if err == nil {
		err = w.Flush()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	s.runs = append(s.runs, path)
	clear(s.rows)
	s.rows = s.rows[:0]
	s.bytes = 0
	return nil
}

func writeSpillString(w io.Writer, v string) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(v))); err != nil {
		return err
	}
	_, err := io.WriteString(w, v)
	return err
}
func readSpillString(r io.Reader) (string, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	b := make([]byte, int(n))
	_, err := io.ReadFull(r, b)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return string(b), err
}
func writeSpillTuple(w io.Writer, row spillTuple) error {
	if err := writeSpillString(w, row.key); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, row.count); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(row.raw))); err != nil {
		return err
	}
	for i, v := range row.raw {
		flag := byte(0)
		if row.null[i] {
			flag = 1
		}
		if _, err := w.Write([]byte{flag}); err != nil {
			return err
		}
		if err := writeSpillString(w, v); err != nil {
			return err
		}
	}
	return nil
}
func readSpillTuple(r io.Reader) (spillTuple, error) {
	var row spillTuple
	var err error
	if row.key, err = readSpillString(r); err != nil {
		return row, err
	}
	if err = binary.Read(r, binary.LittleEndian, &row.count); err != nil {
		return row, spillRecordError(err)
	}
	var n uint32
	if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
		return row, spillRecordError(err)
	}
	row.raw, row.null = make([]string, int(n)), make([]bool, int(n))
	for i := range row.raw {
		var flag [1]byte
		if _, err = io.ReadFull(r, flag[:]); err != nil {
			return row, spillRecordError(err)
		}
		row.null[i] = flag[0] != 0
		if row.raw[i], err = readSpillString(r); err != nil {
			return row, spillRecordError(err)
		}
	}
	return row, nil
}

type spillHead struct {
	row    spillTuple
	source int
}
type spillHeap []spillHead

func (h spillHeap) Len() int           { return len(h) }
func (h spillHeap) Less(i, j int) bool { return h[i].row.key < h[j].row.key }
func (h spillHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *spillHeap) Push(x any)        { *h = append(*h, x.(spillHead)) }
func (h *spillHeap) Pop() any {
	n := len(*h) - 1
	x := (*h)[n]
	(*h)[n] = spillHead{}
	*h = (*h)[:n]
	return x
}

func mergeSpillRuns(ctx context.Context, paths []string, visit func(spillTuple) error) error {
	return mergeSpillRunsWith(ctx, paths, visit, nil, nil)
}

func combineSpillTuple(combine func(*spillTuple, spillTuple) error, dst *spillTuple, src spillTuple) error {
	if combine != nil {
		return combine(dst, src)
	}
	dst.count += src.count
	return nil
}

func mergeSpillRunsWith(ctx context.Context, paths []string, visit func(spillTuple) error, combine func(*spillTuple, spillTuple) error, metrics *partitionMetrics) error {
	files := make([]*os.File, len(paths))
	defer func() {
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	}()
	readers := make([]*bufio.Reader, len(paths))
	heads := spillHeap{}
	for i, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		files[i], readers[i] = f, bufio.NewReaderSize(spillReader(f, metrics), 64<<10)
		row, err := readSpillTuple(readers[i])
		if err == io.EOF {
			continue
		}
		if err != nil {
			return err
		}
		heap.Push(&heads, spillHead{row, i})
	}
	var pending spillTuple
	have := false
	for len(heads) > 0 {
		if err := cancelled(ctx); err != nil {
			return err
		}
		head := heap.Pop(&heads).(spillHead)
		if have && pending.key != head.row.key {
			if err := visit(pending); err != nil {
				return err
			}
			have = false
		}
		if !have {
			pending, have = head.row, true
		} else {
			if err := combineSpillTuple(combine, &pending, head.row); err != nil {
				return err
			}
		}
		row, err := readSpillTuple(readers[head.source])
		if err != io.EOF {
			if err != nil {
				return err
			}
			heap.Push(&heads, spillHead{row, head.source})
		}
	}
	if have {
		return visit(pending)
	}
	return nil
}

func (s *tupleSpill) merge(ctx context.Context, visit func(spillTuple) error) error {
	if err := s.flush(ctx); err != nil {
		return err
	}
	for len(s.runs) > spillFanIn {
		var next []string
		for start := 0; start < len(s.runs); start += spillFanIn {
			paths := s.runs[start:min(start+spillFanIn, len(s.runs))]
			path := s.path()
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			w := bufio.NewWriterSize(spillWriter(f, s.metrics), 64<<10)
			err = mergeSpillRunsWith(ctx, paths, func(row spillTuple) error { return writeSpillTuple(w, row) }, s.combine, s.metrics)
			if err == nil {
				err = w.Flush()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			next = append(next, path)
			for _, old := range paths {
				if err = os.Remove(old); err != nil {
					return err
				}
			}
		}
		s.runs = next
	}
	return mergeSpillRunsWith(ctx, s.runs, visit, s.combine, s.metrics)
}

type measuredSpill struct {
	file    *os.File
	metrics *partitionMetrics
}

func spillReader(f *os.File, m *partitionMetrics) io.Reader {
	if m == nil {
		return f
	}
	return measuredSpill{f, m}
}
func spillWriter(f *os.File, m *partitionMetrics) io.Writer {
	if m == nil {
		return f
	}
	return measuredSpill{f, m}
}
func (s measuredSpill) Read(b []byte) (int, error) {
	defer s.metrics.spillRead.start()()
	n, err := s.file.Read(b)
	s.metrics.spillReadBytes.Add(int64(n))
	return n, err
}
func (s measuredSpill) Write(b []byte) (int, error) {
	defer s.metrics.spillWrite.start()()
	n, err := s.file.Write(b)
	s.metrics.spillBytes.Add(int64(n))
	return n, err
}

func spillRecordError(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
