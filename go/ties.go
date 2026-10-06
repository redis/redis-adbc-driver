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

// Sorting on values the index rounds.
//
// The index sorts (SORTBY) on its sorting vector, which holds a NUMERIC
// attribute as a double. A double holds every integer below 2^53, and every
// decimal of at most 15 significant digits, apart from its neighbours. It
// doesn't hold larger BIGINT values, nor TIME and TIMESTAMP values as large
// (integers of their unit: a nanosecond TIMESTAMP(9) passes 2^53 in April
// 1970, a microsecond one in 2255 and before 1685), nor NUMERIC values with
// more digits: 2^53 and 2^53 + 1 are the same double. The index returns
// rows that tie on its doubles in the order it indexed them (reversed for
// DESC), so up to v0.0.12 ORDER BY returned 9007199254740993 before
// 9007199254740992 when it was inserted first, and LIMIT could pick the
// wrong one. Postgres orders them exactly.
//
// Rounding to a double never swaps two values, so the index's order is
// SQL's except among rows that tie. When the index sorts on a column whose
// values may round (tieBound), the scan sorts those rows again (tieSort):
//
//   - The index returns each row's sort keys (the __sort<i> copies, see
//     aggRequest). Rows that follow each other in its order with the same
//     keys, up to and including the first key that may round, are a run.
//     Rows of different runs are in SQL's order, since the keys before that
//     one don't round. A run may join rows the index tells apart (it prints
//     fractions with 12 significant digits), but never parts rows it
//     doesn't.
//   - A row is wide when a key that may round is at least its column's
//     tieBound in magnitude, as the index returns it (less a margin for
//     those 12 digits). Two rows can only be out of order if both are wide
//     in the first key on which they differ, so a run without a wide row is
//     in order already. A run of two or more rows with a wide one is sorted
//     by the rows' values, read from the HASHes (HMGET) for the columns the
//     query doesn't read anyway. The scan returns a run once the next row
//     starts another, so a stream holds it until then.
//   - With LIMIT and OFFSET in the index, the scan asks for one row more
//     after them, and one before them if there is an OFFSET: they tell
//     whether the first and last runs go on past LIMIT and OFFSET. If one
//     does and has a wide row, LIMIT and OFFSET may have cut it in the wrong
//     place. When the key that rounds is the first, of an integer type
//     (BIGINT, TIME, TIMESTAMP), the scan then reads that run whole, with
//     the index query @c:[v v] for its double v (byValue), and sorts it;
//     for a run that goes on before the rows read, the index also counts
//     the rows that sort before v (FT.SEARCH … LIMIT 0 0). The sorted run's
//     rows take its places. Otherwise the scan asks again for more rows on
//     that side (twice as many, and at least tieWiden more), until the run
//     ends within them. Either way the rows of the limit are read before
//     any is returned, so a LIMIT larger than a stream's batch is read
//     whole.
//
// Without wide rows, then, nothing changes but those two extra rows: the
// scan runs the same commands, and its rows stream as before. Microsecond
// timestamps between 1685 and 2255 are never wide, and nanosecond ones
// (which are) only tie within about 256 ns of each other. Runs of distinct
// values are short: about 1,000 BIGINT values share a double, and 256
// nanosecond timestamps of 2026; many rows with the same wide value make a
// long run, which a LIMIT at its edge reads whole.
//
// Concurrency: rows written while a scan reads a run or more rows may move
// what its LIMIT and OFFSET pick, as between two queries. The HMGET that
// reads a run's values may see a later value than the index sorted on, as
// LOAD * may see one later than the index matched.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	goredis "github.com/redis/go-redis/v9"
)

// tieBound returns the magnitude from which values of an indexed column's
// type may round to the double of another value, or 0 if none can: 2^53
// for integers (the comparison bounds of planWhere are inexact from there
// too), and 10^(15-s) for NUMERIC(p, s) with p above 15, whose smaller
// values have at most 15 significant digits.
func tieBound(t ColType) float64 {
	switch t.Kind {
	case KindInt64, KindTime, KindTimestamp:
		return exactDoubleLimit
	case KindDecimal:
		if t.Precision > 15 {
			return math.Pow10(15 - int(t.Scale))
		}
	}
	return 0
}

// tieStats counts the work of scans with a tieSort; tests read it to tell
// which path a query took.
var tieStats struct {
	sorted   atomic.Int64 // runs sorted by their values
	widened  atomic.Int64 // LIMIT and OFFSET read again with more rows
	runsRead atomic.Int64 // runs past LIMIT and OFFSET read by value
}

// tieMargin is how much less than tieBound a key the index returns may be
// and still count as wide: it prints a fraction with 12 significant digits.
const tieMargin = 1e-9

// tieSort re-sorts the rows of a scan the index sorts on keys that may
// round (see the top of this file).
type tieSort struct {
	cols  []columnMeta // the sort keys' columns
	order []planOrder  // and directions
	// bounds are the cols' tieBounds, and first is the first key with one.
	bounds []float64
	first  int
	// aliases are the keys' sortAliases.
	aliases []string
}

// newTieSort returns the tieSort of a scan the index sorts on cols, in
// order, or nil if no col's values may round.
func newTieSort(cols []columnMeta, order []planOrder) *tieSort {
	t := &tieSort{cols: cols, order: order, first: -1}
	for i, c := range cols {
		b := tieBound(c.Type)
		if b > 0 && t.first < 0 {
			t.first = i
		}
		t.bounds = append(t.bounds, b)
		t.aliases = append(t.aliases, sortAlias(i))
	}
	if t.first < 0 {
		return nil
	}
	return t
}

// runKey returns a row's sort keys as the index returned them, up to the
// first that may round: rows that follow each other with the same runKey
// are a run.
func (t *tieSort) runKey(r aggRow) string {
	if t.first == 0 {
		// A number, never a NUL byte.
		if v, ok := r[t.aliases[0]]; ok {
			return v
		}
		return "\x00"
	}
	var b strings.Builder
	for i := 0; i <= t.first; i++ {
		v, ok := r[t.aliases[i]]
		if !ok {
			b.WriteString("-;") // NULL
			continue
		}
		b.WriteString(strconv.Itoa(len(v)))
		b.WriteByte(':')
		b.WriteString(v)
	}
	return b.String()
}

// wide reports whether a row has a key that may round from its column's
// tieBound on, as the index returned it.
func (t *tieSort) wide(r aggRow) bool {
	for i, b := range t.bounds {
		if b == 0 {
			continue
		}
		v, ok := r[t.aliases[i]]
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.Abs(f) >= b*(1-tieMargin) {
			return true
		}
	}
	return false
}

// tieScan is the state of a scan with a tieSort.
type tieScan struct {
	*tieSort
	store *store
	ar    *aggRequest
	// off and n are the OFFSET and LIMIT, when they run in the index (ar
	// then asks for more rows, see windowLimit).
	off, n int64
	// held is the run read so far, which the next page may go on with, and
	// heldKey its runKey.
	held    []tieRow
	heldKey string
	// ready are the rows to return next, in order, and done is set once
	// they are the last.
	ready []tieRow
	done  bool
}

// tieRow is a row of a scan with a tieSort.
type tieRow struct {
	raw  aggRow // as the index returned it; nil once found deleted
	key  string
	vals map[string]Value // nil if not kept
}

// windowLimit returns the LIMIT a scan with a tieSort asks the index for
// first, to read the rows of LIMIT n OFFSET off (limit): one more after
// them, and one before them if there is an OFFSET.
func (t *tieScan) windowLimit(limit [2]int64) *[2]int64 {
	t.off, t.n = limit[0], limit[1]
	from := max(t.off-1, 0)
	return &[2]int64{from, aggCount(from, min(t.n, maxAggLimit)+1+t.off-from)}
}

// tieWiden is the fewest rows a scan asks for more on a side of LIMIT and
// OFFSET when a run goes on past them (and it can't read the run by
// value): as many nanosecond timestamps of 2026 share a double.
const tieWiden = 256

// maxAggLimit is the largest count FT.AGGREGATE's LIMIT takes, and on a
// cluster its offset and count together (the coordinator asks the shards
// for that many rows).
const maxAggLimit = 1 << 31

// aggCount returns count, cut so that LIMIT from count is within
// maxAggLimit.
func aggCount(from, count int64) int64 {
	return min(count, max(maxAggLimit-from, 1))
}

// pageRows returns the rows of the page keep last saw, in order, with the
// keys and values of those it kept.
func (it *scanIter) pageRows(keys []string, vals []map[string]Value) []tieRow {
	rows := make([]tieRow, len(it.raw))
	k := 0
	for i, raw := range it.raw {
		rows[i].raw = raw
		if k < len(it.kept) && it.kept[k] == i {
			rows[i].key, rows[i].vals = keys[k], vals[k]
			k++
		}
	}
	return rows
}

// readWindow reads the rows of a scan whose LIMIT and OFFSET run in the
// index, sorts their runs and makes them ready. If the first or last run
// has a wide row and goes on to the first or last row read (which aren't
// LIMIT and OFFSET's), it reads again with more rows on that side.
func (it *scanIter) readWindow(ctx context.Context) error {
	t := it.ties
	for {
		var rows []tieRow
		for {
			keys, vals, err := it.nextPage(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			rows = append(rows, it.pageRows(keys, vals)...)
		}
		// rows are from from on: those of LIMIT and OFFSET are rows[lo:hi].
		from, asked := t.ar.limit[0], t.ar.limit[1]
		lo, hi := int(t.off-from), len(rows)
		if int64(hi-lo) > t.n {
			hi = lo + int(t.n)
		}
		if lo >= hi {
			break
		}
		lead := from > 0 && t.reachesEdge(rows, lo, hi, true)
		trail := int64(len(rows)) == asked && t.reachesEdge(rows, lo, hi, false)
		if (lead || trail) && (!lead || t.byValue(t.edgeKey(rows, lo, hi, true))) &&
			(!trail || t.byValue(t.edgeKey(rows, lo, hi, false))) {
			return it.readEdgeRuns(ctx, rows, lo, hi, lead, trail)
		}
		wider := *t.ar.limit
		more := max(asked, tieWiden)
		if lead {
			wider[0] = max(from-more, 0)
		}
		if trail {
			asked += more
		}
		wider[1] = aggCount(wider[0], from+asked-wider[0])
		if wider == *t.ar.limit {
			if err := it.sortRuns(ctx, rows, lo, hi, nil); err != nil {
				return err
			}
			for _, r := range rows[lo:hi] {
				if r.vals != nil {
					t.ready = append(t.ready, r)
				}
			}
			break
		}
		tieStats.widened.Add(1)
		it.cur.close(ctx)
		it.cur = nil
		t.ar.limit = &wider
		cur, err := t.store.openAggregate(ctx, t.ar)
		if err != nil {
			return err
		}
		it.cur = cur
	}
	t.done = true
	return nil
}

// edgeKey returns the runKey of the first row of LIMIT and OFFSET
// (rows[lo:hi]) that wasn't deleted, or of the last, without first.
func (t *tieSort) edgeKey(rows []tieRow, lo, hi int, first bool) string {
	i, in := lo, 1
	if !first {
		i, in = hi-1, -1
	}
	for ; i >= lo && i < hi; i += in {
		if r := rows[i].raw; r != nil {
			return t.runKey(r)
		}
	}
	return ""
}

// byValue reports whether the scan can read the run with runKey key whole
// by its value, with the query @c:[key key]: when the first sort key may
// round, so that it is the run's only key, and the index returned it as an
// integer (it prints integers exactly).
func (t *tieSort) byValue(key string) bool {
	if t.first != 0 || t.cols[0].Type.Kind == KindDecimal {
		return false
	}
	digits := strings.TrimPrefix(key, "-")
	return digits != "" && strings.Trim(digits, "0123456789") == ""
}

// readEdgeRuns makes ready the rows of LIMIT and OFFSET (rows[lo:hi] of
// rows, read from the first row before them) when the first run (lead),
// the last (trail) or both go on past the rows read. The scan reads those
// runs whole by their value (see byValue): the rows of LIMIT and OFFSET
// that are in them are those of the sorted runs in their places. A run that
// goes on before the rows read starts after the rows that sort before its
// value, which the index counts.
func (it *scanIter) readEdgeRuns(ctx context.Context, rows []tieRow, lo, hi int, lead, trail bool) error {
	t := it.ties
	from := t.ar.limit[0]
	type edge struct {
		key   string
		start int64 // the position of the run's first row
		rows  []tieRow
	}
	var edges []edge
	if lead {
		key := t.edgeKey(rows, lo, hi, true)
		start, err := it.countBefore(ctx, key)
		if err != nil {
			return err
		}
		edges = append(edges, edge{key: key, start: start})
	}
	if key := t.edgeKey(rows, lo, hi, false); trail && (!lead || key != edges[0].key) {
		// It starts after the last row of another run, which isn't before
		// the rows read: else it would be the first run too.
		s := hi - 1
		for s > 0 && (rows[s-1].raw == nil || t.runKey(rows[s-1].raw) == key) {
			s--
		}
		edges = append(edges, edge{key: key, start: from + int64(s)})
	}
	inEdge := map[string]bool{}
	for i := range edges {
		run, err := it.readRun(ctx, edges[i].key)
		if err != nil {
			return err
		}
		edges[i].rows = run
		inEdge[edges[i].key] = true
	}
	if err := it.sortRuns(ctx, rows, lo, hi, inEdge); err != nil {
		return err
	}
	for p := t.off; p-t.off < t.n; p++ {
		var r tieRow
		found := false
		for _, e := range edges {
			if p >= e.start && p-e.start < int64(len(e.rows)) {
				r, found = e.rows[p-e.start], true
				break
			}
		}
		if !found {
			i := p - from
			if i >= int64(len(rows)) {
				break
			}
			// A row of an edge run outside the places its rows take (which
			// rows written meanwhile can move) would be there twice.
			if r = rows[i]; r.raw != nil && inEdge[t.runKey(r.raw)] {
				continue
			}
		}
		if r.vals != nil {
			t.ready = append(t.ready, r)
		}
	}
	t.done = true
	return nil
}

// countBefore returns how many rows of the scan sort before the run whose
// runKey (by value) is key: NULLs sort last either way.
func (it *scanIter) countBefore(ctx context.Context, key string) (int64, error) {
	t := it.ties
	term := fmt.Sprintf("@%s:[-inf (%s]", t.cols[0].field(), key)
	if t.order[0].desc {
		term = fmt.Sprintf("@%s:[(%s +inf]", t.cols[0].field(), key)
	}
	return t.store.countMatches(ctx, t.ar.index, andTerm(t.ar.query, term))
}

// readRun reads the rows of the scan in the run whose runKey (by value) is
// key, sorted.
func (it *scanIter) readRun(ctx context.Context, key string) ([]tieRow, error) {
	t := it.ties
	tieStats.runsRead.Add(1)
	// In no particular order: they are sorted here.
	ar := *t.ar
	ar.query = andTerm(ar.query, fmt.Sprintf("@%s:[%s %s]", t.cols[0].field(), key, key))
	ar.sortBy, ar.limit = nil, nil
	cur, err := t.store.openAggregate(ctx, &ar)
	if err != nil {
		return nil, err
	}
	sub := &scanIter{meta: it.meta, need: it.need, env: it.env, cur: cur, ties: t}
	defer sub.close(ctx)
	var run []tieRow
	for {
		keys, vals, err := sub.nextPage(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, r := range sub.pageRows(keys, vals) {
			if r.vals != nil {
				run = append(run, r)
			}
		}
	}
	if len(run) > 1 {
		if err := it.sortRun(ctx, run); err != nil {
			return nil, err
		}
	}
	return run, nil
}

// andTerm returns the index query that matches the rows both query and
// term match.
func andTerm(query, term string) string {
	if query == "*" {
		return term
	}
	return query + " " + term
}

// reachesEdge reports whether the run of rows[lo], the first row of LIMIT
// and OFFSET (of rows[hi-1], the last, without first) has a wide row among
// them and goes on to the first (last) row read. Rows deleted after the
// index matched them (raw nil) may belong to any run.
func (t *tieSort) reachesEdge(rows []tieRow, lo, hi int, first bool) bool {
	i, in := lo, 1
	if !first {
		i, in = hi-1, -1
	}
	key, wide := "", false
	for j := i; j >= lo && j < hi; j += in {
		r := rows[j].raw
		if r == nil {
			continue
		}
		k := t.runKey(r)
		if key == "" {
			key = k
		} else if k != key {
			break
		}
		if t.wide(r) {
			wide = true
			break
		}
	}
	if !wide {
		return false
	}
	for j := i - in; j >= 0 && j < len(rows); j -= in {
		if r := rows[j].raw; r != nil && t.runKey(r) != key {
			return false
		}
	}
	return true
}

// sortRuns sorts the runs of rows with a wide row that have rows in
// rows[lo:hi], each in the places of its kept rows, except those whose
// runKeys are in skip.
func (it *scanIter) sortRuns(ctx context.Context, rows []tieRow, lo, hi int, skip map[string]bool) error {
	t := it.ties
	for s := 0; s < hi; {
		if rows[s].vals == nil {
			s++
			continue
		}
		key := t.runKey(rows[s].raw)
		var at []int
		e := s
		for ; e < len(rows); e++ {
			if r := rows[e]; r.vals != nil {
				if t.runKey(r.raw) != key {
					break
				}
				at = append(at, e)
			}
		}
		if e > lo && len(at) > 1 && !skip[key] {
			run := make([]tieRow, len(at))
			for k, j := range at {
				run[k] = rows[j]
			}
			if t.hasWide(run) {
				if err := it.sortRun(ctx, run); err != nil {
					return err
				}
				for k, j := range at {
					rows[j] = run[k]
				}
			}
		}
		s = e
	}
	return nil
}

// nextInOrder is next for a scan with a tieSort: the rows of its complete
// runs, sorted.
func (it *scanIter) nextInOrder(ctx context.Context) ([]string, []map[string]Value, error) {
	t := it.ties
	for len(t.ready) == 0 && !t.done {
		if err := it.readRuns(ctx); err != nil {
			return nil, nil, err
		}
	}
	if len(t.ready) == 0 {
		return nil, nil, io.EOF
	}
	keys := make([]string, len(t.ready))
	rows := make([]map[string]Value, len(t.ready))
	for i, r := range t.ready {
		keys[i], rows[i] = r.key, r.vals
	}
	t.ready = t.ready[:0]
	return keys, rows, nil
}

// readRuns reads the next page of rows, making its complete runs ready,
// and the last run too after the last page.
func (it *scanIter) readRuns(ctx context.Context) error {
	t := it.ties
	keys, vals, err := it.nextPage(ctx)
	if errors.Is(err, io.EOF) {
		t.done = true
		return it.flushRun(ctx)
	}
	if err != nil {
		return err
	}
	for _, r := range it.pageRows(keys, vals) {
		if r.vals != nil {
			if err := it.addRow(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// addRow adds a kept row to the run it belongs to, making the run before
// it ready if it starts a new one.
func (it *scanIter) addRow(ctx context.Context, r tieRow) error {
	t := it.ties
	k := t.runKey(r.raw)
	if len(t.held) > 0 && k != t.heldKey {
		if err := it.flushRun(ctx); err != nil {
			return err
		}
	}
	t.held, t.heldKey = append(t.held, r), k
	return nil
}

// flushRun makes the held run ready, sorted if it has a wide row.
func (it *scanIter) flushRun(ctx context.Context) error {
	t := it.ties
	run := t.held
	if len(run) > 1 && t.hasWide(run) {
		if err := it.sortRun(ctx, run); err != nil {
			return err
		}
	}
	t.ready = append(t.ready, run...)
	t.held, t.heldKey = run[:0], ""
	return nil
}

// hasWide reports whether a run has a wide row.
func (t *tieSort) hasWide(run []tieRow) bool {
	for _, r := range run {
		if t.wide(r.raw) {
			return true
		}
	}
	return false
}

// sortRun sorts a run by its sort keys' values, read from the HASHes for
// the columns the scan doesn't read.
func (it *scanIter) sortRun(ctx context.Context, run []tieRow) error {
	t := it.ties
	tieStats.sorted.Add(1)
	var fields []string
	only := map[string]bool{}
	for _, c := range t.cols {
		if !it.need[c.Name] && !only[c.Name] {
			only[c.Name] = true
			fields = append(fields, c.field())
		}
	}
	var read []aggRow
	if len(fields) > 0 {
		keys := make([]string, len(run))
		for i, r := range run {
			keys[i] = r.key
		}
		var err error
		if read, err = t.store.readFields(ctx, keys, fields); err != nil {
			return err
		}
	}
	sortKeys := make([][]Value, len(run))
	for i, r := range run {
		var more map[string]Value
		if read != nil {
			var err error
			if more, err = decodeRow(it.meta, read[i], only); err != nil {
				return err
			}
		}
		for _, c := range t.cols {
			v := r.vals[c.Name]
			if only[c.Name] {
				v = more[c.Name]
			}
			sortKeys[i] = append(sortKeys[i], v)
		}
	}
	idx := make([]int, len(run))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return lessKeys(sortKeys[idx[a]], sortKeys[idx[b]], t.order) })
	sorted := make([]tieRow, len(run))
	for i, j := range idx {
		sorted[i] = run[j]
	}
	copy(run, sorted)
	return nil
}

// readFields reads fields of the HASHes at keys; a HASH that is gone has
// none of them.
func (s *store) readFields(ctx context.Context, keys, fields []string) ([]aggRow, error) {
	out := make([]aggRow, len(keys))
	for start := 0; start < len(keys); start += pipelineChunk {
		end := min(start+pipelineChunk, len(keys))
		pipe := s.client.Pipeline()
		cmds := make([]*goredis.SliceCmd, 0, end-start)
		for _, k := range keys[start:end] {
			cmds = append(cmds, pipe.HMGet(ctx, k, fields...))
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, wrapRedis(err, "failed to read rows")
		}
		for i, cmd := range cmds {
			vals, err := cmd.Result()
			if err != nil {
				return nil, wrapRedis(err, "failed to read rows")
			}
			row := make(aggRow, len(fields))
			for j, v := range vals {
				if sv, ok := v.(string); ok {
					row[fields[j]] = sv
				}
			}
			out[start+i] = row
		}
	}
	return out, nil
}
