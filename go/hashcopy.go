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
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// HashCopyOptions selects the HASHes that CopyHashes copies, their
// columns (as for InspectHashes), and the table they are copied into.
type HashCopyOptions struct {
	Prefix    string
	Sample    int
	KeyColumn string
	Types     map[string]string
	Renames   map[string]string
	// NullOnError copies a value that doesn't convert to its column's type
	// as NULL. Otherwise CopyHashes fails on it (see Mode).
	NullOnError bool
	// Schema and Table name the table (by default the database's schema,
	// and a name made from the prefix).
	Schema, Table string
	// Mode is the bulk ingest mode: adbc.OptionValueIngestModeCreate (the
	// default), Append, Replace or CreateAppend. A copy that fails in create
	// mode drops the table it created; in the others, the rows copied
	// before the failure stay.
	Mode string
	// IndexColumns are the columns to index (nil: every one that can be).
	IndexColumns []string
}

// HashCopy is what CopyHashes copied.
type HashCopy struct {
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
	// UnknownFields counts, for each field that isn't a column (the sample
	// didn't have it), the HASHes that have it.
	UnknownFields map[string]int64 `json:"unknown_fields,omitempty"`
	// Nulls counts, for each column, the values copied as NULL because they
	// didn't convert (NullOnError).
	Nulls map[string]int64 `json:"nulls,omitempty"`
}

// hashScan reads the HASHes under a prefix as Arrow record batches, one
// row per HASH, for a bulk ingest. It is an array.RecordReader; releasing
// it closes its connection.
type hashScan struct {
	refs     atomic.Int64
	ctx      context.Context
	conn     *hashConn
	o        HashCopyOptions
	cols     []*guessedColumn
	rcols    []resultColumn
	colOf    map[string]int // field → column
	schema   *arrow.Schema
	keys     *keyScan
	seen     map[string]struct{}
	pending  []string
	scanned  bool
	batchMax int
	sampled  int // HASHes the columns were guessed from
	cur      arrow.RecordBatch
	err      error
	stats    HashCopy
}

// hashBatchRows is the most rows in a batch; hashBatchValues caps the
// values per batch, so that batches of wide HASHes stay a bounded size.
const (
	hashBatchRows   = 10000
	hashBatchValues = 200_000
)

// CopyHashes copies every HASH under o.Prefix into a driver table, one row
// per HASH, with a bulk ingest through the driver. The columns are guessed
// from a sample, as InspectHashes guesses them, and every column is
// nullable. The options are the driver's database options. The HASHes are
// left as they are.
//
// SCAN may return a key more than once, so the copy remembers every key it
// has read: about the size of the keys in memory.
func CopyHashes(ctx context.Context, options map[string]string, o HashCopyOptions) (*HashCopy, error) {
	s, err := scanHashes(ctx, options, o)
	if err != nil {
		return nil, err
	}
	defer s.Release()
	if o.Schema == "" {
		o.Schema = s.conn.db.schema
	}
	if o.Table == "" {
		o.Table = TableNameForPrefix(o.Prefix)
	}
	if o.Mode == "" {
		o.Mode = adbc.OptionValueIngestModeCreate
	}
	s.stats.Table = displaySchema(o.Schema) + "." + o.Table
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, options)
	if err != nil {
		return nil, err
	}
	defer db.Close(ctx)
	conn, err := db.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	st, err := conn.NewStatement(ctx)
	if err != nil {
		return nil, err
	}
	defer st.Close(ctx)
	opts := [][2]string{
		{adbc.OptionKeyIngestTargetTable, o.Table},
		{adbc.OptionKeyIngestMode, o.Mode},
		{adbc.OptionValueIngestTargetDBSchema, o.Schema},
	}
	if o.IndexColumns != nil {
		opts = append(opts, [2]string{OptionStringIngestIndexColumns, strings.Join(o.IndexColumns, ",")})
	}
	for _, kv := range opts {
		if err := st.SetOption(ctx, kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	// The statement releases the reader it is given.
	s.Retain()
	if err := st.BindStream(ctx, s); err != nil {
		return nil, err
	}
	if _, err := st.ExecuteUpdate(ctx); err != nil {
		var ae adbc.Error
		if o.Mode == adbc.OptionValueIngestModeCreate && !(asAdbc(err, &ae) && ae.Code == adbc.StatusAlreadyExists) {
			// The table is this copy's: the ingest created it.
			_ = s.conn.st.dropTable(context.WithoutCancel(ctx), o.Schema, o.Table, true)
		}
		return nil, err
	}
	return &s.stats, nil
}

// scanHashes starts reading the HASHes under o.Prefix (see CopyHashes).
func scanHashes(ctx context.Context, options map[string]string, o HashCopyOptions) (*hashScan, error) {
	if o.Prefix == "" {
		return nil, errorf(adbc.StatusInvalidArgument, "a key prefix is required")
	}
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	s := &hashScan{ctx: ctx, conn: c, o: o, seen: map[string]struct{}{}}
	s.refs.Store(1)
	if err := s.guess(); err != nil {
		c.close(ctx)
		return nil, err
	}
	if s.keys, err = c.scanKeys(ctx, o.Prefix, "hash"); err != nil {
		c.close(ctx)
		return nil, err
	}
	return s, nil
}

// guess reads the sample and guesses the columns.
func (s *hashScan) guess() error {
	sample := s.o.Sample
	if sample == 0 {
		sample = defaultSample
	}
	ks, err := s.conn.scanKeys(s.ctx, s.o.Prefix, "hash")
	if err != nil {
		return err
	}
	var keys []string
	seen := map[string]bool{}
	for sample < 0 || len(keys) < sample {
		page, done, err := ks.next(s.ctx)
		if err != nil {
			return err
		}
		for _, k := range page {
			if !seen[k] && (sample < 0 || len(keys) < sample) {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		if done {
			break
		}
	}
	rows, err := s.conn.readHashes(s.ctx, keys)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errorf(adbc.StatusNotFound, "no HASH keys under %q", s.o.Prefix)
	}
	s.sampled = len(rows)
	s.cols, _, err = guessTable(rows, guessOptions{keyColumn: s.o.KeyColumn, types: s.o.Types, renames: s.o.Renames})
	if err != nil {
		return err
	}
	s.colOf = map[string]int{}
	for i, col := range s.cols {
		s.rcols = append(s.rcols, resultColumn{Name: col.name, Type: col.t})
		if col.field != "" {
			s.colOf[col.field] = i
		}
	}
	s.schema = resultSchema(s.rcols)
	s.batchMax = max(1, min(hashBatchRows, hashBatchValues/len(s.cols)))
	return nil
}

func (s *hashScan) Retain() { s.refs.Add(1) }

func (s *hashScan) Release() {
	if s.refs.Add(-1) == 0 {
		if s.cur != nil {
			s.cur.Release()
			s.cur = nil
		}
		s.conn.close(s.ctx)
	}
}

func (s *hashScan) Schema() *arrow.Schema { return s.schema }

func (s *hashScan) RecordBatch() arrow.RecordBatch { return s.cur }

// Deprecated: Use [hashScan.RecordBatch] instead.
func (s *hashScan) Record() arrow.RecordBatch { return s.cur }

func (s *hashScan) Err() error { return s.err }

func (s *hashScan) Next() bool {
	if s.cur != nil {
		s.cur.Release()
		s.cur = nil
	}
	if s.err != nil {
		return false
	}
	for {
		for len(s.pending) < s.batchMax && !s.scanned {
			page, done, err := s.keys.next(s.ctx)
			if err != nil {
				s.err = err
				return false
			}
			for _, k := range page {
				if _, dup := s.seen[k]; !dup {
					s.seen[k] = struct{}{}
					s.pending = append(s.pending, k)
				}
			}
			s.scanned = done
		}
		if len(s.pending) == 0 {
			return false
		}
		n := min(len(s.pending), s.batchMax)
		batch := s.pending[:n]
		s.pending = s.pending[n:]
		rows, err := s.conn.readHashes(s.ctx, batch)
		if err != nil {
			s.err = err
			return false
		}
		if len(rows) == 0 {
			continue // deleted since SCAN listed them
		}
		values, err := s.convert(rows)
		if err != nil {
			s.err = err
			return false
		}
		if s.cur, err = buildRecord(memory.DefaultAllocator, s.rcols, values); err != nil {
			s.err = err
			return false
		}
		s.stats.Rows += int64(len(values))
		return true
	}
}

// convert reads HASHes as rows of the columns' values.
func (s *hashScan) convert(rows []hashRow) ([][]Value, error) {
	out := make([][]Value, len(rows))
	for r, row := range rows {
		vals := make([]Value, len(s.cols))
		for i, col := range s.cols {
			if col.field == "" {
				vals[i] = stringValue(row.key)
			} else {
				vals[i] = nullValue(col.t)
			}
		}
		for j, f := range row.fields {
			i, ok := s.colOf[f]
			if !ok {
				if f != rowIDField {
					if s.stats.UnknownFields == nil {
						s.stats.UnknownFields = map[string]int64{}
					}
					s.stats.UnknownFields[f]++
				}
				continue
			}
			col := s.cols[i]
			v, err := convertValue(row.values[j], col.t)
			if err == nil {
				v, err = Coerce(v, col.t)
			}
			if err != nil {
				if !s.o.NullOnError {
					fix := "-on-error null reads such values as NULL"
					if col.override == "" {
						fix = fmt.Sprintf("the type guessed from %d sampled HASHes; -type %s=VARCHAR reads it as text, -sample -1 guesses from every HASH, and %s",
							s.sampled, col.name, fix)
					}
					return nil, errorf(adbc.StatusInvalidData, "%s, field %q: %s doesn't convert to %s (%s): %v",
						row.key, f, quoteValue(row.values[j]), col.t.SQLName(), fix, err)
				}
				if s.stats.Nulls == nil {
					s.stats.Nulls = map[string]int64{}
				}
				s.stats.Nulls[col.name]++
				v = nullValue(col.t)
			}
			vals[i] = v
		}
		out[r] = vals
	}
	return out, nil
}
