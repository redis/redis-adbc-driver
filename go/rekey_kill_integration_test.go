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

// Integration tests for re-keys abandoned by their connection (rekey.go):
// a renaming process that is killed, connections that open and close
// straight away, and connections that wait for another one's rollback.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adbc-drivers/redis/go/internal/testenv"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// The test binary is also the renaming process that TestSQLRekeyKilled
// kills.
func TestMain(m *testing.M) {
	if err := testenv.SetupRoots(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"REDIS_URI", "REDIS_ADMIN_URI", "REDIS_FLEX_URI"} {
		if err := testenv.ValidateURI(os.Getenv(name)); err != nil {
			fmt.Fprintln(os.Stderr, name+":", err)
			os.Exit(1)
		}
	}
	if args := os.Getenv("ADBC_REDIS_REKEY_CHILD"); args != "" {
		os.Exit(rekeyChild(args))
	}
	os.Exit(m.Run())
}

// The child blocks at an observed native boundary until the parent kills it.
// No clean Close/abort path runs after that boundary.
type rekeyChildBoundary struct {
	ID, Token, Root string
	Data            []string
}

func rekeyChild(args string) int {
	from, to, _ := strings.Cut(args, " ")
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringRenameRekey: "true"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	conn, err := db.Open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	stmt, err := conn.NewStatement(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := stmt.SetSqlQuery(ctx, "ALTER TABLE "+from+" RENAME TO "+to); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	impl := stmt.(*statementImpl)
	execCtx, transport := impl.conn.executionContext(ctx, &impl.streams, &impl.conn.streams)
	ready := make(chan context.Context, 1)
	if os.Getenv("ADBC_REDIS_REKEY_PHASE") == "committed" {
		impl.conn.store.client.AddHook(&rekeyCommittedReply{ready: ready, resume: make(chan struct{})})
	} else {
		prefix := strings.TrimSuffix(snapshotRows("fixture", defaultSchema), "fixture:")
		transport.client.AddHook(&snapshotAlterReadBarrier{prefix: prefix, ready: ready, resume: make(chan struct{})})
	}
	go func() {
		scope := snapshotOf(<-ready)
		if scope == nil {
			fmt.Fprintln(os.Stderr, "missing child snapshot")
			os.Exit(2)
		}
		boundary := rekeyChildBoundary{ID: scope.id, Token: scope.pin.token, Root: scope.pin.root}
		scope.mu.Lock()
		for data := range scope.owned {
			boundary.Data = append(boundary.Data, data)
		}
		scope.mu.Unlock()
		if err := json.NewEncoder(os.Stdout).Encode(boundary); err != nil {
			os.Exit(2)
		}
	}()
	_, err = stmt.ExecuteUpdate(execCtx)
	fmt.Fprintln(os.Stderr, "the rename ended before the process was killed:", err)
	return 3
}

// dbSize counts the keys of the database (on every master of a cluster).
func (h *sqlHarness) dbSize(raw goredis.UniversalClient) int64 {
	h.t.Helper()
	if cc, ok := raw.(*goredis.ClusterClient); ok {
		var total atomic.Int64 // ForEachMaster runs fn concurrently
		err := cc.ForEachMaster(h.ctx, func(ctx context.Context, n *goredis.Client) error {
			v, err := n.DBSize(ctx).Result()
			total.Add(v)
			return err
		})
		if err != nil {
			h.t.Fatal(err)
		}
		return total.Load()
	}
	n, err := raw.DBSize(h.ctx).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	return n
}

// rekeyRecord returns the re-key recorded for a prefix (nil if none).
func (h *sqlHarness) rekeyRecord(raw goredis.UniversalClient, prefix string) *rekeyJob {
	h.t.Helper()
	job, err := (&store{client: raw}).readRekey(h.ctx, prefix)
	if err != nil {
		h.t.Fatal(err)
	}
	return job
}

// aliveKeys lists the re-key lease keys.
func (h *sqlHarness) aliveKeys(raw goredis.UniversalClient) []string {
	h.t.Helper()
	node := raw
	if cc, ok := raw.(*goredis.ClusterClient); ok { // the {meta} slot's node
		n, err := cc.MasterForKey(h.ctx, metaPrefix)
		if err != nil {
			h.t.Fatal(err)
		}
		node = n
	}
	keys, err := node.Keys(h.ctx, rekeyAliveKey("*")).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	return keys
}

// expectErrorExact runs a statement and checks its whole error message.
func (h *sqlHarness) expectErrorExact(sql, want string) {
	h.t.Helper()
	_, err := tryExec(h.ctx, h.conn, sql)
	if err == nil || err.Error() != want {
		h.t.Errorf("%s:\n got: %v\nwant: %s", sql, err, want)
	}
}

// movingMsg is movingErr's message for a table in the public schema.
func movingMsg(table string) string {
	return fmt.Sprintf(`I/O: [redis] table "public".%q is being renamed and its rows moved to new keys (adbc.redis.rename_rekey); `+
		`try again when that has finished. If the connection renaming it has gone away, the next connection to open, `+
		`or the next statement that changes the table, rolls the rename back once its lease (%s) has expired`, table, rekeyTTL)
}

// Issue #87: SIGKILL before publication rolls private output back, while
// SIGKILL after publication preserves the committed generation. Owner control
// expiry is advanced only after Wait confirms process death; natural lease
// duration is not measured by this test.
func TestSQLRekeyKilled(t *testing.T) {
	for _, mode := range []string{"private_open", "private_write", "committed"} {
		t.Run(mode, func(t *testing.T) {
			const from, to, n = "it_rk_kill", "it_rk_kill_backup", 50000
			h := newSQLHarness(t)
			h.dropTables(from, to)
			raw := h.rawClient()
			ids := make([]int64, n)
			for i := range ids {
				ids[i] = int64(i + 1)
			}
			if err := h.ingest(from, adbc.OptionValueIngestModeCreate, false, ids...); err != nil {
				t.Fatal(err)
			}
			original := h.inspectTable(raw, defaultSchema, from)
			before := h.rawMeta(raw, defaultSchema, from)
			long := newSQLHarness(t)
			source, oldCtx, oldPin := h.inspectionSnapshot(raw)
			defer oldPin.release()
			// Stderr stays in the private test directory; stdout contains only the
			// boundary record, read through a pipe without concurrent buffer access.
			stderr, err := os.CreateTemp(t.TempDir(), "child-stderr-")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			phase := "private"
			if mode == "committed" {
				phase = "committed"
			}
			cmd.Env = append(os.Environ(), "ADBC_REDIS_REKEY_CHILD="+from+" "+to, "ADBC_REDIS_REKEY_PHASE="+phase)
			cmd.Stderr = stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			type observed struct {
				boundary rekeyChildBoundary
				err      error
			}
			ready := make(chan observed, 1)
			go func() {
				var v rekeyChildBoundary
				scanner := bufio.NewScanner(stdout)
				if !scanner.Scan() {
					ready <- observed{err: fmt.Errorf("child ended before boundary: %v", scanner.Err())}
					return
				}
				err := json.Unmarshal(scanner.Bytes(), &v)
				ready <- observed{v, err}
			}()
			var boundary rekeyChildBoundary
			select {
			case got := <-ready:
				if got.err != nil {
					t.Fatal(got.err)
				}
				boundary = got.boundary
			case <-time.After(45 * time.Second):
				t.Fatal("child never reached native crash boundary")
			}
			if boundary.ID == "" || boundary.Token == "" || boundary.Root == "" || len(boundary.Data) != 1 {
				t.Fatal("incomplete child boundary")
			}
			data := boundary.Data[0]
			prefix := snapshotRows(data, defaultSchema)
			payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+boundary.ID).Bytes()
			var work struct {
				State   string `json:"state"`
				Expires int64  `json:"expires"`
				Batch   struct {
					Rows int `json:"rows"`
				} `json:"row_batch"`
			}
			if err != nil || json.Unmarshal(payload, &work) != nil {
				t.Fatal("missing durable child work", err)
			}
			if mode != "committed" {
				now, err := raw.Time(h.ctx).Result()
				if err != nil || work.State != "staging" || work.Expires <= now.UnixMilli() || work.Batch.Rows != snapshotDeltaPage {
					t.Fatal("child did not acknowledge a live private batch", work.State, work.Batch.Rows, err)
				}
				if rows := raw.ZCard(h.ctx, snapshotMembers(data)).Val(); rows != snapshotDeltaPage {
					t.Fatal("wrong private row coverage", rows)
				}
				if h.rawMeta(raw, defaultSchema, from) != before {
					t.Fatal("private child changed published source metadata")
				}
				if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
					t.Fatal("private target exposed", err)
				}
			} else {
				if work.State != "committed" || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() == boundary.Root {
					t.Fatal("child did not commit its root")
				}
				target := h.inspectTable(raw, defaultSchema, to)
				if target.prefix() != prefix || target.prefix() == original.prefix() {
					t.Fatal("committed child identity differs")
				}
				expectRekeyExactRows(t, h, to, n)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal("SIGKILL", err)
			}
			err = cmd.Wait()
			waited = true
			if err == nil || cmd.ProcessState.Success() {
				t.Fatal("child exited successfully rather than being killed")
			}
			if !raw.HExists(h.ctx, snapshotCatalogKey, "w:"+boundary.ID).Val() || !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+boundary.Token).Val() {
				t.Fatal("child ran cleanup or lost ownership before the crash")
			}
			// No heartbeat can race expiry after the OS has reaped the owner.
			if mode != "committed" {
				expireRekeyControl(t, h, raw, "w:"+boundary.ID)
			}
			expireRekeyControl(t, h, raw, "p:"+boundary.Token)
			if mode == "private_open" {
				// Opening then immediately closing still performs ownership recovery.
				db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI")})
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close(h.ctx)
				short, err := db.Open(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := short.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
			} else if mode == "private_write" {
				long.exec("INSERT INTO " + from + " VALUES (50001)")
			} else {
				reopened := newSQLHarness(t)
				expectRekeyExactRows(t, reopened, to, n)
			}
			// Independent old readers survive both rollback and commit recovery.
			rows := snapshotExecuteTest(t, source, oldCtx, "SELECT id FROM "+from+" ORDER BY id").rows
			if len(rows) != n {
				t.Fatal("crash recovery changed pinned input count", len(rows))
			}
			for i, row := range rows {
				if len(row) != 1 || row[0].Null || row[0].T != typeInt64 || row[0].I != int64(i+1) {
					t.Fatal("crash recovery changed pinned input", i)
				}
			}
			oldPin.release()
			if mode != "committed" {
				h.expectSnapshotReleased(raw, prefix, snapshotIndexPrefix(data)+"working")
				count := n
				if mode == "private_write" {
					count++
				}
				expectRekeyExactRows(t, h, from, count)
				if err := long.setConnOption(OptionStringRenameRekey, "true"); err != nil {
					t.Fatal(err)
				}
				long.exec("ALTER TABLE " + from + " RENAME TO " + to)
				target := h.inspectTable(raw, defaultSchema, to)
				if target.prefix() == prefix || target.prefix() == original.prefix() {
					t.Fatal("fresh request reused crashed output")
				}
				expectRekeyExactRows(t, long, to, count)
			}
			if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, from); !isStatus(err, adbc.StatusNotFound) {
				t.Fatal("old binding survived committed rename", err)
			}
			for _, seg := range original.snapshotSegments() {
				h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+boundary.Token).Val() {
				t.Fatal("dead child retained reader control")
			}
			long.exec("INSERT INTO " + to + " VALUES (50002)")
			h.expectRows("SELECT id FROM "+to+" WHERE id=50002", "50002")
		})
	}
}

// abandon starts a re-key, copies the rows, and stops as a killed process
// does (the lease expires: here its key is deleted).
func (h *sqlHarness) abandon(st *store, raw goredis.UniversalClient, from, to string) *rekeyJob {
	h.t.Helper()
	job, start, err := st.beginRekey(h.ctx, "public", from, to)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := st.moveRows(h.ctx, job, start, time.Now()); err != nil {
		h.t.Fatal(err)
	}
	job.lease.release()
	if err := raw.Del(h.ctx, rekeyAliveKey(job.Owner)).Err(); err != nil {
		h.t.Fatal(err)
	}
	return job
}

// While one connection rolls back an abandoned move, connections that open
// wait for it before they are open, and statements that would change the
// table wait before they go on.
func TestSQLRekeyTakeoverWait(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_tw", "it_rk_tw2")
	raw := h.rawClient()
	st := &store{client: raw}
	h.exec(`CREATE TABLE it_rk_tw (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_tw VALUES (1), (2)`)
	job := h.abandon(st, raw, "it_rk_tw", "it_rk_tw2")

	// Another connection takes the job over, and is slow to roll it back.
	taken, err := st.takeOver(h.ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if rec := h.rekeyRecord(raw, job.OldPrefix); rec == nil || !rec.Recovering || rec.Owner != taken.Owner {
		t.Fatalf("record after the takeover: %+v", rec)
	}
	type opened struct {
		conn adbc.ConnectionWithContext
		err  error
		at   time.Time
	}
	openedCh := make(chan opened, 1)
	go func() {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI")})
		if err != nil {
			openedCh <- opened{err: err}
			return
		}
		conn, err := db.Open(h.ctx)
		openedCh <- opened{conn, err, time.Now()}
	}()
	insertCh := make(chan error, 1)
	var insertedAt time.Time
	go func() {
		_, err := tryExec(h.ctx, h.conn, `INSERT INTO it_rk_tw VALUES (3)`)
		insertedAt = time.Now()
		insertCh <- err
	}()
	time.Sleep(500 * time.Millisecond)
	rolledBack := time.Now()
	if err := st.rollbackRekey(h.ctx, taken); err != nil {
		t.Fatal(err)
	}
	taken.lease.release()

	o := <-openedCh
	if o.err != nil {
		t.Fatal(o.err)
	}
	defer o.conn.Close(h.ctx)
	if o.at.Before(rolledBack) {
		t.Error("a connection opened while another one was rolling back an abandoned move")
	}
	if err := <-insertCh; err != nil {
		t.Errorf("the waiting statement: %v", err)
	}
	if insertedAt.Before(rolledBack) {
		t.Error("a statement went on while another connection was rolling the move back")
	}
	if _, err := tryExec(h.ctx, o.conn, `INSERT INTO it_rk_tw VALUES (4)`); err != nil {
		t.Errorf("a write on the new connection: %v", err)
	}
	h.expectReleased(raw, job.KeyPrefix, job.IndexName)
	h.expectRows(`SELECT id FROM it_rk_tw ORDER BY id`, "1", "2", "3", "4")
}

// RENAME TO with rename_rekey of a table whose key prefix still has an
// earlier move recorded: a table dropped while its rows were being moved,
// then created again with the same name by an earlier version of the
// driver (v0.0.7), which gave it the dropped table's prefix. This version
// never hands a prefix out again (claimNames), so the test sets that up.
func TestSQLRekeyEarlierRename(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_er", "it_rk_er2", "it_rk_er3")
	raw := h.rawClient()
	st := &store{client: raw}
	h.exec(`CREATE TABLE it_rk_er (id INTEGER)`)
	n := h.namesN(raw, "public", "it_rk_er", "it_rk_er")
	h.exec(`INSERT INTO it_rk_er VALUES (1), (2)`)
	job, start, err := st.beginRekey(h.ctx, "public", "it_rk_er", "it_rk_er2")
	if err != nil {
		t.Fatal(err)
	}
	defer job.lease.release()
	if err := st.moveRows(h.ctx, job, start, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.exec(`DROP TABLE it_rk_er`)
	// v0.0.7 gave the new table the released prefix, with the generation
	// that DROP TABLE counted: let CREATE TABLE take the prefix, then put
	// the count back and record it in the table's metadata.
	gen, err := raw.HGet(h.ctx, releasedKey, job.OldPrefix).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HDel(h.ctx, releasedKey, job.OldPrefix).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, namesNextKey, rowPrefix("public", "it_rk_er"), n-1).Err(); err != nil {
		t.Fatal(err)
	}
	h.exec(`CREATE TABLE it_rk_er (id INTEGER)`)
	if err := raw.HSet(h.ctx, releasedKey, job.OldPrefix, gen).Err(); err != nil {
		t.Fatal(err)
	}
	if err := st.updateTable(h.ctx, "public", "it_rk_er", func(m *tableMeta) error { m.PrefixGen = gen; return nil }, nil); err != nil {
		t.Fatal(err)
	}
	h.exec(`INSERT INTO it_rk_er VALUES (10)`)
	if prefix, index := h.tableNames(raw, "public", "it_rk_er"); prefix != job.OldPrefix || index != job.OldIndex {
		t.Fatalf("the new table has names %q, %q, not %q, %q", prefix, index, job.OldPrefix, job.OldIndex)
	}
	rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	rk.expectErrorExact(`ALTER TABLE it_rk_er RENAME TO it_rk_er3`,
		`I/O: [redis] an earlier rename of table "public"."it_rk_er" is still moving its rows (adbc.redis.rename_rekey); `+
			`try again when it has finished. If the connection doing it has gone away, the next connection to open, `+
			`or the next such rename, rolls it back once its lease (30s) has expired`)

	// Its connection goes away: the next rename rolls it back first.
	job.lease.release()
	if err := raw.Del(h.ctx, rekeyAliveKey(job.Owner)).Err(); err != nil {
		t.Fatal(err)
	}
	rk.exec(`ALTER TABLE it_rk_er RENAME TO it_rk_er3`)
	h.expectReleased(raw, job.KeyPrefix, job.IndexName)
	h.expectReleased(raw, job.OldPrefix, job.OldIndex)
	if h.rekeyPending(raw, job.OldPrefix) {
		t.Error("the earlier move is still recorded")
	}
	h.expectRows(`SELECT id FROM it_rk_er3`, "10")
	h.namesN(raw, "public", "it_rk_er3", "it_rk_er3")
}

// The messages of a statement that runs into a move whose connection went
// away, when it can't go on.
func TestSQLRekeyAbandonedMessages(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_am", "it_rk_am2")
	raw := h.rawClient()
	st := &store{client: raw}
	h.exec(`CREATE TABLE it_rk_am (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_am VALUES (1)`)

	// Another connection is rolling it back, and takes longer than the
	// statement waits (rekeyTTL, shortened here).
	defer func(was time.Duration) { rekeyTTL = was }(rekeyTTL)
	rekeyTTL = 2 * time.Second
	job := h.abandon(st, raw, "it_rk_am", "it_rk_am2")
	taken, err := st.takeOver(h.ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	h.expectErrorExact(`INSERT INTO it_rk_am VALUES (2)`, `I/O: [redis] table "public"."it_rk_am" was being renamed `+
		`(adbc.redis.rename_rekey) by a connection that has gone away, and another connection is still rolling the rename back; try again in a moment`)
	if err := st.rollbackRekey(h.ctx, taken); err != nil {
		t.Fatal(err)
	}
	taken.lease.release()

	// The table changed otherwise after the rollback (a column was added):
	// the statement can't go on with what it read.
	job = h.abandon(st, raw, "it_rk_am", "it_rk_am2")
	meta, err := st.getTable(h.ctx, "public", "it_rk_am")
	if err != nil {
		t.Fatal(err)
	}
	h2 := newSQLHarness(t) // rolls it back
	h2.exec(`ALTER TABLE it_rk_am ADD COLUMN v INTEGER`)
	err = st.checkWritable(h.ctx, meta)
	want := `I/O: [redis] table "public"."it_rk_am" was being renamed (adbc.redis.rename_rekey) when this statement started, ` +
		`and that has ended since; try again`
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}

	// The move had switched: it is finished, and the table has its new name.
	job, start, err := st.beginRekey(h.ctx, "public", "it_rk_am", "it_rk_am2")
	if err != nil {
		t.Fatal(err)
	}
	meta, err = st.getTable(h.ctx, "public", "it_rk_am")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.moveRows(h.ctx, job, start, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.switchRekey(h.ctx, job, start); err != nil {
		t.Fatal(err)
	}
	job.lease.release()
	if err := raw.Del(h.ctx, rekeyAliveKey(job.Owner)).Err(); err != nil {
		t.Fatal(err)
	}
	err = st.checkWritable(h.ctx, meta)
	want = `I/O: [redis] table "public"."it_rk_am" was being renamed to "it_rk_am2" (adbc.redis.rename_rekey) by a connection ` +
		`that has gone away; that rename has now been completed`
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
	h.expectRows(`SELECT id, v FROM it_rk_am2`, "1|NULL")
	h.expectReleased(raw, job.OldPrefix, job.OldIndex)
	raw2, _ := json.Marshal(h.aliveKeys(raw))
	if string(raw2) != "[]" {
		t.Errorf("lease keys left: %s", raw2)
	}
}

// DROP TABLE of a table whose move was abandoned rolls the move back first,
// so that the move (its record, copies and new names) doesn't outlive the
// table.
func TestSQLRekeyDropAbandoned(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_drop", "it_rk_drop2")
	raw := h.rawClient()
	st := &store{client: raw}
	h.exec(`CREATE TABLE it_rk_drop (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_drop VALUES (1), (2)`)
	job := h.abandon(st, raw, "it_rk_drop", "it_rk_drop2")
	h.exec(`DROP TABLE it_rk_drop`)
	if h.rekeyPending(raw, job.OldPrefix) {
		t.Error("the abandoned move outlived the table")
	}
	h.expectReleased(raw, job.KeyPrefix, job.IndexName)
	h.expectReleased(raw, job.OldPrefix, job.OldIndex)
	h.exec(`CREATE TABLE it_rk_drop (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_drop VALUES (3)`)
	rk := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	rk.exec(`ALTER TABLE it_rk_drop RENAME TO it_rk_drop2`)
	h.expectRows(`SELECT id FROM it_rk_drop2`, "3")
}
