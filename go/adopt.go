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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// HashAdoptOptions selects the HASHes that AdoptHashes adopts, and the
// table they become.
type HashAdoptOptions struct {
	Prefix string
	// Schema and Table name the table (by default the database's schema,
	// and a name made from the prefix).
	Schema, Table string
	// Types sets columns' types in place; every stored value must be
	// readable as one (booleans as 0 and 1, and so on).
	Types map[string]string
	// Renames gives fields other column names (field → column).
	Renames map[string]string
	// IndexColumns are the columns to index (nil: every one that can be).
	IndexColumns []string
	// TrustStrings records how exactly the index holds each string column's
	// values, as measured now. Otherwise every indexed string column is
	// taken to hold values that the index doesn't hold exactly (the
	// application's later writes don't tell the driver), which makes the
	// driver re-check, sort and group string columns itself.
	TrustStrings bool
	// Index names the application's index to read the rows through; by
	// default adopt picks one that can serve, if any (see appindex.go).
	// OwnIndex makes it create the driver's own index even so.
	Index    string
	OwnIndex bool
	// Apply does it; otherwise AdoptHashes only checks and plans.
	Apply bool
	// Progress, if set, is told what is happening.
	Progress func(string)
}

// HashAdoption is what AdoptHashes found, planned and did.
type HashAdoption struct {
	// Report has the collection, the columns (InPlaceType) and the
	// ChecklistSQLInPlace checklist.
	Report *HashReport `json:"report"`
	Table  string      `json:"table"`
	Index  string      `json:"index,omitempty"`
	// Steps are what adopt does (or did, when Applied).
	Steps   []string `json:"steps,omitempty"`
	Applied bool     `json:"applied"`
	// AlreadyAdopted is set when the HASHes are the adopted table already:
	// there is nothing to do (the Report's checklist says whether the
	// driver reads it).
	AlreadyAdopted bool `json:"already_adopted,omitempty"`
	// AppIndex is set when the table reads its rows through the
	// application's index (Index), rather than one adopt creates.
	AppIndex bool `json:"app_index,omitempty"`
}

// adoptingKey (HASH) records adoptions in progress, by key prefix, so that
// running adopt again finishes one that stopped half-way.
const adoptingKey = metaPrefix + "adopting"

// adoptRecord is an adoptingKey entry.
type adoptRecord struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Index  string `json:"index"`
}

// AdoptHashes makes the HASHes under o.Prefix a driver table without
// copying them (see inplace.go). Without o.Apply, it only reads them,
// checks, and says what it would do. The options are the driver's database
// options.
func AdoptHashes(ctx context.Context, options map[string]string, o HashAdoptOptions) (*HashAdoption, error) {
	if o.Prefix == "" {
		return nil, errorf(adbc.StatusInvalidArgument, "a key prefix is required")
	}
	if o.Index != "" && o.OwnIndex {
		return nil, errorf(adbc.StatusInvalidArgument, "-index reads the rows through the application's index, and -own-index creates one: pick one")
	}
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	progress := o.Progress
	if progress == nil {
		progress = func(string) {}
	}
	a := &adopter{c: c, o: o, progress: progress, res: &HashAdoption{Report: &HashReport{Prefix: o.Prefix}}}
	return a.run(ctx)
}

type adopter struct {
	c        *hashConn
	o        HashAdoptOptions
	progress func(string)
	res      *HashAdoption
	schema   string
	table    string
	index    string
	cols     []*guessedColumn
	stats    keyStats
	// app is the application's index the table reads its rows through,
	// or nil.
	app *searchIndex
}

func (a *adopter) run(ctx context.Context) (*HashAdoption, error) {
	c, o, r := a.c, a.o, a.res.Report
	c.server(ctx, &r.Server)
	if o.Apply && r.Server.Search {
		// Writing needs a registry with every table's prefix in it.
		if err := c.st.migrateLegacyMetadata(ctx); err != nil {
			return nil, err
		}
		if err := c.st.ensureRegistry(ctx); err != nil {
			return nil, err
		}
	}
	tables, err := c.driverTables(ctx)
	if err != nil {
		return nil, err
	}
	var indexes []searchIndex
	if r.Server.Search {
		if indexes, r.UnreadableIndexes, err = c.searchIndexes(ctx); err != nil {
			return nil, err
		}
	}
	a.schema, a.table = o.Schema, o.Table
	if a.schema == "" {
		a.schema = c.db.schema
	}
	var existing *tableMeta
	for _, tm := range tables {
		if tm.Adopted && tm.prefix() == o.Prefix {
			existing = tm
		}
	}
	if existing != nil && (o.Table == "" || (existing.Name == o.Table && existing.Schema == a.schema)) {
		a.schema, a.table = existing.Schema, existing.Name
	}
	if a.table == "" {
		a.table = TableNameForPrefix(o.Prefix)
	}
	r.Schema, r.Table = a.schema, a.table
	a.res.Table = displaySchema(a.schema) + "." + a.table
	for _, ix := range indexes {
		if ix.keyType == "HASH" && slices.ContainsFunc(ix.prefixes, func(p string) bool { return prefixesOverlap(p, o.Prefix) }) {
			r.Indexes = append(r.Indexes, HashIndex{Name: ix.name, Prefixes: ix.prefixes, Filter: ix.filter,
				Documents: ix.numDocs, Failures: ix.failures})
		}
	}
	if existing != nil {
		if existing.Schema != a.schema || existing.Name != a.table {
			return nil, errorf(adbc.StatusAlreadyExists, "the HASHes under %q are already the adopted table %s.%s",
				o.Prefix, displaySchema(existing.Schema), existing.Name)
		}
		return a.adopted(ctx, existing, tables, indexes)
	}

	if err := a.read(ctx); err != nil {
		return nil, err
	}
	choice := chooseAppIndex(indexes, a.cols, o.Prefix, a.stats.keys, o.Index, o.OwnIndex)
	a.app = choice.index
	a.index = adoptIndexName(a.schema, a.table)
	if a.app != nil {
		a.index = a.app.name
	}
	a.res.Index, a.res.AppIndex = a.index, a.app != nil
	command := adoptCommand(o.Prefix, a.schema, a.table, c.db.schema, o.Types, o.Renames)
	switch {
	case o.Index != "":
		command += " -index " + shellQuote(o.Index)
	case o.OwnIndex:
		command += " -own-index"
	}
	t := inPlaceTarget{prefix: o.Prefix, schema: a.schema, table: a.table, full: true, sampled: r.Sampled,
		command: command + " -apply", app: choice}
	cl, err := c.checkInPlace(ctx, r, a.cols, &a.stats, tables, indexes, t)
	if err != nil {
		return nil, err
	}
	r.Checks = []Checklist{cl}
	if !cl.Ready {
		return a.res, nil
	}
	meta, err := a.plan()
	if err != nil {
		return nil, err
	}
	if !o.Apply {
		return a.res, nil
	}
	if err := a.apply(ctx, meta); err != nil {
		return a.res, err
	}
	a.res.Applied = true
	return a.res, nil
}

// read counts every HASH key, and guesses the columns from every HASH that
// will be a row (isRowKey).
func (a *adopter) read(ctx context.Context) error {
	c, o, r := a.c, a.o, a.res.Report
	gopts := guessOptions{types: o.Types, renames: o.Renames}
	gs, err := newGuesserFor(gopts)
	if err != nil {
		return err
	}
	ks, err := c.scanKeys(ctx, o.Prefix, "hash")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	last := time.Now()
	for {
		page, done, err := ks.next(ctx)
		if err != nil {
			return err
		}
		keys := page[:0:0]
		for _, k := range page {
			if !seen[k] {
				seen[k] = true
				if a.stats.add(k, o.Prefix) {
					keys = append(keys, k)
				}
			}
		}
		rows, err := c.readHashes(ctx, keys)
		if err != nil {
			return err
		}
		for _, row := range rows {
			gs.add(row)
		}
		if time.Since(last) > 5*time.Second {
			a.progress(fmt.Sprintf("read %d HASHes", a.stats.keys))
			last = time.Now()
		}
		if done {
			break
		}
	}
	r.Keys, r.Sampled = a.stats.keys, gs.rows
	cols, skipped, err := gs.columns(gopts)
	if err != nil {
		return err
	}
	a.cols, r.Skipped = cols, skipped
	for _, col := range cols {
		r.Columns = append(r.Columns, HashColumn{Name: col.name, Field: col.field, Type: col.t.SQLName(),
			InPlaceType: col.inPlace.SQLName(), Present: col.guess.present, Notes: col.notes})
	}
	return nil
}

// plan is the table's metadata, and the steps adopting takes.
func (a *adopter) plan() (*tableMeta, error) {
	o := a.o
	meta := &tableMeta{Schema: a.schema, Name: a.table, KeyPrefix: o.Prefix, IndexName: a.index, Adopted: true, KeyRows: true}
	only := map[string]bool{}
	for _, name := range o.IndexColumns {
		only[name] = true
	}
	n := 0
	for _, col := range a.cols {
		cm := columnMeta{Name: col.name, Type: col.inPlace, Nullable: true}
		if col.field != col.name {
			cm.Field = col.field
		}
		want := indexable(cm.Type) && simpleName(cm.field()) && (o.IndexColumns == nil || only[col.name])
		cm.Indexed = want && n < maxIndexed
		if cm.Indexed {
			n++
			if cm.Type.Kind == KindString {
				// Only "truncated" stays right whatever the application
				// writes later (tags.go).
				cm.TagsChecked = true
				cm.TagValues = tagsTruncated
				if o.TrustStrings {
					cm.TagValues = col.guess.tagLevel
				}
			}
		}
		meta.Columns = append(meta.Columns, cm)
	}
	for name := range only {
		if _, ok := meta.resolve(name); !ok {
			return nil, errorf(adbc.StatusInvalidArgument, "-index-columns: there is no column %q", name)
		}
	}
	if len(meta.Columns) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "the HASHes under %q have no field that can be a column", o.Prefix)
	}
	if a.app != nil {
		if o.IndexColumns != nil {
			return nil, errorf(adbc.StatusInvalidArgument, "-index-columns picks the columns of the index adopt creates, and adopt reads the rows through the application's index %s: add -own-index", a.index)
		}
		meta.AppIndex = true
		applyAppAttrs(meta, a.app.attrs)
		a.res.Steps = []string{
			fmt.Sprintf("reserve the key prefix %s (%s, %s)", o.Prefix, prefixesKey, adoptedKey),
			fmt.Sprintf("read the rows through the application's index %s, which adopt doesn't change", a.index),
			fmt.Sprintf("check that %s has indexed the %s under %s, and had no indexing failures", a.index,
				plural(a.stats.keys, "HASH", "HASHes"), o.Prefix),
			fmt.Sprintf("SET %s (the table's metadata), SADD %s %s", metaKey(a.schema, a.table), tablesKey(a.schema), a.table),
		}
		return meta, nil
	}
	a.res.Steps = []string{
		fmt.Sprintf("reserve the key prefix %s and the index name %s (%s, %s, %s)", o.Prefix, a.index, prefixesKey, adoptedKey, indexesKey),
		strings.Join(argText(adoptIndexArgs(meta)), " "),
		fmt.Sprintf("wait until the index has indexed the %s, and check that it had no indexing failures",
			plural(a.stats.rows, "HASH", "HASHes")),
		fmt.Sprintf("SET %s (the table's metadata), SADD %s %s", metaKey(a.schema, a.table), tablesKey(a.schema), a.table),
	}
	return meta, nil
}

// adoptIndexArgs is the FT.CREATE of an adopted table's index: the
// driver's, over existing keys (no SKIPINITIALSCAN), only for the HASHes
// that are rows (rowKeyFilter), and with the fields that would set a
// document's language, score or payload renamed to ones no application
// uses. Its __rowid attribute is in no HASH (see inplace.go).
func adoptIndexArgs(meta *tableMeta) []any {
	args := []any{"FT.CREATE", meta.index(), "ON", "HASH", "PREFIX", 1, meta.prefix(),
		"FILTER", rowKeyFilter(meta.prefix()),
		"LANGUAGE_FIELD", "__adbc_language", "SCORE_FIELD", "__adbc_score", "PAYLOAD_FIELD", "__adbc_payload",
		"SCHEMA", rowIDField, "NUMERIC"}
	for _, c := range meta.Columns {
		if c.Indexed {
			args = append(args, indexAttrArgs(c)...)
		}
	}
	return args
}

// argText renders a command's arguments for reading.
func argText(args []any) []string {
	out := make([]string, len(args))
	for i, a := range args {
		s := fmt.Sprint(a)
		if s == tagSeparator {
			s = `"\x1f"`
		} else if strings.ContainsAny(s, " ()@") {
			s = strconv.Quote(s)
		}
		out[i] = s
	}
	return out
}

func (a *adopter) apply(ctx context.Context, meta *tableMeta) error {
	c, o := a.c, a.o
	rec, err := json.Marshal(adoptRecord{Schema: a.schema, Table: a.table, Index: a.index})
	if err != nil {
		return err
	}
	resumed, err := a.reserve(ctx, string(rec))
	if err != nil {
		return err
	}
	created := false
	undo := func(cause error) error {
		bg := context.WithoutCancel(ctx)
		if created {
			// Without DD: the HASHes are the application's.
			if err := c.st.searchDo(bg, a.index, "FT.DROPINDEX", a.index).Err(); err != nil && !isUnknownIndex(err) {
				return fmt.Errorf("%w (and dropping the index failed, so its names stay reserved: run FT.DROPINDEX %s, without DD, and run adopt -apply again: %v)",
					cause, a.index, err)
			}
		}
		pipe := c.client.TxPipeline()
		pipe.SRem(bg, prefixesKey, o.Prefix)
		pipe.SRem(bg, adoptedKey, o.Prefix)
		if a.app == nil {
			pipe.SRem(bg, indexesKey, a.index)
		}
		pipe.HDel(bg, adoptingKey, o.Prefix)
		_, _ = pipe.Exec(bg)
		return cause
	}

	if a.app == nil {
		if resumed {
			// The adopt that stopped may have created the index, maybe as an
			// earlier version did: make it again (without DD).
			if err := c.st.searchDo(ctx, a.index, "FT.DROPINDEX", a.index).Err(); err != nil && !isUnknownIndex(err) {
				return wrapRedis(err, "failed to drop the index of the adopt that stopped")
			}
		}
		if err := c.st.searchDo(ctx, a.index, adoptIndexArgs(meta)...).Err(); err != nil {
			return undo(wrapRedis(err, "failed to create the search index"))
		}
		created = true
		a.progress(fmt.Sprintf("created the index %s; indexing %s", a.index, plural(a.stats.rows, "HASH", "HASHes")))
	}
	if err := a.waitIndexed(ctx); err != nil {
		return undo(err)
	}
	if err := a.commit(ctx, meta); err != nil {
		return undo(err)
	}
	return nil
}

// reserve reserves the prefix and the name of the index adopt creates, in
// one transaction that checks again that no driver prefix overlaps the
// prefix. It reports whether an earlier adopt of the same prefix and table
// had reserved them (and stopped).
func (a *adopter) reserve(ctx context.Context, rec string) (resumed bool, err error) {
	c, prefix := a.c, a.o.Prefix
	for attempt := 0; attempt < 20; attempt++ {
		err = c.client.Watch(ctx, func(tx *goredis.Tx) error {
			prev, err := tx.HGet(ctx, adoptingKey, prefix).Result()
			if err != nil && !errors.Is(err, goredis.Nil) {
				return err
			}
			if prev != "" {
				if prev != rec {
					var p adoptRecord
					_ = json.Unmarshal([]byte(prev), &p)
					return errorf(adbc.StatusAlreadyExists, "an adopt of %q into %s.%s stopped before it finished (or is running): run it again with -table %s to finish it",
						prefix, displaySchema(p.Schema), p.Table, p.Table)
				}
				resumed = true
				return nil
			}
			used, err := tx.SMembers(ctx, prefixesKey).Result()
			if err != nil {
				return err
			}
			adopted, err := tx.SMembers(ctx, adoptedKey).Result()
			if err != nil {
				return err
			}
			// A released prefix doesn't count: a statement still writing
			// rows there finds out (checkKeys) by its generation.
			for _, set := range [][]string{used, adopted} {
				if p := adoptedOverlap(set, prefix); p != "" {
					return errorf(adbc.StatusAlreadyExists, "keys under %q can be under %q, a driver table's key prefix", prefix, p)
				}
			}
			if a.app == nil { // the application's index isn't the driver's to name
				if ok, err := tx.SIsMember(ctx, indexesKey, a.index).Result(); err != nil || ok {
					if err == nil {
						err = errorf(adbc.StatusAlreadyExists, "the index name %s is taken", a.index)
					}
					return err
				}
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.SAdd(ctx, prefixesKey, prefix)
				p.SAdd(ctx, adoptedKey, prefix)
				if a.app == nil {
					p.SAdd(ctx, indexesKey, a.index)
				}
				p.HSet(ctx, adoptingKey, prefix, rec)
				return nil
			})
			return err
		}, adoptingKey, prefixesKey, adoptedKey, indexesKey)
		if !errors.Is(err, goredis.TxFailedErr) {
			break
		}
	}
	if err != nil {
		return false, wrapRedis(err, "failed to reserve the key prefix")
	}
	return resumed, nil
}

// waitIndexed waits until the index (the new one, or the application's) has
// indexed every existing HASH, and fails if it couldn't index some (a value
// that a NUMERIC attribute can't hold, written since the HASHes were read),
// or if the application's index can't serve anymore.
func (a *adopter) waitIndexed(ctx context.Context) error {
	last := time.Now()
	for {
		reply, err := a.c.st.searchDo(ctx, a.index, "FT.INFO", a.index).Result()
		if err != nil {
			return wrapRedis(err, "failed to read the search index")
		}
		info := indexInfoOf(reply)
		if why := appIndexProblem(info, a.o.Prefix); a.app != nil && why != "" {
			return errorf(adbc.StatusInvalidState, "the application's index %s changed while adopt ran, and can't serve anymore: %s", a.index, why)
		}
		if info.failures > 0 {
			// hash_indexing_failures counts attempts: a HASH written while
			// the index scans the existing keys can fail twice.
			return errorf(adbc.StatusInvalidData, "the index had %s (the last: %s: %s): fix those values and run adopt again",
				plural(info.failures, "indexing failure"), info.lastErrorKey, info.lastError)
		}
		if !info.indexing && info.percent >= 1 {
			if info.numDocs < a.stats.rows {
				a.progress(fmt.Sprintf("the index has %d of the %d HASHes (the application may have deleted some meanwhile)", info.numDocs, a.stats.rows))
			}
			return nil
		}
		if time.Since(last) > 5*time.Second {
			a.progress(fmt.Sprintf("indexing: %.0f%%", 100*info.percent))
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return wrapRedis(ctx.Err(), "adopt stopped")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// commit writes the table's metadata, and finishes the adoption record.
func (a *adopter) commit(ctx context.Context, meta *tableMeta) error {
	c := a.c
	key := metaKey(a.schema, a.table)
	for attempt := 0; attempt < 20; attempt++ {
		err := c.client.Watch(ctx, func(tx *goredis.Tx) error {
			if n, err := tx.Exists(ctx, key, viewKey(a.schema, a.table)).Result(); err != nil || n > 0 {
				if err == nil {
					err = errorf(adbc.StatusAlreadyExists, "%s.%s was created meanwhile", displaySchema(a.schema), a.table)
				}
				return err
			}
			gen, err := tx.HGet(ctx, releasedKey, meta.KeyPrefix).Int64()
			if err != nil && !errors.Is(err, goredis.Nil) {
				return err
			}
			meta.PrefixGen = gen
			raw, err := marshalMeta(meta)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.Set(ctx, key, raw, 0)
				p.SAdd(ctx, tablesKey(a.schema), a.table)
				p.SAdd(ctx, schemasKey, a.schema)
				p.HDel(ctx, adoptingKey, meta.KeyPrefix)
				return nil
			})
			return err
		}, key, viewKey(a.schema, a.table), releasedKey, adoptingKey)
		if !errors.Is(err, goredis.TxFailedErr) {
			return wrapRedis(err, "failed to write the table's metadata")
		}
	}
	return errorf(adbc.StatusIO, "the table's metadata is being changed concurrently; try again")
}

// adopted reports on HASHes that are the adopted table meta already.
func (a *adopter) adopted(ctx context.Context, meta *tableMeta, tables []*tableMeta, indexes []searchIndex) (*HashAdoption, error) {
	a.index, a.res.Index = meta.index(), meta.index()
	a.res.AlreadyAdopted = true
	r := a.res.Report
	for _, cm := range meta.Columns {
		r.Columns = append(r.Columns, HashColumn{Name: cm.Name, Field: cm.Field, Type: cm.Type.SQLName(),
			InPlaceType: cm.Type.SQLName()})
	}
	t := inPlaceTarget{prefix: meta.prefix(), schema: meta.Schema, table: meta.Name, full: true, existing: meta}
	cl, err := a.c.checkInPlace(ctx, r, nil, &a.stats, tables, indexes, t)
	if err != nil {
		return nil, err
	}
	r.Checks = []Checklist{cl}
	return a.res, nil
}
