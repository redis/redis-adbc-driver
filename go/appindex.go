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

// The application's index
//
// An adopted table (see inplace.go) can read its rows through the search
// index the application made for them, rather than a second one in the
// driver's format (tableMeta.AppIndex). The driver never creates, alters or
// drops that index. It can serve if it is on HASHes, its only prefix is the
// table's, and it has no FILTER (appIndexProblem): then its documents are
// the HASHes under the prefix, and the FILTER step that every FT.AGGREGATE
// on the table starts with (tableMeta.rowFilter) leaves out those that
// aren't rows (user:42:prefs under user:).
//
// Its attributes aren't the driver's, so each statement reads them
// (store.loadAppIndex, one FT.INFO) and marks what each column's attribute
// can answer (appAttrFor, applyAppAttrs):
//
//   - A NUMERIC attribute on a column the driver indexes as a number is as
//     the driver's own: filters, and, if it is SORTABLE, sorts and
//     aggregates run in the index. RediSearch only loads a SORTABLE
//     attribute's value for APPLY, and a cluster sorts the HASH's text of
//     one that isn't, so those don't (columnMeta.App).
//   - A TAG attribute on a string column finds every row that may equal a
//     value, the rows are re-checked, and strings are sorted and grouped by
//     the driver (the truncated level, see tags.go). Its separator splits
//     values into tags, and without CASESENSITIVE it folds case, which the
//     re-check undoes. Without INDEXEMPTY it holds no empty tag, so '' isn't
//     looked up.
//   - Other attributes (TEXT, GEO, VECTOR, NOINDEX, one named with AS) and
//     columns without one are read from the HASHes, and filtered by the
//     driver.
//
// If the application drops the index, or changes its prefixes or FILTER,
// statements on the table fail with what happened, until it is made again
// as it was.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// appAttr is what the attribute of an application's index on a column's
// field can do (set by applyAppAttrs for each statement).
type appAttr struct {
	sep        string // a TAG's separator
	sortable   bool
	indexEmpty bool // a TAG that holds empty values
}

// hasOption reports whether an attribute has a flag (SORTABLE, …).
func (a indexAttr) hasOption(flag string) bool { return slices.Contains(a.options, flag) }

// separator returns a TAG attribute's separator (',' by default).
func (a indexAttr) separator() string {
	for _, o := range a.options {
		if s, ok := strings.CutPrefix(o, "SEPARATOR "); ok {
			return s
		}
	}
	return ","
}

// appAttrFor returns the attribute of an application's index that serves a
// column of kind on field, if any: a NUMERIC one for a column indexed as a
// number, a TAG one for a string column. An attribute named with AS (its
// name isn't the field) doesn't serve, since queries name a column's field.
func appAttrFor(attrs []indexAttr, field string, kind Kind) (indexAttr, bool) {
	for _, a := range attrs {
		if a.identifier != field || a.name != field || !simpleName(field) || a.hasOption("NOINDEX") {
			continue
		}
		switch {
		case a.typ == "NUMERIC" && kind.indexedAsNumeric():
			return a, true
		case a.typ == "TAG" && kind == KindString && len(a.separator()) == 1:
			return a, true
		}
	}
	return indexAttr{}, false
}

// applyAppAttrs marks each column with what the application's index can
// answer for it.
func applyAppAttrs(meta *tableMeta, attrs []indexAttr) {
	for i := range meta.Columns {
		c := &meta.Columns[i]
		a, ok := appAttrFor(attrs, c.field(), c.Type.Kind)
		c.Indexed, c.App = ok, nil
		if !ok {
			continue
		}
		c.App = &appAttr{sep: a.separator(), sortable: a.hasOption("SORTABLE"), indexEmpty: a.hasOption("INDEXEMPTY")}
		if c.Type.Kind == KindString {
			c.TagsChecked, c.TagValues = true, tagsTruncated
		}
	}
}

// appIndexProblem says why an index can't be the application's index of a
// table whose rows are under prefix, or "".
func appIndexProblem(info indexInfo, prefix string) string {
	switch {
	case info.keyType != "HASH":
		return fmt.Sprintf("it is on %s documents, not HASHes", info.keyType)
	case len(info.prefixes) != 1 || info.prefixes[0] != prefix:
		return fmt.Sprintf("its prefixes are %s, not just %s", strings.Join(info.prefixes, " "), shellQuote(prefix))
	case info.filter != "":
		return fmt.Sprintf("it has the FILTER %q", info.filter)
	}
	return ""
}

// rowFilter is the FILTER expression that FT.AGGREGATE on a table's index
// applies to its documents first, or "": the index of a table with
// AppIndex also holds the HASHes under the prefix that aren't rows.
func (t *tableMeta) rowFilter() string {
	if !t.AppIndex {
		return ""
	}
	return rowKeyFilter(t.prefix())
}

// loadAppIndex reads the application's index of a table with AppIndex,
// and marks what its attributes answer (applyAppAttrs).
func (s *store) loadAppIndex(ctx context.Context, meta *tableMeta) error {
	idx := meta.index()
	name := fmt.Sprintf("%q.%q", displaySchema(meta.Schema), meta.Name)
	reply, err := s.searchDo(ctx, idx, "FT.INFO", idx).Result()
	switch {
	case err != nil && isUnknownIndex(err):
		return errorf(adbc.StatusInvalidState, "%s reads the application's search index %s, which is gone: create it again, or DROP TABLE %s (its HASHes stay) and run redis-adbc adopt again",
			name, idx, name)
	case err != nil:
		return wrapRedis(err, "failed to read the search index "+idx)
	}
	info := indexInfoOf(reply)
	if why := appIndexProblem(info, meta.prefix()); why != "" {
		return errorf(adbc.StatusInvalidState, "%s reads the application's search index %s, which can't serve anymore (%s): make it as it was, or DROP TABLE %s (its HASHes stay) and run redis-adbc adopt again",
			name, idx, why, name)
	}
	applyAppAttrs(meta, info.attrs)
	return nil
}

// appIndexChoice is the application's index that adopting HASHes would
// read the rows through (nil: adopt creates the driver's own), and why the
// other indexes on the HASHes can't serve.
type appIndexChoice struct {
	index *searchIndex
	// unusable gives, for each index on the HASHes that can't serve, why.
	unusable map[string]string
	// blocker is set when the index asked for (-index) can't serve.
	blocker string
}

// chooseAppIndex picks the application's index for adopting the HASHes
// under prefix (keys HASH keys) with these columns: want if it is set
// (-index), none if own is (-own-index), and otherwise, of the indexes
// that can serve, the one with the most attributes that serve a column.
func chooseAppIndex(indexes []searchIndex, cols []*guessedColumn, prefix string, keys int64, want string, own bool) appIndexChoice {
	ch := appIndexChoice{unusable: map[string]string{}}
	best := 0
	found := false
	for i := range indexes {
		ix := &indexes[i]
		if ix.keyType != "HASH" || !slices.ContainsFunc(ix.prefixes, func(p string) bool { return prefixesOverlap(p, prefix) }) {
			continue
		}
		if ix.name == want {
			found = true
		}
		why := appIndexProblem(ix.indexInfo, prefix)
		switch {
		case why != "":
		case ix.failures > 0:
			why = fmt.Sprintf("it has had %s, so it may leave HASHes out", plural(ix.failures, "indexing failure"))
		case ix.indexing || ix.percent < 1:
			why = "it is still indexing"
		case ix.numDocs < keys:
			why = fmt.Sprintf("it holds %d documents, and there are %d HASH keys under %s (made with SKIPINITIALSCAN?)", ix.numDocs, keys, shellQuote(prefix))
		}
		serving := servingAttrs(ix.attrs, cols)
		if why == "" && serving == 0 && ix.name != want {
			why = "none of its attributes is a TAG or NUMERIC one on a column"
		}
		if why != "" {
			ch.unusable[ix.name] = why
			if ix.name == want {
				ch.blocker = fmt.Sprintf("the application's index %s can't serve: %s", want, why)
			}
			continue
		}
		if own || (want != "" && ix.name != want) {
			continue
		}
		if ch.index == nil || serving > best || (serving == best && ix.name < ch.index.name) {
			ch.index, best = ix, serving
		}
	}
	if want != "" && !found {
		ch.blocker = fmt.Sprintf("there is no search index named %s on the HASHes under %s", want, shellQuote(prefix))
	}
	return ch
}

// servingAttrs counts the columns that an index has an attribute for.
func servingAttrs(attrs []indexAttr, cols []*guessedColumn) int {
	n := 0
	for _, col := range cols {
		if col.field == "" {
			continue // the key column of copies
		}
		if _, ok := appAttrFor(attrs, col.fieldOrName(), col.inPlace.Kind); ok {
			n++
		}
	}
	return n
}
