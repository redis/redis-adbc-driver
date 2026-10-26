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

// PRIMARY KEY constraints.
//
// CREATE TABLE takes PRIMARY KEY on a column or as a table constraint
// (PRIMARY KEY (cols)), ALTER TABLE … ADD COLUMN on the new column, and
// ALTER TABLE … ADD [CONSTRAINT name] PRIMARY KEY (cols) on the table. As in
// Postgres 16:
//
//   - Definition: a table has at most one, of its own columns (in ALTER
//     TABLE, also those the statement adds), each named once.
//   - Name: CONSTRAINT name as written, otherwise <table>_pkey, with 1, 2, …
//     appended to a name the table's CHECKs use. A table's constraints have
//     different names: a CHECK defined later avoids the key's name too. In
//     CREATE TABLE the CHECKs are named first, so a key named like one of
//     them is an error. Postgres also keeps the name unique among the
//     schema's relations and constraints (it is the name of the key's
//     index), so it may choose t_pkey1 where the driver chooses t_pkey.
//   - NOT NULL: the key's columns become NOT NULL, as if declared so, and
//     stay NOT NULL if the key is dropped. ALTER TABLE checks the existing
//     rows first, as for SET NOT NULL (see validateRows), and DROP NOT NULL
//     on a key column is an error.
//   - Uniqueness isn't enforced: rows with the same key are written.
//     Enforcing it needs a check that is atomic with the write against
//     every concurrent writer, and rows are separate HASHes (in different
//     hash slots on a cluster), written in pipelines without a transaction
//     across them. A lookup of the key in the search index before writing
//     would let two statements each find no row and both insert one.
//   - Storage: the name and the HASH fields of the key's columns, in key
//     order (tableMeta.PrimaryKey; an optional field, so older metadata
//     reads unchanged). Fields don't change with RENAME COLUMN, so the key
//     follows the columns. RENAME TO (also re-keying) and TRUNCATE keep the
//     key with its name, DROP COLUMN of one of its columns drops it, and
//     DROP CONSTRAINT drops it by name.

import (
	"fmt"
	"slices"
)

// keyMeta is a PRIMARY KEY as the table metadata stores it: its name and
// the HASH fields of its columns, in key order.
type keyMeta struct {
	OID      int64    `json:"oid,omitempty"`
	IndexOID int64    `json:"index_oid,omitempty"`
	Name     string   `json:"name"`
	Fields   []string `json:"fields"`
}

// keyColumns returns the names of the columns of the table's primary key,
// in key order (nil without one).
func (t *tableMeta) keyColumns() []string {
	if t.PrimaryKey == nil {
		return nil
	}
	out := make([]string, 0, len(t.PrimaryKey.Fields))
	for _, f := range t.PrimaryKey.Fields {
		if i := slices.IndexFunc(t.Columns, func(c columnMeta) bool { return c.field() == f }); i >= 0 {
			out = append(out, t.Columns[i].Name)
		}
	}
	return out
}

// inKey reports whether c, a column of the table, is in its primary key.
func (t *tableMeta) inKey(c columnMeta) bool {
	return t.PrimaryKey != nil && slices.Contains(t.PrimaryKey.Fields, c.field())
}

// multipleKeys is Postgres's error for a second primary key of a table.
func multipleKeys(table string) error {
	return stateErrorf(stateTableDefinition, "multiple primary keys for table %q are not allowed", table)
}

// resolveKey resolves the columns of a new primary key of meta, in key
// order. As in Postgres, CREATE TABLE reports a column that doesn't exist
// where it comes in the list, and ALTER TABLE (inAlter) after a column that
// comes twice.
func resolveKey(meta *tableMeta, def KeyDef, inAlter bool) ([]int, error) {
	cols := make([]int, len(def.Columns))
	for i, name := range def.Columns {
		j, ok := meta.resolve(name)
		if !ok && !inAlter {
			return nil, stateErrorf(stateUndefinedColumn, "column %q named in key does not exist", name)
		}
		// Column names are unique regardless of case.
		if slices.ContainsFunc(def.Columns[:i], func(o string) bool { return (o == name) }) {
			return nil, stateErrorf(stateDuplicateColumn, "column %q appears twice in primary key constraint", name)
		}
		cols[i] = j
	}
	for i, j := range cols {
		if j < 0 {
			return nil, columnMissing(def.Columns[i], meta.Name)
		}
	}
	return cols, nil
}

// keyName names a new primary key of meta (see the top of this file).
func keyName(meta *tableMeta, name string) (string, error) {
	used := func(n string) bool {
		return slices.ContainsFunc(meta.Checks, func(c checkMeta) bool { return (c.Name == n) })
	}
	if name != "" {
		if used(name) {
			return "", stateErrorf(stateDuplicateObject, "constraint %q for relation %q already exists", name, meta.Name)
		}
		return name, nil
	}
	name = meta.Name + "_pkey"
	for n := 1; used(name); n++ {
		name = fmt.Sprintf("%s_pkey%d", meta.Name, n)
	}
	return name, nil
}

// setKey makes the columns cols of meta its primary key, named name. They
// become NOT NULL; it returns the fields of those that weren't.
func setKey(meta *tableMeta, name string, cols []int) []string {
	key := &keyMeta{Name: name}
	var made []string
	for _, i := range cols {
		c := &meta.Columns[i]
		key.Fields = append(key.Fields, c.field())
		if c.Nullable {
			made = append(made, c.field())
			c.Nullable = false
		}
	}
	meta.PrimaryKey = key
	return made
}

// definePrimaryKey adds the primary key of CREATE TABLE to meta, which has
// the table's columns: keys are the statement's PRIMARY KEY constraints,
// which may be one at most, and defineChecks is called between checking the
// key's columns and naming it, as Postgres defines a table's CHECKs.
func definePrimaryKey(meta *tableMeta, keys []KeyDef, defineChecks func() error) error {
	var cols []int
	if len(keys) > 0 {
		var err error
		if cols, err = resolveKey(meta, keys[0], false); err != nil {
			return err
		}
		if len(keys) > 1 {
			return multipleKeys(meta.Name)
		}
	}
	if err := defineChecks(); err != nil || len(keys) == 0 {
		return err
	}
	name, err := keyName(meta, keys[0].Name)
	if err != nil {
		return err
	}
	setKey(meta, name, cols)
	return nil
}

// alterAddKey applies ADD PRIMARY KEY, and the PRIMARY KEY of an ADD COLUMN
// (a step after the column: Postgres adds the key's index in a later pass).
// The key's columns become NOT NULL, and as for SET NOT NULL, the existing
// rows are checked for those that weren't.
func alterAddKey(m *tableMeta, def KeyDef, res *alterResult) error {
	cols, err := resolveKey(m, def, true)
	if err != nil {
		return err
	}
	if m.PrimaryKey != nil {
		return multipleKeys(m.Name)
	}
	name, err := keyName(m, def.Name)
	if err != nil {
		return err
	}
	made := setKey(m, name, cols)
	res.notNull = append(res.notNull, made...)
	res.log("primary key %q fields %q, not null %q", name, m.PrimaryKey.Fields, made)
	return nil
}
