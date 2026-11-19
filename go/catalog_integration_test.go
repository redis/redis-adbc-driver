// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestCatalogIsolationAndLifecycle(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	h.exec("CREATE DATABASE analytics")
	h.exec("CREATE TABLE analytics.public.t(id INTEGER)")
	h.exec("INSERT INTO analytics.public.t VALUES(2)")
	h.expectRows("SELECT * FROM redis.public.t", "1")
	h.expectRows("SELECT analytics.public.t.id FROM analytics.public.t", "2")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := st.(*statementImpl).conn
	_ = st.Close(h.ctx)
	if err := c.SetCurrentCatalog(h.ctx, "analytics"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT current_database(), current_schema()", "analytics|public")
	h.expectRows("SELECT * FROM t", "2")
	h.exec("CREATE SCHEMA extra")
	h.exec("CREATE TABLE extra.z(id INTEGER)")
	h.expectRows("SELECT table_catalog,table_schema,table_name FROM information_schema.tables ORDER BY table_name", "analytics|public|t", "analytics|extra|z")
	h.expectRows("SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') ORDER BY nspname", "extra", "public")
	schema := "public"
	if _, err := c.GetTableSchema(h.ctx, nil, &schema, "t"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetCurrentCatalog(h.ctx, "redis"); err != nil {
		t.Fatal(err)
	}
	h.exec("DROP DATABASE analytics CASCADE")
	h.exec("CREATE DATABASE analytics")
	h.expectRows("SELECT * FROM t", "1")
	h.expectError("SELECT * FROM analytics.public.t", "does not exist")
	h.exec("DROP DATABASE analytics CASCADE")
}

func TestCatalogSelectionGenerationAndBoundaries(t *testing.T) {
	h, _, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE DATABASE analytics")
	h.exec("CREATE TABLE analytics.public.t(id INTEGER)")
	h.exec("INSERT INTO analytics.public.t VALUES(2)")
	h.exec("CREATE VIEW analytics.public.v AS SELECT * FROM t")
	h.expectRows("SELECT * FROM analytics.public.v", "2")
	h.expectError("SELECT * FROM analytics.information_schema.tables", "select that catalog")
	h.expectError("DROP SCHEMA "+quoteSQLName(catalogSchema("analytics", "public"))+" CASCADE", "without NUL")
	peer, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close(h.ctx)
	st, err := peer.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := st.(*statementImpl).conn
	_ = st.Close(h.ctx)
	if err := c.SetCurrentCatalog(h.ctx, "analytics"); err != nil {
		t.Fatal(err)
	}
	peerHarness := &sqlHarness{t: t, ctx: h.ctx, conn: peer}
	peerHarness.expectRows("SELECT analytics.information_schema.tables.table_catalog FROM analytics.information_schema.tables ORDER BY table_name", "analytics", "analytics")
	peerHarness.expectError("CREATE SCHEMA redis_live", "reserved")
	peerHarness.expectError("DROP DATABASE analytics CASCADE", "current catalog")
	h.exec("DROP DATABASE analytics CASCADE")
	h.exec("CREATE DATABASE analytics")
	h.exec("CREATE TABLE analytics.public.t(id INTEGER)")
	peerHarness.expectError("INSERT INTO t VALUES(99)", "selected catalog changed")
	schema := "public"
	if _, err := c.GetTableSchema(h.ctx, nil, &schema, "t"); err == nil {
		t.Fatal("stale catalog metadata selection was accepted")
	}
	if err := c.SetCurrentCatalog(h.ctx, "analytics"); err != nil {
		t.Fatal(err)
	}
	peerHarness.exec("INSERT INTO t VALUES(3)")
	h.expectRows("SELECT * FROM analytics.public.t", "3")
	h.exec("DROP DATABASE analytics CASCADE")
}
