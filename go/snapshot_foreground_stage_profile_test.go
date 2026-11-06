// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Drive the uncontended publication components once, in production order. This
// opt-in fixture attributes helper wall/client time; it is not a public native
// benchmark, contention profile or alternate driver publication implementation.
func TestSnapshotForegroundPublicationStageProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_STAGE_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_STAGE_PROFILE on an owned Software fixture")
	}
	for _, unrelated := range []int{0, 32} {
		t.Run(fmt.Sprintf("unrelated_%d", unrelated), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE phase_target(id BIGINT,v BIGINT,payload VARCHAR)")
			h.exec("INSERT INTO phase_target SELECT i,9007199254740993,'row-'||i FROM generate_series(1,128) AS g(i)")
			guard := &snapshotUnrelatedMetadataCopies{}
			for i := 0; i < unrelated; i++ {
				name := fmt.Sprintf("phase_other_%d", i)
				h.exec("CREATE TABLE " + name + "(v BIGINT)")
				h.exec("INSERT INTO " + name + " VALUES(77)")
				guard.suffixes = append(guard.suffixes, strings.TrimPrefix(metaKey("public", name), metaPrefix))
			}
			s := snapshotTestStore(t, raw)
			// Exclude first connection/TLS setup from foreground phase timings.
			if err := s.client.Ping(h.ctx).Err(); err != nil {
				t.Fatal(err)
			}
			profile := &compactionPhaseProfile{phases: map[string]*compactionProfilePhase{}}
			s.client.AddHook(profile)
			s.client.AddHook(guard)
			phase := func(name string, run func()) {
				t.Helper()
				profile.mu.Lock()
				profile.phases[name] = &compactionProfilePhase{RoundTrips: map[string]*compactionProfileRoundTrips{}}
				profile.phase = name
				profile.mu.Unlock()
				began := time.Now()
				defer func() {
					profile.mu.Lock()
					profile.phases[name].Steps = 1
					profile.phases[name].Millis = float64(time.Since(began)) / float64(time.Millisecond)
					profile.phase = ""
					profile.mu.Unlock()
				}()
				run()
			}
			var base, work context.Context
			var pin *snapshotPin
			var id string
			var err error
			published := false
			t.Cleanup(func() {
				if pin != nil {
					if id != "" && !published {
						s.abortSnapshot(pin, id)
					}
					pin.release()
				}
			})
			phase("admission", func() {
				base, pin, err = s.acquireSnapshot(h.ctx, true)
				if err != nil {
					t.Fatal(err)
				}
			})
			phase("metadata_resolution_and_dependencies", func() {
				if _, err = s.getTable(base, "public", "phase_target"); err != nil {
					t.Fatal(err)
				}
			})
			scope := snapshotOf(base)
			scope.metadataMu.Lock()
			dependencies := len(scope.metadata.reads)
			scope.metadataMu.Unlock()
			if dependencies == 0 {
				t.Fatal("planning dependency was not recorded")
			}
			phase("reserve_and_shared_metadata", func() {
				work, id, err = s.beginSnapshotWrite(base, pin)
				if err != nil {
					t.Fatal(err)
				}
			})
			scope = snapshotOf(work)
			scope.metadataMu.Lock()
			inheritedDependencies := len(scope.metadata.reads)
			scope.metadataMu.Unlock()
			if inheritedDependencies < dependencies {
				t.Fatal("workspace lost planning dependencies")
			}
			phase("private_metadata_and_row_stage", func() {
				if err = s.prepareSnapshotTable(work, "public", "phase_target"); err != nil {
					t.Fatal(err)
				}
				snapshotExecuteTest(t, s, work, "UPDATE phase_target SET v=v+1 WHERE __rowid=1")
			})
			phase("lease_validation", func() {
				if err = pin.valid(work); err != nil {
					t.Fatal(err)
				}
			})
			phase("index_sealing", func() {
				if err = s.rebuildSnapshotIndexes(work); err != nil {
					t.Fatal(err)
				}
			})
			phase("index_readiness", func() {
				if err = s.validateSnapshot(work); err != nil {
					t.Fatal(err)
				}
			})
			phase("deletion_pruning", func() {
				if err = s.pruneSnapshotMetadata(work); err != nil {
					t.Fatal(err)
				}
			})
			phase("atomic_seal_and_publication", func() {
				n, e := s.snapshotControl(work, "publish", pin.token, id, pin.root, "", "", "", scope.compactionUpdates).Int()
				if e != nil || n != 1 {
					t.Fatal("uncontended publication failed; input was not replayed", n, e)
				}
				published = true
			})
			if guard.reads.Load() != 0 || guard.copies.Load() != 0 {
				t.Fatal("publication read/copied unrelated descriptor payloads", guard.reads.Load(), guard.copies.Load())
			}
			creates, drops := 0, 0
			profile.mu.Lock()
			for _, stats := range profile.phases {
				if r := stats.RoundTrips["ft.create"]; r != nil {
					creates += r.Calls
				}
			}
			for _, name := range []string{"index_sealing", "index_readiness"} {
				if r := profile.phases[name].RoundTrips["ft.dropindex"]; r != nil {
					drops += r.Calls
				}
			}
			profile.mu.Unlock()
			if creates != 1 || drops != 0 {
				t.Fatal("publication did not retain one final index build", creates, drops)
			}
			meta, err := s.getTable(work, "public", "phase_target")
			if err != nil {
				t.Fatal(err)
			}
			data := snapshotDataID(meta.prefix())
			if !snapshotOwns(work, meta.prefix()) {
				t.Fatal("expected an owned output")
			}
			rows, err := raw.ZCard(h.ctx, snapshotMembers(data)).Result()
			if err != nil || rows != 1 {
				t.Fatal("point output copied unchanged rows", rows, err)
			}
			suffix := strings.TrimPrefix(metaKey("public", "phase_target"), metaPrefix)
			descriptor, err := resolveSnapshotMetadata(work, suffix, false)
			if err != nil {
				t.Fatal(err)
			}
			memory := map[string]int64{}
			for name, key := range map[string]string{"output_row": meta.prefix() + "1", "output_directory": snapshotMembers(data), "private_descriptor": descriptor, "root_map": snapshotMetadataMap(id), "owned_metadata_directory": snapshotMetadataKeys(id)} {
				bytes, e := raw.MemoryUsage(h.ctx, key).Result()
				if e != nil || bytes < 0 {
					t.Fatal("private storage accounting failed", name, e)
				}
				memory[name] = bytes
			}
			reply, err := raw.Do(h.ctx, "FT.INFO", meta.index()).Result()
			if err != nil {
				t.Fatal(err)
			}
			info := indexInfoOf(reply)
			if info.numDocs != 1 || info.failures != 0 || len(info.prefixes) != 1 || info.prefixes[0] != meta.prefix() {
				t.Fatal("final index identity/cardinality proof lost")
			}
			fields, _ := redisReplyFields(reply)
			indexMemory := map[string]float64{}
			for _, name := range []string{"inverted_sz_mb", "doc_table_size_mb", "key_table_size_mb", "sortable_values_size_mb"} {
				if value, ok := fields[name]; ok {
					n, e := strconv.ParseFloat(fmt.Sprint(value), 64)
					if e != nil || n < 0 {
						t.Fatal("invalid native index memory metric", name)
					}
					indexMemory[name] = n
				}
			}
			if len(indexMemory) == 0 {
				t.Fatal("native index memory metrics unavailable")
			}
			actual, _ := h.query("SELECT id,v,payload FROM phase_target ORDER BY id")
			if len(actual) != 128 {
				t.Fatal("incomplete post-publication fetch", len(actual))
			}
			for i, row := range actual {
				v := int64(9007199254740993)
				if i == 0 {
					v++
				}
				if row != fmt.Sprintf("%d|%d|row-%d", i+1, v, i+1) {
					t.Fatal("incorrect published row", i+1)
				}
			}
			for i := 0; i < unrelated; i++ {
				h.expectRows(fmt.Sprintf("SELECT v FROM phase_other_%d", i), "77")
			}
			payload := map[string]any{"unrelated_tables": unrelated, "phases": profile.phases, "planning_dependencies": dependencies, "inherited_dependencies": inheritedDependencies, "unrelated_descriptor_reads": guard.reads.Load(), "unrelated_descriptor_copies": guard.copies.Load(), "new_indexes": creates, "sealing_index_drops": drops, "output_rows": rows, "private_key_memory_bytes": memory, "index_memory_mb": indexMemory, "caller_retries": 0, "maintenance": "disabled; uncontended helper attribution"}
			profile.mu.Lock()
			encoded, err := json.Marshal(payload)
			profile.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("snapshot_publication_stages %s", encoded)
		})
	}
}
