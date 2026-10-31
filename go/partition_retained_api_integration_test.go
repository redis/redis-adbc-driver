// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// Explicit fixture roots and verified control persistence remain test-local.
// Managed fixtures set the declaration only after their API verifies policy.
func retainedTestDatabaseOptions(options map[string]string) map[string]string {
	if ca := os.Getenv("REDIS_RETAINED_TLS_CA_FILE"); ca != "" {
		options[OptionStringTLSCAFile] = ca
	}
	if policy := os.Getenv("REDIS_RETAINED_CONTROL_PERSISTENCE"); policy != "" {
		options[OptionStringPartitionControlPersistence] = policy
	}
	return options
}

func retainedAPIConnection(t *testing.T, namespace string, reader bool) adbc.ConnectionWithContext {
	t.Helper()
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringPartitionResultNamespace: namespace}
	if reader {
		options[OptionStringPartitionReaderOnly] = "true"
	}
	database, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(context.Background(), retainedTestDatabaseOptions(options))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	connection, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	return connection
}
func retainedAPIPlan(t *testing.T, connection adbc.ConnectionWithContext, query string) (*arrow.Schema, adbc.Partitions, int64) {
	t.Helper()
	ctx := context.Background()
	statement, err := connection.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(ctx)
	if err := statement.SetSqlQuery(ctx, query); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{OptionStringPartitionResultMode: "retained", OptionIntStreamBatchRows: "2", OptionIntPartitionCount: "2"} {
		if err := statement.SetOption(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	schema, parts, rows, err := statement.ExecutePartitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return schema, parts, rows
}
func retainedAPIReadIDs(t *testing.T, connection adbc.ConnectionWithContext, schema *arrow.Schema, parts adbc.Partitions) []int64 {
	t.Helper()
	var ids []int64
	for _, part := range parts.PartitionIDs {
		reader, err := connection.ReadPartition(context.Background(), part)
		if err != nil {
			t.Fatal(err)
		}
		if !reader.Schema().Equal(schema) {
			reader.Release()
			t.Fatal("retained schema differs")
		}
		for reader.Next() {
			ids = append(ids, reader.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
		}
		err = reader.Err()
		reader.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func TestRetainedPartitionAPIPublicationReplayAndRevoke(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_api"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT,grp INTEGER)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (9007199254740993,1),(9223372036854775807,1),(-1,2),(7,2),(9,3)")
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	publisher := retainedAPIConnection(t, storage.namespace, false)
	schema, parts, rows := retainedAPIPlan(t, publisher, "SELECT id FROM "+table+" ORDER BY id")
	if rows != 5 || parts.NumPartitions != 2 {
		t.Fatal(rows, parts.NumPartitions)
	}
	if err := publisher.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	// A retained result survives planner closure and source writes/DDL; it is
	// never reconstructed by rerunning its SQL on a retrying worker.
	h.exec("UPDATE " + table + " SET id=42 WHERE id=7")
	h.exec("DELETE FROM " + table + " WHERE id=-1")
	h.exec("INSERT INTO " + table + " VALUES (100,4)")
	h.exec("TRUNCATE TABLE " + table)
	worker := retainedAPIConnection(t, storage.namespace, true)
	expected := []int64{-1, 7, 9, 1<<53 + 1, 1<<63 - 1}
	for range 2 {
		if got := retainedAPIReadIDs(t, worker, schema, parts); !slices.Equal(got, expected) {
			t.Fatal("immutable replay changed", got)
		}
	}
	statement, err := worker.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetSqlQuery(h.ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if reader, _, err := statement.ExecuteQuery(h.ctx); reader != nil || !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("reader-only SQL was allowed", err)
	}
	descriptor, err := decodeRetainedPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	revoker := retainedAPIConnection(t, storage.namespace, false)
	drop, err := revoker.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer drop.Close(h.ctx)
	if err := drop.SetSqlQuery(h.ctx, "DROP PARTITION RESULT '"+descriptor.Reference.JobID+"'"); err != nil {
		t.Fatal(err)
	}
	if _, err := drop.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	if reader, err := worker.ReadPartition(h.ctx, parts.PartitionIDs[0]); reader != nil || !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("revoked result remained readable", err)
	}
}

func TestRetainedPartitionAPIEmptyAndBudgetFailure(t *testing.T) {
	h := newSQLHarness(t)
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	publisher := retainedAPIConnection(t, storage.namespace, false)
	schema, parts, rows := retainedAPIPlan(t, publisher, "SELECT 1::BIGINT AS id WHERE false")
	if rows != 0 || parts.NumPartitions != 1 {
		t.Fatal(rows, parts.NumPartitions)
	}
	worker := retainedAPIConnection(t, storage.namespace, true)
	if got := retainedAPIReadIDs(t, worker, schema, parts); len(got) != 0 {
		t.Fatal(got)
	}
	statement, err := publisher.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetOption(h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetOption(h.ctx, OptionStringPartitionExecutionLimits, `{"working_bytes":1}`); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetSqlQuery(h.ctx, "SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	if _, parts, rows, err := statement.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusInvalidState) || parts.NumPartitions != 0 || rows != -1 {
		t.Fatal("budget failure published work", parts, rows, err)
	}
}

func TestRetainedPartitionAPIProcessHelper(t *testing.T) {
	raw := os.Getenv("ADBC_RETAINED_PUBLIC_PARTITIONS")
	if raw == "" {
		t.Skip("internal public API subprocess helper")
	}
	var parts adbc.Partitions
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		t.Fatal(err)
	}
	if len(parts.PartitionIDs) == 0 {
		t.Fatal("missing retained test partitions")
	}
	first, err := decodeRetainedPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	opts := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), OptionStringPartitionReaderOnly: "true", OptionStringPartitionResultNamespace: first.Reference.Namespace, OptionStringCluster: os.Getenv("ADBC_RETAINED_PUBLIC_CLUSTER")}
	database, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(context.Background(), retainedTestDatabaseOptions(opts))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(context.Background())
	worker, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(context.Background())
	var expected []int64
	if err := json.Unmarshal([]byte(os.Getenv("ADBC_RETAINED_PUBLIC_EXPECTED")), &expected); err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, part := range parts.PartitionIDs {
		reader, err := worker.ReadPartition(context.Background(), part)
		if err != nil {
			t.Fatal(err)
		}
		for reader.Next() {
			got = append(got, reader.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
		}
		err = reader.Err()
		reader.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(got, expected) {
		t.Fatal("independent public Arrow replay differs")
	}
	println("RETAINED_PUBLIC_PROCESS_OK")
}
func retainedPublicProcess(t *testing.T, uri string, cluster bool, parts adbc.Partitions, expected []int64) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestRetainedPartitionAPIProcessHelper$", "-test.v")
	env := os.Environ()
	filtered := env[:0]
	for _, item := range env {
		if !strings.HasPrefix(item, "REDIS_URI=") {
			filtered = append(filtered, item)
		}
	}
	command.Env = append(filtered, "REDIS_URI="+uri, "ADBC_RETAINED_PUBLIC_PARTITIONS="+retainedJSON(parts), "ADBC_RETAINED_PUBLIC_EXPECTED="+retainedJSON(expected), "ADBC_RETAINED_PUBLIC_CLUSTER="+strconv.FormatBool(cluster))
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "RETAINED_PUBLIC_PROCESS_OK") {
		t.Fatalf("public process replay failed: %v: %s", err, output)
	}
}

func TestRetainedPartitionAPIIndependentProcessAndMinimalACL(t *testing.T) {
	a := newACLHarness(t)
	storage := retainedFixture(t, a.raw, defaultRetainedLimits())
	publisher := retainedAPIConnection(t, storage.namespace, false)
	_, parts, rows := retainedAPIPlan(t, publisher, "SELECT -1::BIGINT AS id UNION ALL SELECT 9007199254740993::BIGINT UNION ALL SELECT 9223372036854775807::BIGINT ORDER BY id")
	if rows != 3 {
		t.Fatal(rows)
	}
	if err := publisher.Close(a.ctx); err != nil {
		t.Fatal(err)
	}
	user := "it_retained_public_" + storage.namespace[3:15]
	rules := []string{"+ping", "+time", "+getrange", "%R~" + storage.prefix + "*"}
	if a.cluster {
		rules = append(rules, "+cluster|slots", "+command", "+asking")
	}
	a.user(user, rules...)
	expected := []int64{-1, 1<<53 + 1, 1<<63 - 1}
	for range 2 {
		retainedPublicProcess(t, a.userURI(user), a.cluster, parts, expected)
	}
	// Neither process can bootstrap catalog writes/reads or access source HASHes.
	a.expectNoDenials(user)
}

func TestRetainedPartitionAPIControlPolicyAdmission(t *testing.T) {
	if os.Getenv("REDIS_RETAINED_POLICY_TEST") != "1" {
		t.Skip("requires explicit owned, unlimited standalone policy fixture")
	}
	h := newSQLHarness(t)
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	client := storage.client
	if _, ok := client.(*goredis.ClusterClient); ok {
		t.Skip("policy mutation is tested only on the owned standalone fixture")
	}
	// Restore the exact setting even after an assertion failure; maxmemory is
	// unchanged, so this checks admission without triggering actual eviction.
	capacity, err := client.ConfigGet(h.ctx, "maxmemory").Result()
	if err != nil || capacity["maxmemory"] != "0" {
		t.Skip("policy mutation requires maxmemory=0 on the explicit owned standalone fixture")
	}
	original, err := client.ConfigGet(h.ctx, "maxmemory-policy").Result()
	if err != nil {
		t.Skip("requires CONFIG policy inspection")
	}
	t.Cleanup(func() {
		if err := client.ConfigSet(h.ctx, "maxmemory-policy", original["maxmemory-policy"]).Err(); err != nil {
			t.Error(err)
		}
	})
	if err := client.ConfigSet(h.ctx, "maxmemory-policy", "allkeys-lru").Err(); err != nil {
		t.Skip("requires CONFIG policy control")
	}
	publisher := retainedAPIConnection(t, storage.namespace, false)
	statement, err := publisher.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetOption(h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetSqlQuery(h.ctx, "SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	if _, parts, _, err := statement.ExecutePartitions(h.ctx); !isStatus(err, adbc.StatusInvalidState) || parts.NumPartitions != 0 {
		t.Fatal("unsafe control eviction policy admitted", err)
	}
}

func TestRetainedPartitionAPIPrepublicationFailure(t *testing.T) {
	for _, failure := range []string{"cancel", "cardinality", "schema", "index", "view", "replace"} {
		t.Run(failure, func(t *testing.T) {
			h := newSQLHarness(t)
			const table = "it_retained_publication_failure"
			const view = "it_retained_publication_view"
			h.exec("DROP VIEW IF EXISTS " + view)
			h.exec("DROP TABLE IF EXISTS " + table)
			h.exec("CREATE TABLE " + table + " (id BIGINT)")
			t.Cleanup(func() {
				h.exec("DROP VIEW IF EXISTS " + view)
				h.exec("DROP TABLE IF EXISTS " + table)
			})
			h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
			query := "SELECT id FROM " + table + " ORDER BY id"
			if failure == "view" {
				h.exec("CREATE VIEW " + view + " AS SELECT id FROM " + table)
				query = "SELECT id FROM " + view + " ORDER BY id"
			}
			storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
			publisher := retainedAPIConnection(t, storage.namespace, false)
			statement, err := publisher.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer statement.Close(h.ctx)
			if err := statement.SetOption(h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
				t.Fatal(err)
			}
			if err := statement.SetSqlQuery(h.ctx, query); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			var job string
			testHookRetainedBeforePublish = func(upload *retainedUpload) {
				job = upload.id
				if len(upload.chunks) == 0 {
					t.Fatal("failure injected before upload")
				}
				switch failure {
				case "cancel":
					cancel()
				case "cardinality":
					h.exec("INSERT INTO " + table + " VALUES (4)")
				case "schema":
					h.exec("ALTER TABLE " + table + " ADD COLUMN added TEXT")
				case "replace":
					// Same schema/cardinality, different physical table identity.
					h.exec("DROP TABLE " + table)
					h.exec("CREATE TABLE " + table + " (id BIGINT)")
					h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
				case "view":
					h.exec("CREATE OR REPLACE VIEW " + view + " AS SELECT id+1 AS id FROM " + table)
				case "index":
					st := &store{client: storage.client}
					meta, err := st.getTable(h.ctx, defaultSchema, table)
					if err != nil {
						t.Fatal(err)
					}
					if err := st.searchDo(h.ctx, meta.index(), "FT.ALTER", meta.index(), "SCHEMA", "ADD", "external_field", "TAG").Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Cleanup(func() { testHookRetainedBeforePublish = nil })
			schema, parts, rows, err := statement.ExecutePartitions(ctx)
			testHookRetainedBeforePublish = nil
			if err == nil || schema != nil || parts.NumPartitions != 0 || rows != -1 || job == "" {
				t.Fatal("partial result escaped failed publication", parts.NumPartitions, rows, err)
			}
			if failure != "cancel" && !isStatus(err, adbc.StatusInvalidState) {
				t.Fatal(err)
			}
			if exists, err := storage.client.Exists(h.ctx, storage.jobKey(job, "manifest")).Result(); err != nil || exists != 0 {
				t.Fatal("failed publication left manifest", exists, err)
			}
			if exists, err := storage.client.Exists(h.ctx, storage.jobKey(job, "chunk:0")).Result(); err != nil || exists != 0 {
				t.Fatal("failed publication left payload", exists, err)
			}
			if bytes, err := storage.client.ZScore(h.ctx, storage.prefix+"quota-bytes", job).Result(); err != nil || bytes != 0 {
				t.Fatal("failed publication retained byte reservation", err)
			}
		})
	}
}

// Retained output freezes the completed capture without promising a source
// snapshot. Exercise an overlapping writer that updates the same number of rows
// before publication: replay must preserve captured values rather than rerun SQL.
func TestRetainedPartitionAPIUpdateAfterCapture(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_retained_update_after_capture"
	h.exec("DROP TABLE IF EXISTS " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT)")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS " + table) })
	h.exec("INSERT INTO " + table + " VALUES (1),(2),(3)")
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	publisher := retainedAPIConnection(t, storage.namespace, false)
	statement, err := publisher.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetOption(h.ctx, OptionStringPartitionResultMode, "retained"); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetSqlQuery(h.ctx, "SELECT id FROM "+table+" ORDER BY id"); err != nil {
		t.Fatal(err)
	}
	updated := false
	testHookRetainedBeforePublish = func(_ *retainedUpload) {
		h.exec("UPDATE " + table + " SET id = id + 10")
		updated = true
	}
	t.Cleanup(func() { testHookRetainedBeforePublish = nil })
	_, parts, rows, err := statement.ExecutePartitions(h.ctx)
	testHookRetainedBeforePublish = nil
	if err != nil || !updated || rows != 3 || parts.NumPartitions == 0 {
		t.Fatal("publication after same-cardinality update", rows, err)
	}
	h.expectRows("SELECT id FROM "+table+" ORDER BY id", "11", "12", "13")
	reader := retainedAPIConnection(t, storage.namespace, true)
	for range 2 {
		var got []int64
		for _, part := range parts.PartitionIDs {
			r, err := reader.ReadPartition(h.ctx, part)
			if err != nil {
				t.Fatal(err)
			}
			for r.Next() {
				got = append(got, r.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
			}
			err = r.Err()
			r.Release()
			if err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Equal(got, []int64{1, 2, 3}) {
			t.Fatal("replay changed after a source update", got)
		}
	}
}

func TestRetainedPartitionAPICoordinatorDeathHelper(t *testing.T) {
	namespace := os.Getenv("ADBC_RETAINED_DIE_NAMESPACE")
	if namespace == "" {
		t.Skip("internal coordinator death helper")
	}
	publisher := retainedAPIConnection(t, namespace, false)
	testHookRetainedBeforePublish = func(upload *retainedUpload) {
		// Exit without defers, simulating loss after payload upload but before
		// publication. Only the random job identity leaves this subprocess.
		println("RETAINED_UNCOMMITTED_JOB=" + upload.id)
		os.Exit(77)
	}
	retainedAPIPlan(t, publisher, "SELECT 9007199254740993::BIGINT AS id")
	t.Fatal("coordinator did not terminate at the publication boundary")
}

func TestRetainedPartitionAPICoordinatorDeathBeforePublication(t *testing.T) {
	h := newSQLHarness(t)
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(h.ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestRetainedPartitionAPICoordinatorDeathHelper$", "-test.v")
	command.Env = append(os.Environ(), "ADBC_RETAINED_DIE_NAMESPACE="+storage.namespace)
	output, err := command.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 77 {
		t.Fatalf("coordinator death helper: %v: %s", err, output)
	}
	job := ""
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "RETAINED_UNCOMMITTED_JOB=") {
			job = strings.TrimPrefix(line, "RETAINED_UNCOMMITTED_JOB=")
		}
	}
	if !retainedIdentity.MatchString(job) {
		t.Fatal("coordinator did not report a job identity")
	}
	if exists, err := storage.client.Exists(h.ctx, storage.jobKey(job, "manifest")).Result(); err != nil || exists != 0 {
		t.Fatal("dead coordinator published a manifest", exists, err)
	}
	for _, suffix := range []string{"meta", "chunk:0"} {
		ttl, err := storage.client.PTTL(h.ctx, storage.jobKey(job, suffix)).Result()
		if err != nil || ttl <= 0 || ttl > 10*time.Minute {
			t.Fatal("abandoned upload lacks its fixed lease", suffix, ttl, err)
		}
	}
	if bytes, err := storage.client.ZScore(h.ctx, storage.prefix+"quota-bytes", job).Result(); err != nil || bytes != float64(storage.limits.JobBytes) {
		t.Fatal("abandoned upload lost its reservation", bytes, err)
	}
	// Namespace-authorized revocation also cleans an abandoned uploader. No
	// source key, catalog record or application index is in the deletion set.
	if err := storage.revoke(h.ctx, job, ""); err != nil {
		t.Fatal(err)
	}
	if exists, err := storage.client.Exists(h.ctx, storage.jobKey(job, "chunk:0")).Result(); err != nil || exists != 0 {
		t.Fatal(exists, err)
	}
}

// Close at the last source-independent read, before the reader joins owners.
// This makes the connection-close/reader-registration boundary deterministic.
type retainedCloseReadHook struct {
	close func()
}

func (h *retainedCloseReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *retainedCloseReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *retainedCloseReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, command goredis.Cmder) error {
		err := next(ctx, command)
		if err == nil && command.Name() == "getrange" && len(command.Args()) > 1 {
			if key, ok := command.Args()[1].(string); ok && strings.HasSuffix(key, ":chunk:0") {
				h.close()
			}
		}
		return err
	}
}
func TestRetainedPartitionAPICloseBeforeReaderRegistration(t *testing.T) {
	h := newSQLHarness(t)
	storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
	publisher := retainedAPIConnection(t, storage.namespace, false)
	_, parts, _ := retainedAPIPlan(t, publisher, "SELECT 1::BIGINT AS id")
	connection := retainedAPIConnection(t, storage.namespace, true)
	statement, err := connection.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	worker := statement.(*statementImpl).conn
	ctx, transport := worker.executionContext(h.ctx, &worker.streams)
	defer transport.finish()
	readStore, err := worker.retainedReadStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	readStore.client.AddHook(&retainedCloseReadHook{close: func() {
		if err := worker.Close(h.ctx); err != nil {
			t.Error(err)
		}
	}})
	reader, err := worker.ReadPartition(ctx, parts.PartitionIDs[0])
	if reader != nil {
		reader.Release()
		t.Fatal("reader escaped a connection closed before registration")
	}
	if !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
}
