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
	"reflect"
	"testing"
)

func TestDeclarationParsing(t *testing.T) {
	statements, err := ParseScript(`CREATE TABLE t (a integer CONSTRAINT ua UNIQUE, b integer REFERENCES parent(id), c integer, CONSTRAINT multi UNIQUE(c,a), FOREIGN KEY(c,b) REFERENCES parent(x,y) ON DELETE CASCADE); ALTER TABLE t ADD CONSTRAINT fk FOREIGN KEY(a) REFERENCES parent(id)`)
	if err != nil {
		t.Fatal(err)
	}
	defs := statements[0].Stmt.(*CreateTableStmt).Declarations
	if len(defs) != 4 {
		t.Fatalf("declarations: %#v", defs)
	}
	if defs[0].Name != "ua" || defs[0].Kind != "UNIQUE" || !reflect.DeepEqual(defs[2].Columns, []string{"c", "a"}) || !reflect.DeepEqual(defs[3].ReferenceColumns, []string{"x", "y"}) {
		t.Fatalf("declarations: %#v", defs)
	}
	if statements[1].Stmt.(*AlterTableStmt).Cmds[0].Declaration.Name != "fk" {
		t.Fatal("missing ALTER declaration")
	}
	if _, err = ParseScript(`CREATE TABLE t(a integer, b integer, FOREIGN KEY(a,b) REFERENCES p(a))`); err == nil {
		t.Fatal("accepted reference arity mismatch")
	}
}

func TestDeclarationStableIdentity(t *testing.T) {
	parent := &tableMeta{OID: 100, Schema: defaultSchema, Name: "parent", Columns: []columnMeta{{Name: "a"}, {Name: "b"}}}
	child := &tableMeta{OID: 101, Schema: defaultSchema, Name: "child", Columns: []columnMeta{{Name: "x"}, {Name: "y"}}}
	ref := TableName{Name: "parent"}
	err := addDeclarations(child, []DeclarationDef{{Kind: "UNIQUE", Columns: []string{"y", "x"}}, {Kind: "FOREIGN KEY", Columns: []string{"y", "x"}, Reference: ref, ReferenceColumns: []string{"a", "b"}}}, map[TableName]*tableMeta{ref: parent}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent.Name = "renamed"
	parent.Columns[0].Field = "a"
	parent.Columns[0].Name = "new_a"
	fk := declarationInfo(child, child.Declarations[1], map[string]*tableMeta{declarationIdentity(parent): parent})
	if len(fk.ConstraintColumnUsage) != 2 || fk.ConstraintColumnUsage[0].ForeignKeyTable != "renamed" || fk.ConstraintColumnUsage[0].ForeignKeyColumn != "new_a" {
		t.Fatalf("usage: %#v", fk)
	}
	replacement := cloneMeta(parent)
	replacement.OID++
	if got := declarationInfo(child, child.Declarations[1], map[string]*tableMeta{declarationIdentity(replacement): replacement}); got.ConstraintColumnUsage != nil {
		t.Fatal("reference rebound to replacement")
	}
	parent.Columns = parent.Columns[1:]
	if got := declarationInfo(child, child.Declarations[1], map[string]*tableMeta{declarationIdentity(parent): parent}); got.ConstraintColumnUsage != nil {
		t.Fatal("partial reference usage exposed")
	}
	clone := cloneMeta(child)
	clone.Declarations[0].Fields[0] = "changed"
	if child.Declarations[0].Fields[0] != "y" {
		t.Fatal("metadata clone aliases declaration fields")
	}
}

func TestDeclarationUnresolvedAndNames(t *testing.T) {
	m := &tableMeta{Schema: defaultSchema, Name: "t", Columns: []columnMeta{{Name: "a"}}}
	defs := []DeclarationDef{{Kind: "UNIQUE", Columns: []string{"a"}}, {Kind: "UNIQUE", Name: "t_a_key", Columns: []string{"a"}}, {Kind: "FOREIGN KEY", Columns: []string{"a"}, Reference: TableName{Name: "missing"}, ReferenceColumns: []string{"a"}}}
	if err := addDeclarations(m, defs, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.Declarations[0].Name != "t_a_key1" {
		t.Fatal(m.Declarations)
	}
	if got := declarationInfo(m, m.Declarations[2], nil); got.ConstraintColumnUsage != nil {
		t.Fatal("unresolved usage")
	}
	if err := addDeclarations(m, []DeclarationDef{{Name: "t_a_key", Kind: "UNIQUE", Columns: []string{"a"}}}, nil, nil); err == nil {
		t.Fatal("duplicate name accepted")
	}
	res := &alterResult{}
	if err := alterDropColumn(m, AlterCmd{Column: "a"}, res); err != nil {
		t.Fatal(err)
	}
	if len(m.Declarations) != 0 {
		t.Fatal("declarations survived local column drop")
	}
}

func TestDeclarationLogicalCatalogAndSelf(t *testing.T) {
	m := &tableMeta{Schema: catalogSchema("analytics", "reporting"), Name: "self", Columns: []columnMeta{{Name: "id"}, {Name: "parent"}}}
	ref := TableName{Name: "self"}
	if err := addDeclarations(m, []DeclarationDef{{Kind: "FOREIGN KEY", Columns: []string{"parent"}, Reference: ref, ReferenceColumns: []string{"id"}}}, map[TableName]*tableMeta{ref: m}, nil); err != nil {
		t.Fatal(err)
	}
	m.Name = "renamed"
	info := declarationInfo(m, m.Declarations[0], nil)
	if len(info.ConstraintColumnUsage) != 1 {
		t.Fatal(info)
	}
	usage := info.ConstraintColumnUsage[0]
	if *usage.ForeignKeyCatalog != "analytics" || *usage.ForeignKeyDbSchema != "reporting" || usage.ForeignKeyTable != "renamed" {
		t.Fatalf("leaked storage identity: %#v", usage)
	}
}

func TestDeclarationReplacementEpoch(t *testing.T) {
	old := &tableMeta{OID: 91, DeclarationEpoch: "old"}
	replacement := &tableMeta{OID: 91, DeclarationEpoch: "new"}
	if declarationIdentity(old) == declarationIdentity(replacement) {
		t.Fatal("replacement reused identity")
	}
	clone := cloneMeta(old)
	if declarationIdentity(old) != declarationIdentity(clone) {
		t.Fatal("clone changed identity")
	}
	legacy := &tableMeta{OID: 92}
	if declarationIdentity(legacy) != declarationIdentity(cloneMeta(legacy)) {
		t.Fatal("legacy clone changed identity")
	}
}
