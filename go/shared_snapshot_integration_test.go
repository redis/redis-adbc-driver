// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSharedSnapshotMetadataAndSequentialReads(t *testing.T) {
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE orders (id INTEGER, v BIGINT)")
	h.exec("CREATE TABLE items (id INTEGER, v BIGINT)")
	h.exec("INSERT INTO orders VALUES (1,9007199254740993),(1,9007199254740993),(2,NULL)")
	h.exec("INSERT INTO items VALUES (1,10),(1,10),(2,20)")
	h.exec("CREATE VIEW item_view AS SELECT id,v FROM items")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	writer := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	expectedObjects := sharedSnapshotObjects(t, h)
	defer func() {
		for _, r := range expectedObjects {
			r.Release()
		}
	}()
	before, err := h.conn.GetTableSchema(h.ctx, nil, nil, "items")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, "adbc.redis.read_snapshot_action", "acquire"); err != nil {
		t.Fatal(err)
	}
	defer h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, "adbc.redis.read_snapshot_action", "release")
	h.expectRows("SELECT id,v FROM orders ORDER BY id", "1|9007199254740993", "1|9007199254740993", "2|NULL")
	writer.exec("UPDATE orders SET v=42")
	writer.exec("INSERT INTO items VALUES (3,30)")
	writer.exec("DELETE FROM items WHERE id=2")
	writer.exec("UPDATE items SET v=99 WHERE id=1")
	writer.exec("ALTER TABLE items ADD COLUMN newer INTEGER")
	writer.exec("DROP VIEW item_view")
	writer.exec("CREATE VIEW item_view AS SELECT id FROM items")
	schema, err := h.conn.GetTableSchema(h.ctx, nil, nil, "items")
	if err != nil {
		t.Fatal(err)
	}
	if !schema.Equal(before) {
		t.Fatalf("schema changed: %v != %v", schema, before)
	}
	actualObjects := sharedSnapshotObjects(t, h)
	defer func() {
		for _, r := range actualObjects {
			r.Release()
		}
	}()
	if len(actualObjects) != len(expectedObjects) {
		t.Fatal("metadata batch count changed")
	}
	for i := range expectedObjects {
		if !array.RecordEqual(expectedObjects[i], actualObjects[i]) {
			t.Fatal("GetObjects schema, columns or constraints changed")
		}
	}
	h.expectRows("SELECT id,v FROM item_view ORDER BY id", "1|10", "1|10", "2|20")
	writer.exec("TRUNCATE items")
	writer.exec("DROP TABLE items")
	h.expectRows("SELECT id,v FROM items ORDER BY id", "1|10", "1|10", "2|20")
	writer.expectRows("SELECT v FROM orders ORDER BY id", "42", "42", "42")
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, "adbc.redis.read_snapshot_action", "release"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM orders ORDER BY id", "42", "42", "42")
}

func sharedSnapshotConnection(t *testing.T, h *sqlHarness) *connectionImpl {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := st.(*statementImpl).conn
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSharedSnapshotStreamOwnership(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (id INTEGER, v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,1201) AS g(i)")
	c := sharedSnapshotConnection(t, h)
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	parent := c.store.sharedView.pin
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	r, n, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if n != -1 || !r.Next() {
		t.Fatal("expected live stream", n, r.Err())
	}
	count := 0
	check := func() {
		b := r.RecordBatch()
		v := b.Column(1).(*array.Int64)
		for i := 0; i < v.Len(); i++ {
			if v.Value(i) != 9007199254740993 {
				t.Fatal("mixed generation")
			}
			count++
		}
	}
	check()
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release"); err != nil {
		t.Fatal(err)
	}
	if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+parent.token).Val() {
		t.Fatal("parent release retired active reader")
	}
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	writer := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	writer.exec("UPDATE t SET v=42")
	writer.exec("DROP TABLE t")
	for r.Next() {
		check()
	}
	if r.Err() != nil || count != 1201 {
		t.Fatal("incomplete stream", count, r.Err())
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+parent.token).Val() {
		t.Fatal("reader exhaustion leaked parent")
	}
}

func TestSharedSnapshotFailureAndCancellation(t *testing.T) {
	for _, failure := range []string{"lease-loss", "expiry"} {
		t.Run(failure, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			h.exec("CREATE TABLE t (v INTEGER)")
			h.exec("INSERT INTO t SELECT i FROM generate_series(1,101) AS g(i)")
			c := sharedSnapshotConnection(t, h)
			if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
				t.Fatal(err)
			}
			defer c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
			v := c.store.sharedView
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			st.SetOption(h.ctx, OptionIntStreamBatchRows, "7")
			st.SetSqlQuery(h.ctx, "SELECT v FROM t")
			r, _, err := st.ExecuteQuery(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			if !r.Next() {
				t.Fatal(r.Err())
			}
			if failure == "lease-loss" {
				if err := raw.HDel(h.ctx, snapshotCatalogKey, "p:"+v.pin.token).Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				v.cancel()
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("unavailable view emitted buffered rows", r.Err())
			}
			if _, err := c.GetTableSchema(h.ctx, nil, nil, "t"); err == nil {
				t.Fatal("metadata fell back to live view")
			}
			st.SetSqlQuery(h.ctx, "SELECT v FROM t")
			if r, _, err := st.ExecuteQuery(h.ctx); err == nil {
				r.Release()
				t.Fatal("query fell back to live view")
			}
			if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err == nil {
				t.Fatal("reacquired without explicit release")
			}
		})
	}
	t.Run("cancel-and-early-close", func(t *testing.T) {
		h, raw, _ := newSnapshotHarness(t)
		h.exec("CREATE TABLE t (v INTEGER)")
		h.exec("INSERT INTO t SELECT i FROM generate_series(1,101) AS g(i)")
		c := sharedSnapshotConnection(t, h)
		if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
			t.Fatal(err)
		}
		token := c.store.sharedView.pin.token
		for _, cancel := range []bool{false, true} {
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			st.SetOption(h.ctx, OptionIntStreamBatchRows, "7")
			st.SetSqlQuery(h.ctx, "SELECT v FROM t")
			r, _, err := st.ExecuteQuery(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Next() {
				t.Fatal(r.Err())
			}
			if cancel {
				if err := st.(*statementImpl).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			r.Release()
			st.Close(h.ctx)
			if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
				t.Fatal("consumer close released binding")
			}
			h.expectRows("SELECT COUNT(*) FROM t", "101")
		}
		if err := h.conn.Close(h.ctx); err != nil {
			t.Fatal(err)
		}
		if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
			t.Fatal("connection close leaked binding")
		}
	})
}

func TestSharedSnapshotRejectsUnsupportedUse(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t (v INTEGER)")
	h.exec("CREATE SEQUENCE seq")
	c := sharedSnapshotConnection(t, h)
	for _, retention := range []string{"0", "119999", "3600001", "overflow"} {
		if err := c.SetOption(h.ctx, OptionIntReadSnapshotRetention, retention); err == nil {
			t.Fatal("accepted invalid retention", retention)
		}
	}
	if err := c.SetOptionInt(h.ctx, OptionIntReadSnapshotRetention, 120000); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetOptionInt(h.ctx, OptionIntReadSnapshotRetention); err != nil || got != 120000 {
		t.Fatal(got, err)
	}
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "attach"); err == nil {
		t.Fatal("accepted attachment")
	}
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	defer c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
	for _, sql := range []string{"INSERT INTO t VALUES(1)", "TRUNCATE t", "DROP TABLE t", "CREATE TEMP TABLE x(v INTEGER)", "GRANT SELECT ON t TO role1", "SELECT nextval('seq')", "WITH changed AS (INSERT INTO t VALUES(1) RETURNING v) SELECT * FROM changed"} {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSqlQuery(h.ctx, sql); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ExecuteUpdate(h.ctx); err == nil {
			t.Fatal("accepted unsupported SQL", sql)
		}
		st.Close(h.ctx)
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	st.SetSqlQuery(h.ctx, "SELECT * FROM t")
	if _, _, _, err := st.ExecutePartitions(h.ctx); err == nil {
		t.Fatal("accepted partitions")
	}
	if r, err := h.conn.ReadPartition(h.ctx, []byte("{}")); err == nil {
		r.Release()
		t.Fatal("accepted partition attachment")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "0")
}

func sharedSnapshotObjects(t *testing.T, h *sqlHarness) []arrow.RecordBatch {
	t.Helper()
	r, err := h.conn.GetObjects(h.ctx, adbc.ObjectDepthAll, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	var out []arrow.RecordBatch
	for r.Next() {
		b := r.RecordBatch()
		b.Retain()
		out = append(out, b)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return out
}

func TestSharedSnapshotQuota(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	c := sharedSnapshotConnection(t, h)
	var tokens []string
	defer func() {
		for _, token := range tokens {
			c.store.snapshotControl(h.ctx, "release", token, "", "")
		}
	}()
	for i := 0; i < 128; i++ {
		token := snapshotID()
		if err := c.store.snapshotControl(h.ctx, "acquire", token, "", "").Err(); err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err == nil || !strings.Contains(err.Error(), "reader limit") {
		t.Fatal("quota did not refuse binding", err)
	}
	if c.store.sharedView != nil {
		t.Fatal("failed acquisition bound a view")
	}
	if n := raw.HLen(h.ctx, snapshotCatalogKey).Val(); n < 128 {
		t.Fatal("lost existing pins")
	}
}

// A real process exits without Close, releasing neither its local owner nor
// its server lease. The parent waits out the unmodified two-minute lease.
func TestSharedSnapshotProcessHelper(t *testing.T) {
	result := os.Getenv("REDIS_SHARED_SNAPSHOT_PROCESS_RESULT")
	if result == "" {
		t.Skip("separate-process helper")
	}
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h := &sqlHarness{t: t, ctx: ctx, conn: conn}
	c := sharedSnapshotConnection(t, h)
	if err := c.SetOption(ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	pin := c.store.sharedView.pin
	raw, _ := json.Marshal(struct{ Token, Root string }{pin.token, pin.root})
	if err := os.WriteFile(result, raw, 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestSharedSnapshotProcessDeath(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	writer := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	c := sharedSnapshotConnection(t, h)
	if err := c.SetOptionInt(h.ctx, OptionIntReadSnapshotRetention, 120000); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	defer c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
	result := filepath.Join(t.TempDir(), "pin.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedSnapshotProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "REDIS_SHARED_SNAPSHOT_PROCESS_RESULT="+result)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, output)
	}
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var pin struct{ Token, Root string }
	if err := json.Unmarshal(data, &pin); err != nil {
		t.Fatal(err)
	}
	if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.Token).Val() {
		t.Fatal("helper did not leave live pin")
	}
	writer.exec("UPDATE t SET v=2")
	deadline := time.Now().Add(snapshotLease + 20*time.Second)
	maintenance := snapshotTestStore(t, raw)
	for time.Now().Before(deadline) {
		maintenance.reapSnapshot(h.ctx)
		if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+pin.Token).Val() && raw.Exists(h.ctx, snapshotMetadataMap(pin.Root)).Val() == 0 {
			if _, err := c.GetTableSchema(h.ctx, nil, nil, "t"); err == nil {
				t.Fatal("naturally expired view fell back to current metadata")
			}
			if _, err := c.GetOption(h.ctx, OptionStringReadSnapshotStatus); err == nil {
				t.Fatal("naturally expired view reported active")
			}
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("dead process lease or retired generation survived collection deadline")
}

func TestSharedSnapshotSurvivesCompactionAndOptionContext(t *testing.T) {
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	h.exec("INSERT INTO t VALUES(10),(10),(NULL)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	writer := &sqlHarness{t: t, ctx: h.ctx, conn: other, afterExec: h.afterExec}
	c := sharedSnapshotConnection(t, h)
	ctx, cancel := context.WithCancel(h.ctx)
	if err := c.SetOption(ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
		t.Fatal(err)
	}
	cancel() // The successfully acquired binding belongs to the connection.
	defer c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
	status, err := c.GetOption(h.ctx, OptionStringReadSnapshotStatus)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		writer.exec("UPDATE t SET v=v+1")
	}
	h.expectRows("SELECT v FROM t ORDER BY v NULLS LAST", "10", "10", "NULL")
	writer.expectRows("SELECT v FROM t ORDER BY v NULLS LAST", "19", "19", "NULL")
	after, err := c.GetOption(h.ctx, OptionStringReadSnapshotStatus)
	if err != nil || after != status {
		t.Fatal("generation identity changed", err)
	}
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release"); err != nil {
		t.Fatal(err)
	}
	h.exec("CREATE TEMP TABLE tmp(v INTEGER)")
	if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err == nil {
		t.Fatal("temporary objects accepted")
	}
}

// Admission can wait on compaction. Readers of binding state must still make
// progress while a server acquire is outstanding, in either acquisition path.
type sharedSnapshotAdmissionGate struct {
	snapshotHook
	started, unblock chan struct{}
	once             sync.Once
}

func (g *sharedSnapshotAdmissionGate) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		if len(a) > 3 && snapshotObservedControlName(cmd) == "eval" && a[1] == snapshotControl {
			keys, _ := strconv.Atoi(fmt.Sprint(a[2]))
			if 3+keys < len(a) && a[3+keys] == "acquire" {
				g.once.Do(func() {
					close(g.started)
					select {
					case <-g.unblock:
					case <-ctx.Done():
					}
				})
			}
		}
		return next(ctx, cmd)
	}
}
func TestSharedSnapshotAdmissionDoesNotHoldBindingMutex(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(strconv.FormatBool(shared), func(t *testing.T) {
			h, _, _ := newSnapshotHarness(t)
			c := sharedSnapshotConnection(t, h)
			gate := &sharedSnapshotAdmissionGate{started: make(chan struct{}), unblock: make(chan struct{})}
			c.store.client.AddHook(gate)
			var unblock sync.Once
			defer unblock.Do(func() { close(gate.unblock) })
			done := make(chan error, 1)
			go func() {
				if shared {
					done <- c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire")
					return
				}
				_, pin, err := c.store.acquireSnapshot(h.ctx)
				pin.release()
				done <- err
			}()
			select {
			case <-gate.started:
			case <-time.After(5 * time.Second):
				t.Fatal("acquisition did not reach server")
			}
			state := make(chan bool, 1)
			go func() { state <- c.store.hasSharedReadView() }()
			select {
			case <-state:
			case <-time.After(time.Second):
				t.Error("admission holds binding mutex and can deadlock compaction")
			}
			unblock.Do(func() { close(gate.unblock) })
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if shared {
				if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
