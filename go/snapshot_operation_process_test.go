// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type operationCountingInput struct {
	array.RecordReader
	calls int
}

func (r *operationCountingInput) Next() bool { r.calls++; return r.RecordReader.Next() }

func TestSnapshotClusterOperationArrowAPI(t *testing.T) {
	h, _, _ := snapshotClusterHarness(t)
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "payload", Type: arrow.BinaryTypes.Binary, Nullable: true}}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{9007199254740993, 0}, []bool{true, false})
	builder.Field(1).(*array.BinaryBuilder).AppendValues([][]byte{{0, 255}, {}}, nil)
	record := builder.NewRecordBatch()
	defer record.Release()
	stream, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	if err != nil {
		t.Fatal(err)
	}
	input := &operationCountingInput{RecordReader: stream}
	defer input.Release()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for key, value := range map[string]string{adbc.OptionKeyIngestTargetTable: "operation_arrow", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeCreate, OptionStringOperationAction: "prepare"} {
		if err := st.SetOption(h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	input.Retain()
	if err := st.BindStream(h.ctx, input); err != nil {
		input.Release()
		t.Fatal(err)
	}
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 0 {
		t.Fatal("Arrow prepare", n, err)
	}
	handle, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
	if err != nil {
		t.Fatal(err)
	}
	consumed := input.calls
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='operation_arrow'", "0")
	other, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	for key, value := range map[string]string{OptionStringOperationHandle: handle, OptionStringOperationAction: "resume"} {
		if err := other.SetOption(h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := other.ExecuteUpdate(h.ctx); err != nil || n != 2 {
		t.Fatal("ADBC resume without Arrow input", n, err)
	}
	if input.calls != consumed {
		t.Fatal("resume consumed the original Arrow input again")
	}
	h.expectRows("SELECT COUNT(*),COUNT(v),MAX(v) FROM operation_arrow", "2|1|9007199254740993")
	query, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close(h.ctx)
	if err := query.SetSqlQuery(h.ctx, "SELECT payload FROM operation_arrow ORDER BY v NULLS LAST"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := query.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Release()
	expected := [][]byte{{0, 255}, {}}
	count := 0
	for rows.Next() {
		data, ok := rows.RecordBatch().Column(0).(*array.Binary)
		if !ok {
			t.Fatal("binary Arrow type changed")
		}
		for i := 0; i < data.Len(); i++ {
			if count >= len(expected) || data.IsNull(i) || !slices.Equal(data.Value(i), expected[count]) {
				t.Fatal("captured binary/empty value changed")
			}
			count++
		}
	}
	if rows.Err() != nil || count != 2 {
		t.Fatal("complete Arrow result fetch", count, rows.Err())
	}
}

// This helper is invoked only in a separate process, never via a resetting
// harness. It shares only a public handle and the test fixture's existing URI.
func TestOperationProcessHelper(t *testing.T) {
	action := os.Getenv("REDIS_OPERATION_TEST_ACTION")
	if action == "" {
		t.Skip("separate-process operation helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	handle := os.Getenv("REDIS_OPERATION_TEST_HANDLE")
	var status *SnapshotOperationStatus
	var err error
	switch action {
	case "crash-output":
		testHookRowsWritten = func(_ *tableMeta, n int) {
			if n > 0 {
				if e := os.WriteFile(os.Getenv("REDIS_OPERATION_TEST_RESULT"), []byte("acknowledged"), 0600); e != nil {
					t.Fatal(e)
				}
				// The parent kills this process before any release/Close runs.
				select {}
			}
		}
		status, err = ResumeOperation(ctx, options, handle)
	case "prepare":
		_, status, err = PrepareAdoptHashes(ctx, options, HashAdoptOptions{Prefix: "operation:process:", Table: "operation_process", Types: map[string]string{"v": "BIGINT"}}, time.Hour)
	case "partial":
		var fired atomic.Bool
		testHookRowsWritten = func(_ *tableMeta, n int) {
			if n > 0 && fired.CompareAndSwap(false, true) {
				cancel()
			}
		}
		status, err = ResumeOperation(ctx, options, handle)
		testHookRowsWritten = nil
		if err == nil || !fired.Load() || status == nil || status.State != "ready" || status.RemainingRows <= 0 || status.RemainingRows >= 513 {
			t.Fatal("partial continuation did not stop at an acknowledged checkpoint", status, err)
		}
		err = nil
	case "indexes":
		c, e := dialHashesWithPin(ctx, options, false, false)
		if e != nil {
			t.Fatal(e)
		}
		defer c.close(ctx)
		owner, terminal, e := c.st.takeSnapshotOperation(ctx, handle)
		if e != nil || terminal != nil {
			t.Fatal(terminal, e)
		}
		defer owner.Pin.release()
		if e := c.st.prepareOperationPublication(owner); e != nil {
			t.Fatal(e)
		}
		if _, e := c.st.operationControl(owner.Context, "release", handle, owner.Pin.token, owner.Pin.root, 0, nil).Int(); e != nil {
			t.Fatal(e)
		}
		status, err = c.st.snapshotOperationStatus(ctx, handle)
	case "resume":
		status, err = ResumeOperation(ctx, options, handle)
	default:
		t.Fatal("unknown helper action")
	}
	if err != nil || status == nil {
		t.Fatal(status, err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("REDIS_OPERATION_TEST_RESULT"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runOperationProcess(t *testing.T, action, handle string) *SnapshotOperationStatus {
	t.Helper()
	result := filepath.Join(t.TempDir(), "outcome.json")
	command := exec.Command(os.Args[0], "-test.run=^TestOperationProcessHelper$", "-test.timeout=4m")
	command.Env = append(os.Environ(), "REDIS_OPERATION_TEST_ACTION="+action, "REDIS_OPERATION_TEST_HANDLE="+handle, "REDIS_OPERATION_TEST_RESULT="+result)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("operation helper %s failed: %v\n%s", action, err, output)
	}
	raw, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var status SnapshotOperationStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	return &status
}

func TestSnapshotClusterOperationSeparateProcesses(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	keys := make([]string, 513)
	for i := range keys {
		keys[i] = fmt.Sprintf("operation:process:%05d", i)
		if i == 0 {
			keys[i] = "operation:process:\x00\xff"
		}
		if err := raw.HSet(h.ctx, keys[i], "v", "9007199254740993").Err(); err != nil {
			t.Fatal(err)
		}
	}
	prepared := runOperationProcess(t, "prepare", "")
	if prepared.State != "ready" || prepared.Rows != 513 {
		t.Fatal(prepared)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='operation_process'", "0")
	// Application changes/deletes after capture cannot change retained input.
	for _, key := range keys {
		if err := raw.Del(h.ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	partial := runOperationProcess(t, "partial", prepared.Handle)
	if partial.State != "ready" || partial.RemainingRows >= 513 {
		t.Fatal(partial)
	}
	indexed := runOperationProcess(t, "indexes", prepared.Handle)
	if indexed.State != "ready" || indexed.RemainingRows != 0 {
		t.Fatal(indexed)
	}
	indexesBefore, err := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if err != nil {
		t.Fatal(err)
	}
	committed := runOperationProcess(t, "resume", prepared.Handle)
	if committed.State != "committed" || committed.AffectedRows != 513 {
		t.Fatal(committed)
	}
	indexesAfter, err := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(indexesBefore)
	slices.Sort(indexesAfter)
	if !slices.Equal(indexesBefore, indexesAfter) {
		t.Fatal("resume recreated already prepared indexes")
	}
	h.expectRows("SELECT COUNT(*),MAX(v),COUNT(__key) FROM operation_process", "513|9007199254740993|513")
	if committed := runOperationProcess(t, "resume", prepared.Handle); committed.State != "committed" {
		t.Fatal(committed)
	}
	h.expectRows("SELECT COUNT(*) FROM operation_process", "513")
}

func TestSnapshotClusterOperationKilledOwner(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	for i := 0; i < 257; i++ {
		if err := raw.HSet(h.ctx, fmt.Sprintf("operation:process:%05d", i), "v", "9007199254740993").Err(); err != nil {
			t.Fatal(err)
		}
	}
	prepared := runOperationProcess(t, "prepare", "")
	result := filepath.Join(t.TempDir(), "acknowledged")
	command := exec.Command(os.Args[0], "-test.run=^TestOperationProcessHelper$", "-test.timeout=4m")
	command.Env = append(os.Environ(), "REDIS_OPERATION_TEST_ACTION=crash-output", "REDIS_OPERATION_TEST_HANDLE="+prepared.Handle, "REDIS_OPERATION_TEST_RESULT="+result)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := os.Stat(result); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never acknowledged its partial output")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("child exited cleanly instead of being killed")
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	status, err := GetOperationStatus(h.ctx, options, prepared.Handle)
	if err != nil || !status.OwnerActive || status.RemainingRows <= 0 || status.RemainingRows >= 257 {
		t.Fatal("crash checkpoint", status, err)
	}
	if _, err := ResumeOperation(h.ctx, options, prepared.Handle); err == nil {
		t.Fatal("takeover admitted before original lease expired")
	}
	deadline = time.Now().Add(snapshotLease + 15*time.Second)
	for status.OwnerActive {
		if time.Now().After(deadline) {
			t.Fatal("dead owner lease never expired")
		}
		time.Sleep(time.Second)
		status, err = GetOperationStatus(h.ctx, options, prepared.Handle)
		if err != nil {
			t.Fatal(err)
		}
	}
	status = runOperationProcess(t, "resume", prepared.Handle)
	if status.State != "committed" || status.AffectedRows != 257 {
		t.Fatal(status)
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v),COUNT(__key) FROM operation_process", "257|9007199254740993|9007199254740993|257")
}

func TestSnapshotClusterOperationRecovery(t *testing.T) {
	dir := os.Getenv("REDIS_SNAPSHOT_CHAOS_DIR")
	if dir == "" {
		t.Skip("requires owned promotion/persistence recovery orchestration")
	}
	h, raw, _ := snapshotClusterHarness(t)
	for i := 0; i < 257; i++ {
		if err := raw.HSet(h.ctx, fmt.Sprintf("operation:process:%05d", i), "v", "9007199254740993").Err(); err != nil {
			t.Fatal(err)
		}
	}
	captured := runOperationProcess(t, "prepare", "")
	// A second independent SQL operation is fully indexed and sealed before the
	// outage. Recovery may validate it, but must never create its index again.
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetSqlQuery(h.ctx, "CREATE TABLE operation_recovery_sql AS SELECT 17 AS v"); err != nil {
		t.Fatal(err)
	}
	if err = st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	handle, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	sealed := runOperationProcess(t, "indexes", handle)
	if sealed.State != "ready" {
		t.Fatal(sealed)
	}
	for i := 0; i < 257; i++ {
		if err := raw.Del(h.ctx, fmt.Sprintf("operation:process:%05d", i)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dir+"/ready", []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if _, err := os.Stat(dir + "/continue"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation recovery orchestrator timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, operation := range []*SnapshotOperationStatus{sealed, captured} {
		status := runOperationProcess(t, "resume", operation.Handle)
		if status.State != "committed" || status.AffectedRows != operation.Rows {
			t.Fatal(status)
		}
	}
	h.expectRows("SELECT v FROM operation_recovery_sql", "17")
	h.expectRows("SELECT COUNT(*),MAX(v),COUNT(__key) FROM operation_process", "257|9007199254740993|257")
	snapshotClusterAuditMetadata(t, h.ctx, raw, "resumable operation recovery")
}
