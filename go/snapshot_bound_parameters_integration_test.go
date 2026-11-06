// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// Installed on an idle no-worker maintenance client. Publish a genuine ALTER
// after the first read pin is acquired, before the second parameter execution.
type boundSchemaPublication struct {
	goredis.UniversalClient
	once    sync.Once
	publish func()
}

func (o *boundSchemaPublication) Process(ctx context.Context, cmd goredis.Cmder) error {
	err := o.UniversalClient.Process(ctx, cmd)
	a := snapshotObservedControlArgs(cmd)
	if err == nil && len(a) > 3 && strings.EqualFold(snapshotObservedControlName(cmd), "eval") {
		n, _ := strconv.Atoi(fmt.Sprint(a[2]))
		if len(a) > 3+n && a[3+n] == "acquire" {
			o.once.Do(o.publish)
		}
	}
	return err
}

func TestSnapshotBoundParametersRejectDeclaredSchemaChange(t *testing.T) {
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE bound_schema(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO bound_schema VALUES(1,10),(2,20)")
	conn, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(h.ctx)
	st, err := conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, "SELECT v FROM bound_schema WHERE id=$1"); err != nil {
		t.Fatal(err)
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int32}}, nil))
	defer b.Release()
	b.Field(0).(*array.Int32Builder).AppendValues([]int32{1, 2}, nil)
	record := b.NewRecordBatch()
	defer record.Release()
	if err := st.Bind(h.ctx, record); err != nil {
		t.Fatal(err)
	}
	s := st.(*statementImpl).conn.store
	if s.compactor != nil || len(s.activeExecutions()) != 0 {
		t.Fatal("schema observer requires an idle no-worker fixture")
	}
	s.client = &boundSchemaPublication{UniversalClient: s.client, publish: func() {
		h.exec("ALTER TABLE bound_schema ALTER COLUMN v TYPE TEXT")
	}}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if rdr != nil {
		rdr.Release()
	}
	if !isStatus(err, adbc.StatusInvalidState) || !strings.Contains(err.Error(), "result schema changed between parameter executions") {
		t.Fatalf("actual declared schema change was accepted: %v", err)
	}
	h.expectRows("SELECT v FROM bound_schema ORDER BY id", "10", "20")
}
