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

// Native rekey owner death, concurrent reclamation, name reuse and diagnostics.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adbc-drivers/redis/go/internal/testenv"

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
		prefix := os.Getenv("ADBC_REDIS_REKEY_ACCESS") + "~" + snapshotSchemaPart(defaultSchema) + snapshotRowMarker
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

// expectErrorExact runs a statement and checks its whole error message.
func (h *sqlHarness) expectErrorExact(sql, want string) {
	h.t.Helper()
	_, err := tryExec(h.ctx, h.conn, sql)
	if err == nil || err.Error() != want {
		h.t.Errorf("%s:\n got: %v\nwant: %s", sql, err, want)
	}
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
			cmd.Env = append(os.Environ(), "ADBC_REDIS_REKEY_CHILD="+from+" "+to, "ADBC_REDIS_REKEY_PHASE="+phase, "ADBC_REDIS_REKEY_ACCESS="+original.SnapshotAccessID)
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
			prefix := snapshotRows(data, defaultSchema, original.SnapshotAccessID)
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

// Pause one collector immediately before its first real bounded row deletion.
// Other collectors can complete the same ownership-checked work; a stale batch
// must then be harmless, including after the logical table name is reused.
type rekeyGCBarrier struct {
	snapshotHook
	prefix string
	paused atomic.Bool
	ready  chan struct{}
	resume chan struct{}
}

func (b *rekeyGCBarrier) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if len(args) > 6 && strings.EqualFold(fmt.Sprint(args[0]), "EVAL") && args[1] == snapshotGCRowBatchLua &&
			strings.HasPrefix(fmt.Sprint(args[6]), b.prefix) && b.paused.CompareAndSwap(false, true) {
			close(b.ready)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-b.resume:
			}
		}
		return next(ctx, cmd)
	}
}

// A second collector can take over zero-reference output while the first is
// paused. Published-table writes and a fresh binding do not wait for that stale
// collector, whose resumed deletion cannot touch current data.
func TestSQLRekeyTakeoverWait(t *testing.T) {
	const from, to = "it_rk_tw", "it_rk_tw2"
	h := newSQLHarness(t)
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " SELECT i FROM generate_series(1,600) AS g(i)")
	raw := h.rawClient()
	abandoned := stageRekeySnapshot(t, h, raw, from, to)
	prefix := abandoned.target.prefix()
	barrier := &rekeyGCBarrier{prefix: prefix, ready: make(chan struct{}), resume: make(chan struct{})}
	collector := &store{client: h.rawClient(barrier), snapshots: true}
	abandoned.pin.release()
	expireRekeyControl(t, h, raw, "w:"+abandoned.id)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ {
			result, err := collector.reapSnapshotStep(ctx)
			if err != nil {
				done <- err
				return
			}
			if result.prefix == prefix {
				done <- nil
				return
			}
		}
		done <- fmt.Errorf("collector did not reach abandoned output")
	}()
	var once sync.Once
	resume := func() { once.Do(func() { close(barrier.resume) }) }
	joined := false
	t.Cleanup(func() {
		resume()
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("collector did not stop")
			}
		}
	})
	select {
	case <-barrier.ready:
	case err := <-done:
		joined = true
		t.Fatal("collector did not pause", err)
	case <-ctx.Done():
		t.Fatal("collector never reached bounded deletion")
	}
	if n := raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(prefix))).Val(); n != 600 {
		t.Fatal("collector mutated before barrier", n)
	}
	// Opening another connection and its recovery must finish while the first
	// collector is still held. This uses bounded context instead of sleep timing.
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	openCtx, stopOpen := context.WithTimeout(h.ctx, 5*time.Second)
	conn, err := db.Open(openCtx)
	stopOpen()
	if err != nil {
		t.Fatal("second recovery waited for paused collector", err)
	}
	defer conn.Close(h.ctx)
	expectStagedRekeyReleased(t, h, raw, abandoned)
	h.exec("INSERT INTO " + from + " VALUES (601)")
	if err := h.setConnOption(OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE " + from + " RENAME TO " + to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (900)")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	resume()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal("stale collector failed", err)
		}
	case <-ctx.Done():
		t.Fatal("stale collector did not finish")
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("collector changed current catalog")
	}
	expectRekeyExactRows(t, h, to, 601)
	h.expectRows("SELECT id FROM "+from, "900")
	expectStagedRekeyReleased(t, h, raw, abandoned)
	assertSnapshotDataReferences(t, h, &store{client: raw, snapshots: true})
}

// A private rewrite of a dropped incarnation cannot publish over a recreated
// name or a later rename. Prefixes stay unique without mutable name registries
// or artificial metadata from an earlier storage format.
func TestSQLRekeyEarlierRename(t *testing.T) {
	const from, to, third = "it_rk_er", "it_rk_er2", "it_rk_er3"
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables(from, to, third)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (1),(2)")
	raw := h.rawClient()
	earlier := stageRekeySnapshot(t, h, raw, from, to)
	h.exec("DROP TABLE " + from)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (10)")
	replacement := h.inspectTable(raw, defaultSchema, from)
	if replacement.prefix() == earlier.source.prefix() || replacement.prefix() == earlier.target.prefix() {
		t.Fatal("name reuse shared earlier storage")
	}
	h.exec("ALTER TABLE " + from + " RENAME TO " + third)
	current := h.inspectTable(raw, defaultSchema, third)
	if current.prefix() == replacement.prefix() || current.prefix() == earlier.source.prefix() || current.prefix() == earlier.target.prefix() {
		t.Fatal("fresh rekey reused an earlier identity")
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := earlier.store.publishSnapshot(earlier.ctx, earlier.pin, earlier.id); sqlState(err) != "40001" {
		t.Fatal("earlier incarnation published", err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("earlier rekey changed root")
	}
	rows := snapshotExecuteTest(t, earlier.store, earlier.base, "SELECT id FROM "+from+" ORDER BY id").rows
	if len(rows) != 2 || rows[0][0].I != 1 || rows[1][0].I != 2 {
		t.Fatal("earlier pinned input changed", rows)
	}
	earlier.store.abortSnapshot(earlier.pin, earlier.id)
	earlier.pin.release()
	expectStagedRekeyReleased(t, h, raw, earlier)
	for _, seg := range earlier.source.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	h.expectSnapshotReleased(raw, replacement.prefix(), replacement.index())
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("earlier target leaked", err)
	}
	h.expectRows("SELECT id FROM "+third, "10")
	h.exec("INSERT INTO " + third + " VALUES (11)")
	h.expectRows("SELECT id FROM "+third+" ORDER BY id", "10", "11")
}

// Native diagnostics distinguish expired ownership from serialization refusal.
// Recovery never redirects a stale statement to an altered or renamed binding.
func TestSQLRekeyAbandonedMessages(t *testing.T) {
	const from, to = "it_rk_am", "it_rk_am2"
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (1)")
	raw := h.rawClient()
	abandoned := stageRekeySnapshot(t, h, raw, from, to)
	abandoned.pin.release()
	expireRekeyControl(t, h, raw, "w:"+abandoned.id)
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	err := abandoned.store.publishSnapshot(abandoned.ctx, abandoned.pin, abandoned.id)
	if !isStatus(err, adbc.StatusIO) || !strings.Contains(err.Error(), "statement snapshot expired") {
		t.Fatal("expired-owner diagnostic", err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("expired owner changed root")
	}
	reopened := newSQLHarness(t)
	expectStagedRekeyReleased(t, h, raw, abandoned)
	// Stage a real write, then change the schema through SQL. Its publication
	// reports 40001; a fresh request revalidates against the new schema.
	st := &store{client: raw, snapshots: true}
	ctx, pin, id := snapshotStageSQL(t, st, h.ctx, from, "INSERT INTO "+from+" VALUES (2)")
	private, err := st.getTable(ctx, defaultSchema, from)
	if err != nil {
		t.Fatal(err)
	}
	reopened.exec("ALTER TABLE " + from + " ADD COLUMN v INTEGER")
	root = raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	err = st.publishSnapshot(ctx, pin, id)
	if !isStatus(err, adbc.StatusIO) || sqlState(err) != "40001" {
		t.Fatal("altered-source diagnostic", err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("stale write changed root")
	}
	st.abortSnapshot(pin, id)
	pin.release()
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	h.expectRows("SELECT id,v FROM "+from, "1|NULL")
	// A committed rename also refuses a previously staged INSERT, while its
	// independent old snapshot still reads the original binding.
	ctx, pin, id = snapshotStageSQL(t, st, h.ctx, from, "INSERT INTO "+from+" VALUES (3,30)")
	private, err = st.getTable(ctx, defaultSchema, from)
	if err != nil {
		t.Fatal(err)
	}
	source, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	h.exec("ALTER TABLE " + from + " RENAME TO " + to)
	root = raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := st.publishSnapshot(ctx, pin, id); !isStatus(err, adbc.StatusIO) || sqlState(err) != "40001" {
		t.Fatal("renamed-source diagnostic", err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("stale renamed write changed root")
	}
	st.abortSnapshot(pin, id)
	pin.release()
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	rows := snapshotExecuteTest(t, source, oldCtx, "SELECT id,v FROM "+from).rows
	if len(rows) != 1 || rows[0][0].I != 1 || !rows[0][1].Null {
		t.Fatal("rename changed old diagnostic input", rows)
	}
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, from); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("old binding remained", err)
	}
	h.expectRows("SELECT id,v FROM "+to, "1|NULL")
	h.exec("INSERT INTO " + to + " VALUES (4,40)")
	h.expectRows("SELECT id,v FROM "+to+" ORDER BY id", "1|NULL", "4|40")
}

// DROP retires abandoned output without invalidating old readers. A recreated
// binding and its explicit rekey never inherit either old physical identity.
func TestSQLRekeyDropAbandoned(t *testing.T) {
	const from, to = "it_rk_drop", "it_rk_drop2"
	h := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	h.dropTables(from, to)
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (1),(2)")
	raw := h.rawClient()
	source, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	abandoned := stageRekeySnapshot(t, h, raw, from, to)
	abandoned.pin.release()
	expireRekeyControl(t, h, raw, "w:"+abandoned.id)
	h.exec("DROP TABLE " + from)
	expectStagedRekeyReleased(t, h, raw, abandoned)
	if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, to); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal("abandoned destination became public", err)
	}
	h.exec("CREATE TABLE " + from + " (id INTEGER)")
	h.exec("INSERT INTO " + from + " VALUES (3)")
	h.exec("ALTER TABLE " + from + " RENAME TO " + to)
	current := h.inspectTable(raw, defaultSchema, to)
	if current.prefix() == abandoned.source.prefix() || current.prefix() == abandoned.target.prefix() {
		t.Fatal("recreated binding reused abandoned storage")
	}
	h.expectRows("SELECT id FROM "+to, "3")
	rows := snapshotExecuteTest(t, source, oldCtx, "SELECT id FROM "+from+" ORDER BY id").rows
	if len(rows) != 2 || rows[0][0].I != 1 || rows[1][0].I != 2 {
		t.Fatal("DROP recovery changed retained input", rows)
	}
	oldPin.release()
	for _, seg := range abandoned.source.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	assertSnapshotDataReferences(t, h, &store{client: raw, snapshots: true})
}
