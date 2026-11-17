// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"testing"
)

func TestSQLQuotedNamedIndexes(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP SCHEMA IF EXISTS it_quoted_idx CASCADE")
	t.Cleanup(func() { h.exec("DROP SCHEMA IF EXISTS it_quoted_idx CASCADE") })
	h.exec("CREATE SCHEMA it_quoted_idx")
	h.exec(`CREATE TABLE it_quoted_idx.source_rows ("Customer ID" BIGINT, "CamelCase" BIGINT NOINDEX, "a""b:@{}[]| -" TEXT)`)
	h.exec(`INSERT INTO it_quoted_idx.source_rows VALUES (1,2,'One'),(3,4,'Two'),(NULL,NULL,NULL)`)
	h.exec("CREATE MATERIALIZED VIEW it_quoted_idx.mv AS SELECT * FROM it_quoted_idx.source_rows")
	for _, table := range []string{"source_rows", "mv"} {
		t.Run(table, func(t *testing.T) {
			local := *h
			local.t = t
			h := &local
			for i, col := range []string{"Customer ID", "CamelCase", `a"b:@{}[]| -`} {
				name := fmt.Sprintf("idx_%s_%d", table, i)
				h.exec("CREATE INDEX it_quoted_idx." + name + " ON it_quoted_idx." + table + " (" + quoteSQLName(col) + ")")
				h.expectRows("SELECT indexdef FROM pg_indexes WHERE schemaname='it_quoted_idx' AND indexname='"+name+"'",
					"CREATE INDEX "+quoteSQLName(name)+" ON "+qualifiedName("it_quoted_idx", table)+" USING redis_search ("+quoteSQLName(col)+")")
			}
			h.expectRows("SELECT count(*) FROM information_schema.columns WHERE table_schema='it_quoted_idx' AND table_name='"+table+"' AND is_indexed='YES'", "3")
			h.expectRows(`SELECT "Customer ID" FROM it_quoted_idx.`+table+` WHERE "a""b:@{}[]| -"='Two' AND "CamelCase"=4`, "3")
			h.expectRows(`SELECT count(*) FROM it_quoted_idx.`+table+` WHERE "Customer ID" IS NULL`, "1")
			h.expectRows(`SELECT "Customer ID" FROM it_quoted_idx.`+table+` ORDER BY "Customer ID" NULLS LAST`, "1", "3", "NULL")
			h.expectRows(`SELECT "Customer ID" FROM it_quoted_idx.`+table+` ORDER BY "a""b:@{}[]| -" NULLS LAST`, "1", "3", "NULL")
			h.expectRows(`SELECT DISTINCT "a""b:@{}[]| -" FROM it_quoted_idx.`+table+` ORDER BY "a""b:@{}[]| -" NULLS LAST`, "One", "Two", "NULL")
			h.expectRows(`SELECT count("Customer ID"),sum("Customer ID") FROM it_quoted_idx.`+table, "2|4")
			checkQuotedIndexBindings(t, h, table)
		})
	}
	h.exec(`INSERT INTO it_quoted_idx.source_rows VALUES (5,6,'Three')`)
	h.exec("REFRESH MATERIALIZED VIEW it_quoted_idx.mv")
	checkQuotedIndexBindings(t, h, "mv")
	h.expectRows(`SELECT "Customer ID" FROM it_quoted_idx.mv WHERE "a""b:@{}[]| -"='Three'`, "5")
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE schemaname='it_quoted_idx' AND tablename='mv' AND indexname LIKE 'idx_mv_%'", "3")
	h.exec(`ALTER TABLE it_quoted_idx.source_rows RENAME COLUMN "Customer ID" TO "Renamed ID"`)
	h.exec("ALTER TABLE it_quoted_idx.source_rows RENAME TO renamed_rows")
	h.exec("ALTER MATERIALIZED VIEW it_quoted_idx.mv RENAME TO renamed_mv")
	checkQuotedIndexBindings(t, h, "renamed_rows")
	checkQuotedIndexBindings(t, h, "renamed_mv")
	h.expectRows(`SELECT "Renamed ID" FROM it_quoted_idx.renamed_rows WHERE "Renamed ID"=3`, "3")
	h.expectRows("SELECT tablename FROM pg_indexes WHERE indexname='idx_source_rows_0'", "renamed_rows")
	h.expectRows("SELECT tablename FROM pg_indexes WHERE indexname='idx_mv_0'", "renamed_mv")
	h.exec("DROP INDEX it_quoted_idx.idx_mv_0")
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE indexname='idx_mv_0'", "0")
	h.exec(`ALTER TABLE it_quoted_idx.renamed_rows DROP COLUMN "Renamed ID"`)
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE indexname='idx_source_rows_0'", "0")
	h.exec("DROP SCHEMA it_quoted_idx CASCADE")
	h.expectRows("SELECT count(*) FROM pg_indexes WHERE schemaname='it_quoted_idx'", "0")
	h.exec("CREATE SCHEMA it_quoted_idx")
	h.exec(`CREATE TABLE it_quoted_idx.source_rows ("Customer ID" BIGINT)`)
	h.exec(`CREATE INDEX it_quoted_idx.idx_source_rows_0 ON it_quoted_idx.source_rows ("Customer ID")`)
}

// Verify native Search predicates independently of the SQL HASH fallback,
// including after refresh/rename. The HASH field stays byte-for-byte intact.
func checkQuotedIndexBindings(t *testing.T, h *sqlHarness, table string) {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := st.(*statementImpl).conn.store
	defer st.Close(h.ctx)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	m, err := s.getTable(ctx, "it_quoted_idx", table)
	if err != nil {
		t.Fatal(err)
	}
	predicates := map[string]string{"Customer ID": "[1 1]", `a"b:@{}[]| -`: "{Two}"}
	totals := map[string]int64{}
	bindings := map[string]string{}
	// This fixture only appends rows, so summing segment counts is an
	// independent physical-index check without row-version deduplication.
	for _, segment := range m.snapshotSegments() {
		reply, err := s.searchDo(ctx, segment.Index, "FT.INFO", segment.Index).Result()
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		aliases := map[string]bool{}
		for _, attr := range indexInfoOf(reply).attrs {
			predicate, ok := predicates[attr.identifier]
			if !ok {
				continue
			}
			found++
			if !simpleName(attr.name) || aliases[attr.name] {
				t.Fatalf("unsafe or colliding Search attribute %q", attr.name)
			}
			if prior := bindings[attr.identifier]; prior != "" && prior != attr.name {
				t.Fatalf("Search alias changed between segments: %q / %q", prior, attr.name)
			}
			aliases[attr.name], bindings[attr.identifier] = true, attr.name
			r, err := s.searchDo(ctx, segment.Index, "FT.SEARCH", segment.Index, "@"+attr.name+":"+predicate, "NOCONTENT", "DIALECT", 2).Result()
			if err != nil {
				t.Fatal(err)
			}
			n, err := parseSearchCount(r)
			if err != nil {
				t.Fatal(err)
			}
			totals[attr.identifier] += n
		}
		if found != len(predicates) {
			t.Fatal("quoted HASH fields missing from physical Search schema")
		}
	}
	for field := range predicates {
		if totals[field] != 1 {
			t.Fatalf("%s: native predicate for %q matched %d rows", table, field, totals[field])
		}
	}
}
