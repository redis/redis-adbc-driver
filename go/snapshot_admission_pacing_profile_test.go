// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A held target admission cannot progress. Unrelated publications must not
// reset that target's retry schedule. Both controls retain production workers;
// this is a probe-count diagnostic, not a CPU or fairness benchmark.
func TestManagedAdmissionUnrelatedTrafficProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_PACING_CONTROL") != "true" {
		t.Skip("opt-in owned Software blocked-target pacing control")
	}
	for _, traffic := range []bool{false, true} {
		t.Run(fmt.Sprint(traffic), func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, true, false)
			h.exec("CREATE SEQUENCE pacing_q CACHE 1")
			h.exec("CREATE TABLE pacing_blocked(id BIGINT)")
			h.exec("CREATE TABLE pacing_other(id BIGINT)")
			h.exec("INSERT INTO pacing_other VALUES(0)")
			ownerStore := snapshotTestStore(t, raw)
			_, owner := holdIngestAdmission(t, h, ownerStore, "pacing_blocked")
			defer owner.release()
			conn := admissionConnection(t, db)
			st, err := conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, "INSERT INTO pacing_blocked VALUES(NEXTVAL('pacing_q'))"); err != nil {
				t.Fatal(err)
			}
			impl := st.(*statementImpl).conn
			epoch := time.Now()
			hook := &managedTailHook{epoch: epoch}
			impl.store.client.AddHook(hook)
			pool := impl.newExecutionPool(impl.timeouts)
			pool.client.AddHook(hook)
			impl.store.idleExecution = pool
			sample := &managedTailSample{Kind: "blocked"}
			ctx, cancel := context.WithTimeout(context.WithValue(h.ctx, managedTailOperationKey{}, sample), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			began := time.Now()
			sample.StartNS = began.Sub(epoch).Nanoseconds()
			go func() { _, err := st.ExecuteUpdate(ctx); done <- err }()
			joined := false
			defer func() {
				cancel()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("blocked writer did not join")
					}
				}
			}()
			// Wait for an actual server admission refusal before applying traffic.
			for {
				hook.mu.Lock()
				attempted := hook.commands["control_admit_ingest"] > 0
				hook.mu.Unlock()
				if attempted {
					break
				}
				select {
				case err := <-done:
					joined = true
					t.Fatal("held target unexpectedly completed", err)
				case <-ctx.Done():
					t.Fatal("blocked target never reached admission")
				case <-time.After(time.Millisecond):
				}
			}
			published, inserted := 0, 0
			if traffic {
				ticker := time.NewTicker(250 * time.Millisecond)
				defer ticker.Stop()
				for i := 1; i <= 6; i++ {
					select {
					case <-ticker.C:
					case <-ctx.Done():
						t.Fatal("control exceeded deadline")
					}
					if i <= 2 {
						if n := h.exec(fmt.Sprintf("INSERT INTO pacing_other VALUES(%d)", i)); n != 1 {
							t.Fatal("unrelated insert was not acknowledged")
						}
						inserted++
					} else {
						// Stay below four segments: a full-catalog owner pin can block
						// compaction installation, which is a separate admission condition.
						if n := h.exec(fmt.Sprintf("COMMENT ON TABLE pacing_other IS 'pacing-%d'", i)); n != -1 {
							t.Fatal("unrelated comment was not acknowledged")
						}
					}
					published++
				}
			}
			timer := time.NewTimer(max(0, 2*time.Second-time.Since(began)))
			select {
			case <-timer.C:
			case err := <-done:
				joined = true
				timer.Stop()
				t.Fatal("held target did not remain blocked", err)
			}
			cancel()
			select {
			case err = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled blocked writer did not finish")
			}
			elapsed := time.Since(began)
			if !isStatus(err, adbc.StatusCancelled) {
				t.Fatal("cancellation status changed", err)
			}
			hook.mu.Lock()
			counts := make(map[string]int, len(hook.commands))
			for k, n := range hook.commands {
				counts[k] = n
			}
			hook.mu.Unlock()
			sample.summarize()
			owner.release()
			if err := st.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			h.expectRows("SELECT COUNT(*) FROM pacing_blocked", "0")
			h.expectRows("SELECT NEXTVAL('pacing_q')", "1")
			expected := []string{"0"}
			for i := 1; i <= inserted; i++ {
				expected = append(expected, fmt.Sprint(i))
			}
			h.expectRows("SELECT id FROM pacing_other ORDER BY id", expected...)
			vctx, vpin, err := ownerStore.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := ownerStore.getTable(vctx, defaultSchema, "pacing_other")
			vpin.release()
			if err != nil {
				t.Fatal(err)
			}
			wantComment := ""
			if traffic {
				wantComment = "pacing-6"
			}
			if meta.Comment != wantComment {
				t.Fatal("unrelated comment state differs")
			}
			payload, err := json.Marshal(map[string]any{"unrelated_traffic": traffic, "unrelated_acknowledged_publications": published, "unrelated_data_inserts": inserted, "unrelated_metadata_comments": published - inserted, "seconds": elapsed.Seconds(), "blocked_foreground_command_counts": counts, "blocked_acquire_attempts": sample.AcquireAttempts, "cancelled": true, "blocked_execution_count": 0, "validated": true, "scope": "held target; two unrelated INSERTs plus four COMMENT publications, or idle; below compaction trigger; production workers; no input execution/replay; command counts are not CPU"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("MANAGED_PACING_UNRELATED_CONTROL %s", payload)
		})
	}
}
