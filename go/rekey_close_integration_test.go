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

// Integration tests for closing a connection while a re-keying rename runs
// on another goroutine (stopRekeys in rekey.go).

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// closeDuringRekey renames from to to with rename_rekey on a connection of
// its own, on another goroutine, and closes that connection once the rename
// reaches step ("copy" or "switched"). There the rename waits for Close to
// stop its move, or for hold if Close doesn't (or if ignoreClose is set, as
// a command waiting for its reply does). It returns the job as recorded
// before Close, how long Close took, and a channel with the rename's error.
func (h *sqlHarness) closeDuringRekey(from, to, step string, hold time.Duration, ignoreClose bool) (*rekeyJob, time.Duration, <-chan error) {
	h.t.Helper()
	raw := h.rawClient()
	oldPrefix, _ := h.tableNames(raw, "public", from)
	r := newRekeyHarness(h.t, map[string]string{OptionStringRenameRekey: "true"})
	paused := make(chan struct{})
	var once sync.Once
	testHookRekeyStep = func(ctx context.Context, at string) {
		if at != step {
			return
		}
		once.Do(func() { close(paused) })
		if ignoreClose {
			time.Sleep(hold)
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(hold):
		}
	}
	renamed, done := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := tryExec(h.ctx, r.conn, "ALTER TABLE "+from+" RENAME TO "+to)
		renamed <- err
	}()
	// The hook is reset once the rename has ended.
	go func() {
		err := <-renamed
		testHookRekeyStep = nil
		done <- err
	}()
	select {
	case <-paused:
	case err := <-done:
		h.t.Fatalf("the rename ended before it reached %q: %v", step, err)
	case <-time.After(30 * time.Second):
		h.t.Fatalf("the rename did not reach %q", step)
	}
	job := h.rekeyRecord(raw, oldPrefix)
	if job == nil {
		h.t.Fatal("no rename recorded")
	}
	if keys := h.aliveKeys(raw); len(keys) != 1 {
		h.t.Errorf("lease keys during the rename: %v", keys)
	}
	began := time.Now()
	if err := r.conn.Close(h.ctx); err != nil {
		h.t.Errorf("Close: %v", err)
	}
	return job, time.Since(began), done
}

// expectClosedRename checks the error of a rename stopped by Close.
func expectClosedRename(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "the connection was closed while the table was being renamed") {
		t.Errorf("the rename: got %v, want the error of a rename stopped by Close", err)
	}
}

// Closing a connection while its rename moves the rows on another goroutine
// stops the rename and rolls it back before Close returns: no lease is
// left, so other connections change the table, or rename it, at once.
func TestSQLRekeyCloseDuringMove(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_cm", "it_rk_cm2")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_rk_cm (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_cm VALUES (1), (2), (3)`)
	oldPrefix, oldIndex := h.tableNames(raw, "public", "it_rk_cm")

	job, took, renamed := h.closeDuringRekey("it_rk_cm", "it_rk_cm2", "copy", 3*time.Second, false)
	if took > 5*time.Second {
		t.Errorf("Close took %v", took)
	}
	if keys := h.aliveKeys(raw); len(keys) != 0 {
		t.Errorf("lease keys left after Close: %v", keys)
	}
	if h.rekeyPending(raw, oldPrefix) {
		t.Error("the rename is still recorded after Close")
	}
	h.expectReleased(raw, job.KeyPrefix, job.IndexName)
	expectClosedRename(t, <-renamed)

	// The table is as it was, and changes at once.
	began := time.Now()
	h.exec(`INSERT INTO it_rk_cm VALUES (4)`)
	if d := time.Since(began); d > 5*time.Second {
		t.Errorf("an INSERT after Close took %v", d)
	}
	h.expectRows(`SELECT id FROM it_rk_cm ORDER BY id`, "1", "2", "3", "4")
	if p, i := h.tableNames(raw, "public", "it_rk_cm"); p != oldPrefix || i != oldIndex {
		t.Errorf("names after the rollback: %q %q, want %q %q", p, i, oldPrefix, oldIndex)
	}
	if err := h.execErr(`SELECT * FROM it_rk_cm2`); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("the new name after the rollback: %v", err)
	}
	other := newRekeyHarness(t, map[string]string{OptionStringRenameRekey: "true"})
	other.exec(`ALTER TABLE it_rk_cm RENAME TO it_rk_cm2`)
	other.expectRows(`SELECT id FROM it_rk_cm2 ORDER BY id`, "1", "2", "3", "4")
	h.expectReleased(raw, oldPrefix, oldIndex)
}

// A rename that has switched the metadata when its connection is closed
// is finished before Close returns.
func TestSQLRekeyCloseAfterSwitch(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_rk_cs", "it_rk_cs2")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_rk_cs (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_cs VALUES (1), (2), (3)`)
	oldPrefix, oldIndex := h.tableNames(raw, "public", "it_rk_cs")

	job, took, renamed := h.closeDuringRekey("it_rk_cs", "it_rk_cs2", "switched", 3*time.Second, false)
	if took > 5*time.Second {
		t.Errorf("Close took %v", took)
	}
	if keys := h.aliveKeys(raw); len(keys) != 0 {
		t.Errorf("lease keys left after Close: %v", keys)
	}
	if h.rekeyPending(raw, oldPrefix) {
		t.Error("the rename is still recorded after Close")
	}
	h.expectReleased(raw, oldPrefix, oldIndex)
	if err := <-renamed; err != nil {
		t.Errorf("the rename: %v", err)
	}
	if p, i := h.tableNames(raw, "public", "it_rk_cs2"); p != job.KeyPrefix || i != job.IndexName {
		t.Errorf("names after the rename: %q %q, want %q %q", p, i, job.KeyPrefix, job.IndexName)
	}
	h.exec(`INSERT INTO it_rk_cs2 VALUES (4)`)
	h.expectRows(`SELECT id FROM it_rk_cs2 ORDER BY id`, "1", "2", "3", "4")
}

// A rename that doesn't stop within rekeyTTL/2 of Close (here: it waits
// for a command's reply) gives its lease up: Close deletes the lease key,
// so the next connection rolls the rename back at once, and the rename
// takes no further step.
func TestSQLRekeyCloseGivesUp(t *testing.T) {
	defer func(was time.Duration) { rekeyTTL = was }(rekeyTTL)
	rekeyTTL = 2 * time.Second
	h := newSQLHarness(t)
	h.dropTables("it_rk_cg", "it_rk_cg2")
	raw := h.rawClient()
	h.exec(`CREATE TABLE it_rk_cg (id INTEGER)`)
	h.exec(`INSERT INTO it_rk_cg VALUES (1), (2), (3)`)
	oldPrefix, _ := h.tableNames(raw, "public", "it_rk_cg")

	// The lease is renewed every 500 ms meanwhile.
	job, took, renamed := h.closeDuringRekey("it_rk_cg", "it_rk_cg2", "copy", 3*time.Second, true)
	if took < rekeyTTL/2 || took > rekeyTTL {
		t.Errorf("Close took %v, want about %v", took, rekeyTTL/2)
	}
	if keys := h.aliveKeys(raw); len(keys) != 0 {
		t.Errorf("lease keys left after Close: %v", keys)
	}
	if !h.rekeyPending(raw, oldPrefix) {
		t.Error("the rename is no longer recorded")
	}
	// The rename is still waiting.
	h2 := newSQLHarness(t)
	if h2.rekeyPending(raw, oldPrefix) {
		t.Error("a connection that opened did not roll the rename back")
	}
	h2.expectReleased(raw, job.KeyPrefix, job.IndexName)
	if err := h2.execErr(`INSERT INTO it_rk_cg VALUES (4)`); err != nil {
		t.Error(err)
	}
	expectClosedRename(t, <-renamed)
	// It copied nothing once it went on.
	h2.expectReleased(raw, job.KeyPrefix, job.IndexName)
	h2.expectRows(`SELECT id FROM it_rk_cg ORDER BY id`, "1", "2", "3", "4")
}
