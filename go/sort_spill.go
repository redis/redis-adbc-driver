// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// External merge sort for a streamed single-table SELECT … ORDER BY whose
// input exceeds a memory budget (#654). Rows are buffered with their sort keys;
// beyond the budget the buffer is sorted and written to a query-local run file,
// and the result streams from a k-way merge of the runs. Below the budget the
// buffered rows finish in memory exactly as selectScanned does. Run files hold
// each row's fields in their storage encoding with a type index into an
// in-memory table, and sort keys are recomputed from the decoded row, so only
// row-local, non-volatile ORDER BY and select-list expressions qualify. Ties
// keep scan order (a sequence number), as the in-memory stable sort does.

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
)

// sortSpillBytes is the estimated retained size of buffered input rows above
// which a sort spills (sortRowBytes). Each run is at most this large.
const sortSpillBytes = 256 << 20

// testSortSpillBytes overrides sortSpillBytes in tests (0: the default).
var testSortSpillBytes int64

// sortSpillStats counts spilling sorts' work; tests read it. maxBuffered is
// the largest estimated buffer written as one run.
var sortSpillStats struct {
	runs, mergedRuns, maxBuffered atomic.Int64
}

// testHookSortSpillDir, if set, sees each spilling sort's run directory.
var testHookSortSpillDir func(string)

func sortSpillBudget() int64 {
	if testSortSpillBytes > 0 {
		return testSortSpillBytes
	}
	return sortSpillBytes
}

// sortSpillable reports whether a streamed SELECT's global sort may spill: a
// plain index or directory scan of one table, a sort outside the index, no
// late materialization, windows or set-returning functions, and ORDER BY and
// select-list expressions that read only the row (no subquery) and are not
// volatile, so that keys can be recomputed and items evaluated as the merged
// rows stream, after the scan.
func (p *selectPlan) sortSpillable(sc selectScan, so *streamOpts) bool {
	m := p.meta
	if so == nil || so.distinct || so.required || len(p.order) == 0 || sc.indexSort || sc.req.late != nil || sc.req.where.none ||
		sc.req.hashScan || sc.req.where.keys != nil || m == nil || m.isMem || m.Live || len(m.PendingCleanup) > 0 ||
		p.windowed() || p.hasSetReturning() {
		return false
	}
	rowLocal := true
	visit := func(x Expr) {
		walkExprPruned(x, func(x Expr) bool {
			switch f := x.(type) {
			case *Subquery, *WindowFunc:
				rowLocal = false
			case *Func:
				if volatileFuncs[f.Name] || aggregateFuncs[f.Name] {
					rowLocal = false
				}
			}
			return rowLocal
		})
	}
	for _, o := range p.order {
		visit(o.expr)
	}
	for _, it := range p.items {
		visit(it.expr)
	}
	return rowLocal
}

// sortRowBytes estimates the memory a buffered row and its keys retain: its
// map (one 8-slot group of 168-byte key/Value slots per eight fields), string
// and decimal payloads, and the key slice.
func sortRowBytes(row map[string]Value, keys []Value) int64 {
	groups := (len(row) + smallRowEntries - 1) / smallRowEntries
	n := int64(64 + groups*smallRowEntries*168 + len(keys)*160)
	for _, v := range row {
		n += int64(len(v.S))
		if v.D != nil {
			n += 48
		}
	}
	for _, v := range keys {
		n += int64(len(v.S))
	}
	return n
}

type sortRecord struct {
	seq  int64
	keys []Value
	row  map[string]Value
}

// sortSpill holds a spilling sort's runs and the field-name and type tables
// its run records index.
type sortSpill struct {
	exec    *executor
	order   []planOrder
	dir     string
	runs    []string
	serial  int
	names   []string
	nameIdx map[string]uint16
	types   []ColType
	typeIdx map[ColType]uint16
	fanIn   int
}

func (s *sortSpill) less(a, b *sortRecord) bool {
	if lessKeys(a.keys, b.keys, s.order) {
		return true
	}
	if lessKeys(b.keys, a.keys, s.order) {
		return false
	}
	return a.seq < b.seq
}

func (s *sortSpill) maxFanIn() int {
	if s.fanIn > 1 {
		return s.fanIn
	}
	return spillFanIn
}

func (s *sortSpill) path() string {
	s.serial++
	return filepath.Join(s.dir, fmt.Sprintf("run-%08d", s.serial))
}

func (s *sortSpill) intern(name string, t ColType) (uint16, uint16, error) {
	n, ok := s.nameIdx[name]
	if !ok {
		if len(s.names) >= 1<<16 {
			return 0, 0, errorf(adbc.StatusInternal, "sort spill field table is full")
		}
		n = uint16(len(s.names))
		s.names = append(s.names, name)
		s.nameIdx[name] = n
	}
	i, ok := s.typeIdx[t]
	if !ok {
		if len(s.types) >= 1<<16 {
			return 0, 0, errorf(adbc.StatusInternal, "sort spill type table is full")
		}
		i = uint16(len(s.types))
		s.types = append(s.types, t)
		s.typeIdx[t] = i
	}
	return n, i, nil
}

// writeRun sorts records and writes them as one run.
func (s *sortSpill) writeRun(ctx context.Context, recs []sortRecord) error {
	if err := sortSliceUnstable(ctx, recs, func(i, j int) bool { return s.less(&recs[i], &recs[j]) }); err != nil {
		return err
	}
	sortSpillStats.runs.Add(1)
	return s.writeFile(ctx, func(write func(*sortRecord) error) error {
		for i := range recs {
			if err := cancelled(ctx); err != nil {
				return err
			}
			if err := write(&recs[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *sortSpill) writeFile(ctx context.Context, produce func(func(*sortRecord) error) error) error {
	path := s.path()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	s.runs = append(s.runs, path) // removed with the directory even if writing fails
	w := bufio.NewWriterSize(spillWriter(f, s.exec.aggregateMetrics), 64<<10)
	err = produce(func(rec *sortRecord) error { return s.writeRecord(w, rec) })
	if err == nil {
		err = w.Flush()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (s *sortSpill) writeRecord(w io.Writer, rec *sortRecord) error {
	if err := binary.Write(w, binary.LittleEndian, rec.seq); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(rec.row))); err != nil {
		return err
	}
	for name, v := range rec.row {
		n, t, err := s.intern(name, v.T)
		if err != nil {
			return err
		}
		flag := byte(0)
		text := ""
		if v.Null {
			flag = 1
		} else {
			text = encodeStored(v)
		}
		if err := binary.Write(w, binary.LittleEndian, [2]uint16{n, t}); err != nil {
			return err
		}
		if _, err := w.Write([]byte{flag}); err != nil {
			return err
		}
		if err := writeSpillString(w, text); err != nil {
			return err
		}
	}
	return nil
}

// readRecord decodes the next record of a run and recomputes its keys with
// env (io.EOF at the run's end).
func (s *sortSpill) readRecord(r io.Reader, env *evalEnv) (sortRecord, error) {
	var rec sortRecord
	if err := binary.Read(r, binary.LittleEndian, &rec.seq); err != nil {
		return rec, err
	}
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return rec, spillRecordError(err)
	}
	rec.row = make(map[string]Value, n)
	for range n {
		var idx [2]uint16
		if err := binary.Read(r, binary.LittleEndian, &idx); err != nil {
			return rec, spillRecordError(err)
		}
		var flag [1]byte
		if _, err := io.ReadFull(r, flag[:]); err != nil {
			return rec, spillRecordError(err)
		}
		text, err := readSpillString(r)
		if err != nil {
			return rec, spillRecordError(err)
		}
		if int(idx[0]) >= len(s.names) || int(idx[1]) >= len(s.types) {
			return rec, errorf(adbc.StatusInternal, "invalid sort spill record")
		}
		t := s.types[idx[1]]
		v := nullValue(t)
		if flag[0] == 0 {
			if v, err = decodeStored(text, t); err != nil {
				return rec, errorf(adbc.StatusInternal, "sort spill field %q: %v", s.names[idx[0]], err)
			}
		}
		rec.row[s.names[idx[0]]] = v
	}
	keys, err := sortKeys(env, s.order, rec.row)
	rec.keys = keys
	return rec, err
}

func sortKeys(env *evalEnv, order []planOrder, row map[string]Value) ([]Value, error) {
	env.row = row
	keys := make([]Value, len(order))
	for i, o := range order {
		k, err := env.eval(o.expr)
		if err != nil {
			return nil, invalidArg(err)
		}
		keys[i] = k
	}
	return keys, nil
}

// sortSource yields one sorted sequence of records: a run file or the final
// in-memory buffer.
type sortSource struct {
	file *os.File
	r    *bufio.Reader
	mem  []sortRecord
	at   int
}

func (s *sortSpill) openSource(path string) (*sortSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &sortSource{file: f, r: bufio.NewReaderSize(spillReader(f, s.exec.aggregateMetrics), 64<<10)}, nil
}

func (s *sortSpill) next(src *sortSource, env *evalEnv) (sortRecord, error) {
	if src.file == nil {
		if src.at == len(src.mem) {
			return sortRecord{}, io.EOF
		}
		rec := src.mem[src.at]
		src.mem[src.at] = sortRecord{}
		src.at++
		return rec, nil
	}
	return s.readRecord(src.r, env)
}

type sortHead struct {
	rec    sortRecord
	source int
}

type sortMerge struct {
	spill   *sortSpill
	env     *evalEnv
	sources []*sortSource
	heads   []sortHead
}

func (m *sortMerge) Len() int           { return len(m.heads) }
func (m *sortMerge) Less(i, j int) bool { return m.spill.less(&m.heads[i].rec, &m.heads[j].rec) }
func (m *sortMerge) Swap(i, j int)      { m.heads[i], m.heads[j] = m.heads[j], m.heads[i] }
func (m *sortMerge) Push(x any)         { m.heads = append(m.heads, x.(sortHead)) }
func (m *sortMerge) Pop() any {
	n := len(m.heads) - 1
	x := m.heads[n]
	m.heads[n] = sortHead{}
	m.heads = m.heads[:n]
	return x
}

func (s *sortSpill) newMerge(sources []*sortSource, env *evalEnv) (*sortMerge, error) {
	m := &sortMerge{spill: s, env: env, sources: sources}
	for i, src := range sources {
		rec, err := s.next(src, env)
		if err == io.EOF {
			continue
		}
		if err != nil {
			m.close()
			return nil, err
		}
		m.heads = append(m.heads, sortHead{rec, i})
	}
	heap.Init(m)
	return m, nil
}

func (m *sortMerge) pop(ctx context.Context) (sortRecord, error) {
	if err := cancelled(ctx); err != nil {
		return sortRecord{}, err
	}
	if len(m.heads) == 0 {
		return sortRecord{}, io.EOF
	}
	head := m.heads[0]
	rec, err := m.spill.next(m.sources[head.source], m.env)
	switch {
	case err == io.EOF:
		heap.Pop(m)
	case err != nil:
		return sortRecord{}, err
	default:
		m.heads[0].rec = rec
		heap.Fix(m, 0)
	}
	return head.rec, nil
}

func (m *sortMerge) close() {
	for _, src := range m.sources {
		if src != nil && src.file != nil {
			_ = src.file.Close()
			src.file = nil
		}
	}
	m.sources, m.heads = nil, nil
}

// reduce merges runs fanIn at a time until the runs and the final buffer
// fit one merge.
func (s *sortSpill) reduce(ctx context.Context, env *evalEnv) error {
	fanIn := s.maxFanIn()
	for len(s.runs)+1 > fanIn {
		var next []string
		for start := 0; start < len(s.runs); start += fanIn {
			group := s.runs[start:min(start+fanIn, len(s.runs))]
			if len(group) == 1 {
				next = append(next, group[0])
				continue
			}
			sources := make([]*sortSource, 0, len(group))
			for _, path := range group {
				src, err := s.openSource(path)
				if err != nil {
					(&sortMerge{sources: sources}).close()
					return err
				}
				sources = append(sources, src)
			}
			m, err := s.newMerge(sources, env)
			if err != nil {
				return err
			}
			before := len(s.runs)
			sortSpillStats.mergedRuns.Add(1)
			err = s.writeFile(ctx, func(write func(*sortRecord) error) error {
				for {
					rec, err := m.pop(ctx)
					if err == io.EOF {
						return nil
					}
					if err != nil {
						return err
					}
					if err := write(&rec); err != nil {
						return err
					}
				}
			})
			m.close()
			if err != nil {
				return err
			}
			next = append(next, s.runs[before])
			s.runs = s.runs[:before]
			for _, old := range group {
				if err := os.Remove(old); err != nil {
					return err
				}
			}
		}
		s.runs = next
	}
	return nil
}

// sortSpillRows streams a spilled sort's merged rows as select-list values.
// It owns the run directory, which close removes.
// OFFSET and LIMIT apply to the merged order; rows are fitted to the result
// columns as a scan stream fits them (streamReader.fill).
type sortSpillRows struct {
	spill      *sortSpill
	merge      *sortMerge
	env        *evalEnv
	items      []planItem
	cols       []resultColumn
	zone       tzZone
	coerce     bool  // the stream accounts batch bytes on coerced values
	skip, left int64 // OFFSET still to skip; LIMIT still to return (-1: none)
	done       bool
}

func (r *sortSpillRows) next(ctx context.Context) ([]Value, error) {
	for !r.done && r.left != 0 {
		rec, err := r.merge.pop(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if r.skip > 0 {
			r.skip--
			continue
		}
		r.env.row = rec.row
		row, err := evalItems(r.env, r.items)
		if err != nil {
			return nil, err
		}
		if err := fitRow(row, r.cols, r.zone); err != nil {
			return nil, err
		}
		if r.coerce {
			for j, c := range r.cols {
				if row[j], err = Coerce(row[j], c.Type); err != nil {
					return nil, invalidArg(err)
				}
			}
		}
		if r.left > 0 {
			r.left--
		}
		return row, nil
	}
	r.close()
	return nil, io.EOF
}

func (r *sortSpillRows) close() {
	if r.done {
		return
	}
	r.done = true
	if r.merge != nil {
		r.merge.close()
	}
	if r.spill != nil && r.spill.dir != "" {
		_ = os.RemoveAll(r.spill.dir)
	}
}

// sortSpillSelect runs a sortSpillable streamed SELECT … ORDER BY. Below the
// budget it returns the materialized rows of selectRows. Above it, the result
// is a stream of the merged runs, read after the scan has finished. handled is
// false when the plan is not eligible.
func (e *executor) sortSpillSelect(ctx context.Context, plan *selectPlan, sc selectScan, params []Value, so *streamOpts) (rows [][]Value, stream *streamReader, handled bool, err error) {
	// Retained partition captures account materialized rows (retainedAllocate)
	// and keep the established materialized path.
	if !plan.sortSpillable(sc, so) || ctx.Value(retainedAllocationKey{}) != nil {
		return nil, nil, false, nil
	}
	budget := sortSpillBudget()
	it, err := e.openScan(ctx, sc.req, params)
	if err != nil {
		return nil, nil, true, err
	}
	defer it.close(ctx)
	env := e.newEnv(ctx, plan.meta.types(), params)
	spill := &sortSpill{exec: e, order: plan.order, nameIdx: map[string]uint16{}, typeIdx: map[ColType]uint16{}}
	defer func() {
		if stream == nil && spill.dir != "" {
			_ = os.RemoveAll(spill.dir)
		}
	}()
	var buf []sortRecord
	var bytes, seq int64
	for {
		_, page, err := it.next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, true, err
		}
		for _, row := range page {
			if err := cancelled(ctx); err != nil {
				return nil, nil, true, err
			}
			keys, err := sortKeys(env, plan.order, row)
			if err != nil {
				return nil, nil, true, err
			}
			buf = append(buf, sortRecord{seq: seq, keys: keys, row: row})
			seq++
			if bytes += sortRowBytes(row, keys); bytes < budget {
				continue
			}
			if spill.dir == "" {
				if spill.dir, err = os.MkdirTemp("", "redis-adbc-sort-*"); err != nil {
					return nil, nil, true, err
				}
				if testHookSortSpillDir != nil {
					testHookSortSpillDir(spill.dir)
				}
				e.diagnostics.setPhase("external_sort")
			}
			for prev := sortSpillStats.maxBuffered.Load(); bytes > prev && !sortSpillStats.maxBuffered.CompareAndSwap(prev, bytes); {
				prev = sortSpillStats.maxBuffered.Load()
			}
			if err := spill.writeRun(ctx, buf); err != nil {
				return nil, nil, true, err
			}
			clear(buf)
			buf, bytes = buf[:0], 0
		}
	}
	if spill.dir == "" {
		// Everything fit: finish exactly as the materialized path does.
		mem := make([]map[string]Value, len(buf))
		keys := make([][]Value, len(buf))
		for i := range buf {
			mem[i], keys[i] = buf[i].row, buf[i].keys
		}
		rows, err := e.selectRows(ctx, plan, sc, mem, keys, params)
		return rows, nil, true, err
	}
	if err := sortSliceUnstable(ctx, buf, func(i, j int) bool { return spill.less(&buf[i], &buf[j]) }); err != nil {
		return nil, nil, true, err
	}
	if err := e.store.checkReads(ctx, e.cache.loaded); err != nil {
		return nil, nil, true, err
	}
	cols := so.cols
	if cols == nil {
		cols = plan.columns()
	}
	sctx, cancel := context.WithCancelCause(ctx)
	mergeEnv := e.newEnv(sctx, plan.meta.types(), params)
	if err := spill.reduce(sctx, mergeEnv); err != nil {
		cancel(nil)
		return nil, nil, true, err
	}
	sources := make([]*sortSource, 0, len(spill.runs)+1)
	for _, path := range spill.runs {
		src, err := spill.openSource(path)
		if err != nil {
			(&sortMerge{sources: sources}).close()
			cancel(nil)
			return nil, nil, true, err
		}
		sources = append(sources, src)
	}
	sources = append(sources, &sortSource{mem: buf})
	merge, err := spill.newMerge(sources, mergeEnv)
	if err != nil {
		cancel(nil)
		return nil, nil, true, err
	}
	source := &sortSpillRows{spill: spill, merge: merge, env: e.newEnv(sctx, plan.meta.types(), params), items: plan.items,
		cols: cols, zone: e.zone(), coerce: so.batchBytes > 0, left: -1}
	if off := plan.sel.Offset; off != nil {
		source.skip = *off
	}
	if lim := plan.sel.Limit; lim != nil {
		source.left = *lim
	}
	r := &streamReader{ctx: sctx, cancel: cancel, exec: e, cache: e.cache, union: source,
		cols: cols, schema: resultSchema(cols), mem: so.mem, batchRows: so.batchRows, batchBytes: so.batchBytes, left: -1}
	r.refs.Store(1)
	r.attachOwners(so.owners)
	stream = r
	return nil, r, true, nil
}
