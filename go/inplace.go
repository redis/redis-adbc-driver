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

// Adopting HASHes in place
//
// An application's HASHes can become a driver table without being copied
// (AdoptHashes, adopt.go). Adopting writes nothing into them; the table has:
//
//   - its rows identified by their keys (KeyRows, see keyrows.go): every
//     HASH under the prefix with no ':' after it, whatever its key, is a
//     row, including those the application writes later.
//   - a search index in the driver's format: each column as indexAttrArgs
//     makes it, and a __rowid NUMERIC attribute that no HASH needs (FT.CREATE
//     needs one attribute, and noMatchQuery names it). The application's own
//     index can't serve (the driver never checks an index's attributes, and
//     its queries assume them), so adopting creates a second one, whose
//     FILTER (rowKeyFilter) leaves out the HASHes that aren't rows.
//   - values stored as the driver stores them (decodeStored): numbers as
//     numbers, but booleans as 0 and 1 and dates and times as integers. So
//     a column is only BIGINT, NUMERIC or DOUBLE PRECISION in place if every
//     value is such a number; true/false text or ISO dates are VARCHAR.
//   - the table's metadata, marked adopted (see refuseAdopted for what the
//     driver then refuses), and its prefix registered.
//
// redis-adbc v0.0.11 identified an adopted table's rows by __rowid, which
// it wrote into each HASH whose key ended in an integer. The driver refuses
// such a table (errAdoptedByRowID), but DROP TABLE drops it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// ChecklistSQLInPlace names the checklist for adopting HASHes in place.
const ChecklistSQLInPlace = "sql_in_place"

// adoptIndexName is the name of an adopted table's index. Escaping turns
// ':' into %3A in schema and table names, so no driver table's index can
// have it (namesFor).
func adoptIndexName(schema, table string) string { return indexName(schema, table) + ":adopted" }

// keyStats counts the HASH keys under a prefix, and those that adopting
// makes rows (isRowKey).
type keyStats struct {
	keys     int64
	rows     int64
	nested   int64 // keys with another ':' after the prefix
	nestedEx string
}

// add counts a key, and reports whether it is a row.
func (k *keyStats) add(key, prefix string) bool {
	k.keys++
	if isRowKey(prefix, key) {
		k.rows++
		return true
	}
	k.nested++
	if k.nestedEx == "" {
		k.nestedEx = key
	}
	return false
}

// inPlaceTarget is the table that adopting would make.
type inPlaceTarget struct {
	prefix, schema, table string
	// full is whether the columns were guessed from every HASH (adopt)
	// rather than a sample (check).
	full     bool
	sampled  int
	command  string // the adopt command line
	existing *tableMeta
	// resuming is set when an earlier adopt into the same table stopped
	// half-way (adoptingKey), and reserved the names.
	resuming bool
}

func (t inPlaceTarget) name() string { return displaySchema(t.schema) + "." + t.table }

// verb picks the form for n things.
func verb(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// whyWeaker says why a column's type in place is weaker than a copy's.
func whyWeaker(col *guessedColumn) string {
	g := col.guess
	switch {
	case col.t.Kind == KindBool:
		return "the driver stores booleans as 0 and 1"
	case col.t.Kind == KindDate || col.t.Kind == KindTimestamp:
		return "the driver stores dates and times as numbers"
	case g != nil && g.empty > 0:
		return fmt.Sprintf("%d HASHes have it empty, and RediSearch doesn't index a HASH whose NUMERIC field isn't a number", g.empty)
	case g != nil && g.counts[classSpecial] > 0:
		return "it holds NaN or infinities, which the driver stores differently"
	}
	return "the values aren't stored as the driver stores that type"
}

// checkInPlace is the checklist for adopting the HASHes in place.
func (c *hashConn) checkInPlace(ctx context.Context, r *HashReport, cols []*guessedColumn, k *keyStats,
	tables []*tableMeta, indexes []searchIndex, t inPlaceTarget) (Checklist, error) {
	cl := Checklist{Name: ChecklistSQLInPlace, Title: "SQL, adopted in place (redis-adbc adopt)"}
	c.checkServer(&cl, r)
	if t.existing != nil {
		return c.checkAdopted(ctx, cl, t)
	}
	for _, tm := range tables {
		if tm.prefix() == t.prefix {
			cl.add(CheckOK, fmt.Sprintf("these are the rows of the driver table %s, which SQL already reads",
				displaySchema(tm.Schema)+"."+tm.Name), "")
			cl.finish("")
			return cl, nil
		}
	}
	rec, err := c.adoptingRecord(ctx, t.prefix)
	if err != nil {
		return cl, err
	}
	if rec != nil {
		if rec.Schema == t.schema && rec.Table == t.table {
			t.resuming = true
			cl.add(CheckWarn, fmt.Sprintf("an adopt of %s into %s stopped before it finished (or is running)", shellQuote(t.prefix), t.name()),
				"adopt -apply finishes it")
		} else {
			cl.add(CheckBlocker, fmt.Sprintf("an adopt of %s into %s.%s stopped before it finished (or is running)", shellQuote(t.prefix),
				displaySchema(rec.Schema), rec.Table), fmt.Sprintf("run adopt -apply with -table %s to finish it", rec.Table))
		}
	}
	if err := c.checkAdoptPrefix(ctx, &cl, tables, t); err != nil {
		return cl, err
	}
	if err := c.checkAdoptNames(ctx, &cl, indexes, t); err != nil {
		return cl, err
	}

	// The keys.
	switch {
	case k.keys == 0:
		cl.add(CheckBlocker, fmt.Sprintf("there are no HASH keys under %s", shellQuote(t.prefix)), "")
	case k.rows == 0:
		cl.add(CheckBlocker, fmt.Sprintf("every HASH key under %s has another ':' after it (such as %s), so none would be a row",
			shellQuote(t.prefix), k.nestedEx), "adopt the prefix up to the last ':' of the keys (redis-adbc check lists the collections)")
	case k.nested > 0:
		cl.add(CheckOK, fmt.Sprintf("%s under %s %s rows", plural(k.rows, "HASH", "HASHes"), shellQuote(t.prefix), verb(k.rows, "is", "are")), "")
		cl.add(CheckWarn, fmt.Sprintf("%s (such as %s) %s another ':' after %s, and %s out of the table",
			plural(k.nested, "key", "keys"), k.nestedEx, verb(k.nested, "has", "have"), shellQuote(t.prefix), verb(k.nested, "stays", "stay")), "")
	default:
		cl.add(CheckOK, fmt.Sprintf("every HASH under %s is a row (%d)", shellQuote(t.prefix), k.rows), "")
	}

	// The columns.
	if !t.full && r.Keys > int64(t.sampled) {
		cl.add(CheckWarn, fmt.Sprintf("the in-place types are guessed from %d of the %d HASHes", t.sampled, r.Keys),
			"adopt reads them all, and refuses values that don't fit their column")
	}
	n := 0
	for _, col := range cols {
		if col.field == "" {
			continue // the key column of copies
		}
		g := col.guess
		if col.override != "" && g != nil && g.misfits > 0 {
			cl.add(CheckBlocker, fmt.Sprintf("-type %s=%s: %s %s (such as %s in %s: %v)", col.name, col.override,
				plural(int64(g.misfits), "value", "values"), verb(int64(g.misfits), "doesn't fit", "don't fit"),
				quoteValue(g.misfit.value), g.misfit.key, storedFits(g.misfit.value, col.inPlace)),
				"pick another type, or copy the HASHes instead")
			continue
		}
		if col.override == "" && col.inPlace.Kind != col.t.Kind && col.t.Kind != KindString {
			cl.add(CheckWarn, fmt.Sprintf("%s is %s in place (%s when copied): %s", col.name, col.inPlace.SQLName(),
				col.t.SQLName(), whyWeaker(col)), "")
		}
		if indexable(col.inPlace) {
			if !simpleName(col.fieldOrName()) {
				cl.add(CheckWarn, fmt.Sprintf("field %q isn't indexed: only names of letters, digits and _ are", col.field),
					"filters on it read every row")
				continue
			}
			n++
		}
	}
	if n > maxIndexed {
		cl.add(CheckWarn, fmt.Sprintf("%d columns can be indexed, but a table indexes at most %d", n, maxIndexed),
			"adopt -index-columns picks the ones to index")
	}
	for _, s := range r.Skipped {
		if !strings.HasPrefix(s, rowIDField+":") {
			cl.add(CheckWarn, "not a column: "+s, "")
		}
	}

	// What adopt creates.
	var apps []string
	for _, ix := range r.Indexes {
		apps = append(apps, ix.Name)
	}
	idx := fmt.Sprintf("a search index in the driver's format on %s", shellQuote(t.prefix))
	fix := fmt.Sprintf("adopt creates %s, over the HASHes that are rows, including those the application writes later; it takes memory of its own",
		adoptIndexName(t.schema, t.table))
	if len(apps) > 0 {
		fix += fmt.Sprintf(" (the application's %s stays as it is, and isn't used)", strings.Join(apps, ", "))
	}
	cl.add(CheckMissing, idx, fix)
	cl.add(CheckMissing, fmt.Sprintf("the table's metadata (%s)", metaKey(t.schema, t.table)), "adopt writes it")
	for _, h := range adoptHazards {
		cl.add(CheckWarn, h, "")
	}
	cl.finish(t.command)
	return cl, nil
}

// adoptHazards are what an adopted table's users should know.
var adoptHazards = []string{
	"a write by the application of a value a numeric column can't hold (text, an empty value, NaN) takes that HASH out of the index, and so out of the table",
	"SQL UPDATE and DELETE change the application's HASHes (an UPDATE that would leave a HASH without fields is refused); INSERT, TRUNCATE, ADD COLUMN and a re-keying RENAME are refused, and DROP TABLE drops only the index and the metadata",
}

// checkAdoptPrefix checks that no driver key can be under the prefix.
func (c *hashConn) checkAdoptPrefix(ctx context.Context, cl *Checklist, tables []*tableMeta, t inPlaceTarget) error {
	if t.prefix == "" {
		cl.add(CheckBlocker, "the prefix is empty", "adopt needs the collection's own key prefix")
		return nil
	}
	if prefixesOverlap(t.prefix, metaPrefix) || prefixesOverlap(t.prefix, tempSchemaBase) {
		cl.add(CheckBlocker, fmt.Sprintf("keys under %s can be the driver's own (%s…, %s…)", shellQuote(t.prefix), metaPrefix, tempSchemaBase), "")
		return nil
	}
	for _, tm := range tables {
		if prefixesOverlap(tm.prefix(), t.prefix) {
			cl.add(CheckBlocker, fmt.Sprintf("keys under %s can be rows of the driver table %s (%s)", shellQuote(t.prefix),
				displaySchema(tm.Schema)+"."+tm.Name, tm.prefix()), "")
			return nil
		}
	}
	pipe := c.client.Pipeline()
	used := pipe.SMembers(ctx, prefixesKey)
	adopted := pipe.SMembers(ctx, adoptedKey)
	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return wrapRedis(err, "failed to read the table registry")
	}
	for _, set := range [][]string{used.Val(), adopted.Val()} {
		if t.resuming {
			// The earlier adopt reserved the prefix itself.
			set = slices.DeleteFunc(slices.Clone(set), func(p string) bool { return p == t.prefix })
		}
		if p := adoptedOverlap(set, t.prefix); p != "" {
			cl.add(CheckBlocker, fmt.Sprintf("keys under %s can be under %s, a driver table's key prefix", shellQuote(t.prefix), p), "")
			return nil
		}
	}
	cl.add(CheckOK, fmt.Sprintf("no driver table's rows can be under %s", shellQuote(t.prefix)), "")
	return nil
}

// checkAdoptNames checks that the table, view and index names are free.
func (c *hashConn) checkAdoptNames(ctx context.Context, cl *Checklist, indexes []searchIndex, t inPlaceTarget) error {
	if reservedSchema(t.schema) {
		cl.add(CheckBlocker, fmt.Sprintf("%s is a temporary schema", t.schema), "pick a permanent schema with -schema")
		return nil
	}
	view, err := c.st.viewExists(ctx, t.schema, t.table)
	if err != nil {
		return err
	}
	table, err := c.st.tableExists(ctx, t.schema, t.table)
	if err != nil {
		return err
	}
	switch {
	case view:
		cl.add(CheckBlocker, fmt.Sprintf("%s is a view", t.name()), "pick another table name with -table")
	case table:
		cl.add(CheckBlocker, fmt.Sprintf("the table %s exists", t.name()), "pick another table name with -table")
	default:
		cl.add(CheckOK, fmt.Sprintf("%s doesn't exist yet", t.name()), "")
	}
	idx := adoptIndexName(t.schema, t.table)
	for _, ix := range indexes {
		if ix.name == idx && !t.resuming {
			cl.add(CheckBlocker, fmt.Sprintf("a search index named %s exists", idx), "pick another table name with -table")
		}
	}
	return nil
}

// adoptingRecord reads the record of an adopt of the prefix that hasn't
// finished, if any.
func (c *hashConn) adoptingRecord(ctx context.Context, prefix string) (*adoptRecord, error) {
	raw, err := c.client.HGet(ctx, adoptingKey, prefix).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapRedis(err, "failed to read "+adoptingKey)
	}
	var rec adoptRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return nil, errorf(adbc.StatusInternal, "corrupt %s entry for %q: %v", adoptingKey, prefix, err)
	}
	return &rec, nil
}

// checkAdopted checks a table that was adopted already: there is nothing
// to do, unless redis-adbc v0.0.11 adopted it.
func (c *hashConn) checkAdopted(ctx context.Context, cl Checklist, t inPlaceTarget) (Checklist, error) {
	m := t.existing
	name := displaySchema(m.Schema) + "." + m.Name
	if !m.KeyRows {
		cl.add(CheckBlocker, fmt.Sprintf("these HASHes are the table %s, which redis-adbc v0.0.11 adopted, and the driver no longer reads", name),
			fmt.Sprintf("DROP TABLE %s (the HASHes stay), and adopt them again", name))
		cl.finish("")
		return cl, nil
	}
	cl.add(CheckOK, fmt.Sprintf("these HASHes are the adopted table %s, which reads the HASHes the application writes", name), "")
	if reply, err := c.st.searchDo(ctx, m.index(), "FT.INFO", m.index()).Result(); err == nil {
		if info := indexInfoOf(reply); info.failures > 0 {
			cl.add(CheckWarn, fmt.Sprintf("its index %s has had %s (the last: %s: %s); a HASH it couldn't index is left out of the table",
				m.index(), plural(info.failures, "indexing failure"), info.lastErrorKey, info.lastError),
				"fix those values, and write them again")
		}
	}
	cl.finish("")
	return cl, nil
}
