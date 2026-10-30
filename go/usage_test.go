// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func usageForTest(t *testing.T, opts map[string]string) (*usageState, *usageTelemetry) {
	t.Helper()
	s := &usageState{}
	if opts == nil {
		opts = map[string]string{OptionStringTelemetryEnabled: "true"}
	}
	for k, v := range opts {
		if err := s.set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	return s, u
}
func usageData(t *testing.T, u *usageTelemetry) usageSnapshot {
	t.Helper()
	var p usageSnapshot
	if err := json.Unmarshal(u.snapshot(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func usageStatement(t *testing.T, u *usageTelemetry, query string) *statementImpl {
	t.Helper()
	db, err := newDriverImpl(memory.NewGoAllocator()).newDatabaseImpl(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := db.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := &connectionImpl{clientCfg: cfg, usage: u, store: &store{}, sess: newSession(defaultSchema), workerBudget: &workerBudget{}, ConnectionImplBase: driverbase.NewConnectionImplBase(&db.DatabaseImplBase)}
	return &statementImpl{conn: c, query: query, StatementImplBase: driverbase.NewStatementImplBase(&c.ConnectionImplBase, c.ErrorHelper)}
}
func TestUsageDisabledAndValidation(t *testing.T) {
	s, u := usageForTest(t, map[string]string{})
	if u != nil {
		t.Fatal("telemetry must default off")
	}
	if got, _ := s.get(OptionStringTelemetrySnapshot); got != "{}" {
		t.Fatal(got)
	}
	for _, endpoint := range []string{"http://example.com/v1/usage", "https://user:secret@example.com", "https://example.com?token=secret", "file:///tmp/log"} {
		var s usageState
		if s.set(OptionStringTelemetryEndpoint, endpoint) == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	for _, interval := range []string{"0s", "-1s", "25h", "not-a-duration"} {
		var s usageState
		if s.set(OptionStringTelemetryInterval, interval) == nil {
			t.Error(interval)
		}
	}
	var x usageState
	if x.set(OptionStringTelemetryToken, "secret") != nil {
		t.Fatal("set token")
	}
	if got, _ := x.get(OptionStringTelemetryToken); got != "" {
		t.Fatal("token exposed")
	}
	if err := s.set(OptionStringTelemetryEnabled, "true"); err == nil {
		t.Fatal("configuration changed after open")
	}
	s.close()
	if _, err := s.start(); err == nil {
		t.Fatal("reopened closed telemetry")
	}
}
func TestUsageQueryLifecycleAndPrivacy(t *testing.T) {
	_, u := usageForTest(t, nil)
	s := usageStatement(t, u, "SELECT 'super-secret' AS private_column")
	r, _, err := s.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usageData(t, u).Metrics["select"].Success != 0 {
		t.Fatal("success before fetch")
	}
	r.Retain()
	if !r.Next() || r.RecordBatch().NumRows() != 1 {
		t.Fatal("missing original data")
	}
	if r.Next() || r.Err() != nil {
		t.Fatal("fetch failed")
	}
	r.Release()
	r.Release()
	m := usageData(t, u).Metrics["select"]
	if m.Attempts != 1 || m.Success != 1 || m.Abandoned != 0 || m.Rows != 1 {
		t.Fatalf("metrics %+v", m)
	}
	r, _, err = s.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	m = usageData(t, u).Metrics["select"]
	if m.Attempts != 2 || m.Abandoned != 1 || m.Success != 1 {
		t.Fatalf("abandoned %+v", m)
	}
	s.query = "SELECT nonexistent_secret_function('private-value')"
	s.parsed = nil
	if _, _, err = s.ExecuteQuery(context.Background()); err == nil {
		t.Fatal("expected query failure")
	}
	if usageData(t, u).Metrics["select"].Errors != 1 {
		t.Fatal("missing failure")
	}
	payload := string(u.snapshot())
	for _, secret := range []string{"super-secret", "private_column", "private-value", "nonexistent_secret_function"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
}

type failingUsageReader struct {
	array.RecordReader
	err error
}

func (r *failingUsageReader) Next() bool { return false }
func (r *failingUsageReader) Err() error { return r.err }
func TestUsageReaderLateErrorsAndCancellation(t *testing.T) {
	for _, err := range []error{errors.New("private error"), context.Canceled, stateErrorf(stateQueryCanceled, "private cancellation")} {
		_, u := usageForTest(t, nil)
		op := usageStatement(t, u, "SELECT 1").startUsage()
		inner, e := array.NewRecordReader(arrow.NewSchema(nil, nil), nil)
		if e != nil {
			t.Fatal(e)
		}
		r := &usageReader{RecordReader: &failingUsageReader{inner, err}, op: op, refs: 1}
		if r.Next() {
			t.Fatal("unexpected row")
		}
		r.Release()
		m := usageData(t, u).Metrics["select"]
		if m.Success != 0 || m.Errors+m.Canceled != 1 || m.Abandoned != 0 {
			t.Fatalf("late error %+v", m)
		}
		if strings.Contains(string(u.snapshot()), "private") {
			t.Fatal("error text leaked")
		}
	}
}
func TestUsageFeaturesAndConcurrency(t *testing.T) {
	_, u := usageForTest(t, nil)
	query := "SELECT a.secret, COUNT(*) FROM sensitive a JOIN hidden b ON a.id=b.id WHERE a.secret='secret' GROUP BY a.secret ORDER BY a.secret LIMIT 10"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &statementImpl{conn: &connectionImpl{usage: u}, query: query}
			for j := 0; j < 20; j++ {
				op := s.startUsage()
				op.ready()
				op.finish(nil, false)
				u.snapshot()
			}
		}()
	}
	wg.Wait()
	d := usageData(t, u)
	if d.Metrics["select"].Attempts != 400 || d.Metrics["select"].Success != 400 {
		t.Fatal(d.Metrics)
	}
	for _, f := range []string{"join", "aggregate", "filter", "sort", "limit"} {
		if d.Features[f] != 400 {
			t.Fatal(d.Features)
		}
	}
}
func TestUsageReportingRetriesAndNoRedirect(t *testing.T) {
	var calls atomic.Int64
	got := make(chan usageSnapshot, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing auth")
		}
		var p usageSnapshot
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		got <- p
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", 503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	s, u := usageForTest(t, map[string]string{OptionStringTelemetryEnabled: "true", OptionStringTelemetryEndpoint: server.URL, OptionStringTelemetryToken: "token", OptionStringTelemetryInstallationID: "stable-id"})
	op := usageStatement(t, u, "SELECT 1").startUsage()
	op.finish(nil, false)
	// Send directly to make failure/retry deterministic; start() hasn't signaled.
	u.send()
	if err := s.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, b := <-got, <-got
	if a.SessionID != b.SessionID || b.Sequence <= a.Sequence || b.Metrics["select"].Attempts != 1 || b.DeliveryFailures != 1 || b.InstallationID != "stable-id" {
		t.Fatalf("retry reports %+v %+v", a, b)
	}
	s.close()
	select {
	case <-u.done:
	case <-time.After(4 * time.Second):
		t.Fatal("reporter leaked")
	}
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, v := usageForTest(t, nil)
	v.endpoint = redirect.URL
	v.send()
	if targetCalls.Load() != 0 {
		t.Fatal("followed redirect")
	}
}

// Real cursor/ingestion coverage; uses only the explicitly supplied disposable
// REDIS_URI, like the rest of the SQL integration suite.
func TestUsageRedisIntegration(t *testing.T) {
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to a disposable Redis Software database")
	}
	ctx := context.Background()
	opts := map[string]string{adbc.OptionKeyURI: uri, OptionStringTelemetryEnabled: "true", OptionStringTelemetryInstallationID: "integration-test"}
	if endpoint := os.Getenv("REDIS_ADBC_TEST_TELEMETRY_ENDPOINT"); endpoint != "" {
		opts[OptionStringTelemetryEndpoint] = endpoint
		opts[OptionStringTelemetryToken] = os.Getenv("REDIS_ADBC_TEST_TELEMETRY_TOKEN")
	}
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(ctx) })
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	h := &sqlHarness{t: t, ctx: ctx, conn: conn}
	const table = "it_usage_analytics"
	h.setupStreamTable(table, 250)
	t.Cleanup(func() { h.dropTables(table) })
	st, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(ctx)
	if err = st.SetOption(ctx, OptionIntStreamBatchRows, "10"); err != nil {
		t.Fatal(err)
	}
	if err = st.SetSqlQuery(ctx, "SELECT id FROM "+table); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows int64
	for r.Next() {
		rows += r.RecordBatch().NumRows()
	}
	err = r.Err()
	r.Release()
	if err != nil || rows != 250 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
	r, _, err = st.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() {
		t.Fatal("expected first batch")
	}
	r.Release()
	r, _, err = st.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = canceler(t, st).Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	for r.Next() {
	}
	if r.Err() == nil {
		t.Fatal("cancellation lost")
	}
	r.Release()
	raw, err := conn.(adbc.GetSetOptionsWithContext).GetOption(ctx, OptionStringTelemetrySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var d usageSnapshot
	if err = json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	m := d.Metrics["select"]
	if m.Attempts != 3 || m.Success != 1 || m.Abandoned != 1 || m.Canceled != 1 || m.Rows != 260 {
		t.Fatalf("stream metrics %+v", m)
	}
	if d.Metrics["ingest"].Success != 1 || d.Metrics["ingest"].Affected != 250 || d.Connections != 1 {
		t.Fatalf("ingestion/connection metrics %+v", d)
	}
	if opts[OptionStringTelemetryEndpoint] != "" {
		if err := db.SetOption(ctx, OptionStringTelemetryFlush, "true"); err != nil {
			t.Fatal(err)
		}
	}
}
