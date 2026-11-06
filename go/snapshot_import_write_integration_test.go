// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"io"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func (h *sqlHarness) ingestImportRecord(table, mode string, rec arrow.RecordBatch) error {
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		return err
	}
	defer st.Close(h.ctx)
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(h.ctx, k, v); err != nil {
			return err
		}
	}
	if err := st.Bind(h.ctx, rec); err != nil {
		return err
	}
	_, err = st.ExecuteUpdate(h.ctx)
	return err
}

type importReplaceFailingInput struct {
	array.RecordReader
	batches int
}

func (r *importReplaceFailingInput) Next() bool {
	if !r.RecordReader.Next() {
		return false
	}
	r.batches++
	return true
}
func (*importReplaceFailingInput) Err() error { return io.ErrUnexpectedEOF }

func TestManagedImportArrowReplaceRetainsRefreshIdentity(t *testing.T) {
	f := newHashFixture(t, "it:replace-import:")
	const table = "it_replace_import"
	f.h.dropTables(table)
	t.Cleanup(func() { f.h.dropTables(table) })
	f.hset(f.prefix+"seed", "name", "Seed", "active", "YES")
	if !f.adopt(HashAdoptOptions{Table: table, Apply: true}).Applied {
		t.Fatal("initial import failed")
	}
	old, _ := f.h.openStream(f.h.ctx, f.h.conn, "SELECT __key,name FROM "+table, nil, map[string]string{OptionIntStreamBatchRows: "1"})
	defer old.Release()
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true}}, nil))
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append("Replacement")
	rec := b.NewRecordBatch()
	defer rec.Release()
	// A late Arrow error after replacement rows were consumed must retain the
	// old public schema, rows and import identity, with no publication.
	func() {
		st, err := f.h.conn.NewStatement(f.h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(f.h.ctx)
		for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeReplace} {
			if err := st.SetOption(f.h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		reader, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
		if err != nil {
			t.Fatal(err)
		}
		input := &importReplaceFailingInput{RecordReader: reader}
		if err := st.BindStream(f.h.ctx, input); err != nil {
			t.Fatal(err)
		}
		root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
		if _, err := st.ExecuteUpdate(f.h.ctx); err == nil {
			t.Fatal("late failed replacement published")
		}
		if input.batches != 1 {
			t.Fatalf("failed replacement consumed %d batches; require exactly one", input.batches)
		}
		if root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
			t.Fatal("late replacement error changed the root")
		}
	}()
	f.h.expectRows("SELECT __key,name,active FROM "+table, f.prefix+"seed|Seed|true")
	if err := f.h.ingestImportRecord(table, adbc.OptionValueIngestModeReplace, rec); err != nil {
		t.Fatal(err)
	}
	f.h.expectRows("SELECT __key,name FROM "+table, "NULL|Replacement")
	schema, err := f.h.conn.GetTableSchema(f.h.ctx, nil, nil, table)
	if err != nil || schema.NumFields() != 1 || schema.Field(0).Name != "name" {
		t.Fatal("replace did not use its Arrow public schema", schema, err)
	}
	if !old.Next() || old.RecordBatch().NumRows() != 1 || old.RecordBatch().Column(0).(*array.String).Value(0) != f.prefix+"seed" || old.RecordBatch().Column(1).(*array.String).Value(0) != "Seed" {
		t.Fatal("replace changed the pinned imported stream", old.Err())
	}
	if old.Next() || old.Err() != nil {
		t.Fatal("old imported stream coverage changed", old.Err())
	}
	old.Release()
	f.hset(f.prefix+"seed", "name", "External")
	if !f.adopt(HashAdoptOptions{Table: table, Apply: true}).Applied {
		t.Fatal("explicit refresh after replace failed")
	}
	f.h.expectRows("SELECT __key,name,active FROM "+table, f.prefix+"seed|External|true")
	if f.raw.HGet(f.h.ctx, f.prefix+"seed", "active").Val() != "YES" {
		t.Fatal("replace/refresh changed original encoding")
	}
}

func TestManagedImportProvenanceJoins(t *testing.T) {
	f := newHashFixture(t, "it:join-import:")
	left, right := "it_import_left", "it_import_right"
	f.h.dropTables(left, right)
	t.Cleanup(func() { f.h.dropTables(left, right) })
	keys := []string{f.prefix + "nul\x00A", f.prefix + "雪"}
	for i, key := range keys {
		f.hset(key, "name", []string{"Alpha", "Snow"}[i], "v", 10+i)
	}
	for _, table := range []string{left, right} {
		if !f.adopt(HashAdoptOptions{Table: table, Apply: true}).Applied {
			t.Fatal("copy failed")
		}
	}
	f.h.exec("INSERT INTO " + left + "(name,v) VALUES('Left',99)")
	f.h.exec("INSERT INTO " + right + "(name,v) VALUES('Right',100)")
	f.h.expectRows("SELECT a.__key,a.name,b.name FROM "+left+" a JOIN "+right+" b ON a.__key=b.__key ORDER BY a.__key", keys[0]+"|Alpha|Alpha", keys[1]+"|Snow|Snow")
	f.h.expectRows("SELECT COUNT(*),COUNT(a.__key),COUNT(b.__key) FROM "+left+" a FULL JOIN "+right+" b ON a.__key=b.__key", "4|2|2")
	f.h.expectRows("SELECT p.__key,b.name FROM (SELECT __key FROM "+left+" WHERE __key IS NOT NULL) p JOIN "+right+" b ON p.__key=b.__key ORDER BY p.__key", keys[0]+"|Alpha", keys[1]+"|Snow")
	f.h.expectRows("WITH p AS (SELECT __key,name FROM "+left+") SELECT p.__key,b.name FROM p JOIN "+right+" b ON p.__key=b.__key ORDER BY p.__key", keys[0]+"|Alpha", keys[1]+"|Snow")
	f.h.expectRows("SELECT s.k,a.__key,a.name FROM (VALUES ('missing'),('"+keys[1]+"')) AS s(k) LEFT JOIN "+left+" a ON a.__key=s.k ORDER BY s.k", keys[1]+"|"+keys[1]+"|Snow", "missing|NULL|NULL")
	if schema := f.h.expectRows("SELECT a.*,b.* FROM "+left+" a JOIN "+right+" b ON a.__key=b.__key ORDER BY a.v", "Alpha|10|Alpha|10", "Snow|11|Snow|11"); schema.NumFields() != 4 {
		t.Fatal("qualified star exposed provenance")
	}
	if schema := f.h.expectRows("SELECT * FROM "+left+" NATURAL JOIN "+right+" ORDER BY v", "Alpha|10", "Snow|11"); schema.NumFields() != 2 {
		t.Fatal("natural join exposed provenance")
	}
	f.h.expectError("SELECT __key FROM "+left+" a JOIN "+right+" b ON a.__key=b.__key", "ambiguous")
	f.h.expectError("SELECT a.__rowid FROM "+left+" a JOIN "+right+" b ON a.__key=b.__key", "does not exist")
	f.h.expectReturning("UPDATE "+left+" a SET v=42 FROM "+right+" b WHERE a.__key=b.__key AND a.__key='"+keys[1]+"' RETURNING a.__key,a.v", "__key|v", keys[1]+"|42")
	f.h.expectReturning("DELETE FROM "+left+" a USING "+right+" b WHERE a.__key=b.__key AND a.__key='"+keys[0]+"' RETURNING a.__key", "__key", keys[0])
	f.h.expectRows("SELECT __key,name,v FROM "+left+" ORDER BY v", keys[1]+"|Snow|42", "NULL|Left|99")
	f.h.expectRows("SELECT COUNT(*),SUM(v) FROM "+right, "3|121")
	for i, key := range keys {
		if f.raw.HGet(f.h.ctx, key, "v").Val() != []string{"10", "11"}[i] {
			t.Fatal("joined DML changed application data")
		}
	}
}
