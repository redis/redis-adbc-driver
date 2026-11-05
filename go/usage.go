// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Usage analytics deliberately has no access to Redis addresses, credentials,
// query text, error messages, column names, or result values in its wire types.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/google/uuid"
)

const usagePrefix = "adbc.redis.telemetry."
const defaultUsageEndpoint = "https://adbc-telemetry.sa.redislabs.com/v1/usage"
const (
	OptionStringTelemetryInstallationIDFile = usagePrefix + "installation_id_file"
	OptionStringTelemetrySourceHostname     = usagePrefix + "source_hostname"
	OptionStringTelemetryEnabled            = usagePrefix + "enabled"
	OptionStringTelemetryEndpoint           = usagePrefix + "endpoint"
	OptionStringTelemetryToken              = usagePrefix + "token"
	OptionStringTelemetryInstallationID     = usagePrefix + "installation_id"
	OptionStringTelemetryOrganization       = usagePrefix + "organization"
	OptionStringTelemetryTeam               = usagePrefix + "team"
	OptionStringTelemetryApplication        = usagePrefix + "application"
	OptionStringTelemetryEnvironment        = usagePrefix + "environment"
	OptionStringTelemetryUseCase            = usagePrefix + "use_case"
	OptionStringTelemetryIntegration        = usagePrefix + "integration"
	OptionStringTelemetryInterval           = usagePrefix + "interval"
	OptionStringTelemetrySnapshot           = usagePrefix + "snapshot"
	// Explicit bounded delivery for applications about to exit. Not a query path.
	OptionStringTelemetryFlush = usagePrefix + "flush"
)

// Buckets are disjoint, with a final overflow bucket. Durations include client
// fetch time, so these describe observed application latency, not Redis CPU time.
var usageLatencyBounds = []int64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000}

type usageMetric struct {
	Attempts     int64   `json:"attempts"`
	Success      int64   `json:"success"`
	Errors       int64   `json:"errors"`
	Canceled     int64   `json:"canceled"`
	Abandoned    int64   `json:"abandoned"`
	Rows         int64   `json:"rows"`
	Affected     int64   `json:"affected"`
	Latency      []int64 `json:"latency"`
	ReadyLatency []int64 `json:"ready_latency"`
}

// Only fixed classifications, anonymous execution IDs and numeric work counters
// leave the driver. SQL, plans, predicates and relation names are never exported.
type usageExecution struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Features        []string  `json:"features"`
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at"`
	Status          string    `json:"status"`
	DurationMS      int64     `json:"duration_ms"`
	ReadyMS         *int64    `json:"ready_ms"`
	RowsReturned    int64     `json:"rows_returned"`
	RowsAffected    *int64    `json:"rows_affected"`
	RowsExamined    *int64    `json:"rows_examined"`
	IndexedRows     *int64    `json:"indexed_rows"`
	RowsRepresented *int64    `json:"rows_represented"`
}

const usageExecutionLimit = 24

type usageOperationContextKey struct{}

type usageSnapshot struct {
	InstanceID           string                  `json:"instance_id,omitempty"`
	WorkloadIdentity     *usageWorkloadIdentity  `json:"workload_identity,omitempty"`
	InstallationIDSource string                  `json:"installation_id_source,omitempty"`
	SourceHostname       string                  `json:"source_hostname,omitempty"`
	Executions           []usageExecution        `json:"executions,omitempty"`
	SamplesDropped       int64                   `json:"execution_samples_dropped,omitempty"`
	SchemaVersion        int                     `json:"schema_version"`
	SessionID            string                  `json:"session_id"`
	InstallationID       string                  `json:"installation_id"`
	Sequence             uint64                  `json:"sequence"`
	StartedAt            time.Time               `json:"started_at"`
	ObservedAt           time.Time               `json:"observed_at"`
	LastQueryAt          *time.Time              `json:"last_query_at,omitempty"`
	DriverVersion        string                  `json:"driver_version"`
	OS                   string                  `json:"os"`
	Arch                 string                  `json:"arch"`
	Labels               map[string]string       `json:"labels"`
	Connections          int64                   `json:"connections"`
	ConnectionErrors     int64                   `json:"connection_errors"`
	Features             map[string]int64        `json:"features"`
	Metrics              map[string]*usageMetric `json:"metrics"`
	LatencyBounds        []int64                 `json:"latency_bounds_ms"`
	DeliveryFailures     int64                   `json:"delivery_failures"`
}

type usageState struct {
	mu              sync.Mutex
	options         map[string]string
	reporter        *usageTelemetry
	started, closed bool
}

func usageDefault(key string) (string, bool) {
	switch key {
	case OptionStringTelemetryInstallationIDFile:
		directory, err := os.UserConfigDir()
		if err != nil {
			return "", true
		}
		return filepath.Join(directory, "redis-adbc-driver", "installation_id"), true
	case OptionStringTelemetrySourceHostname:
		hostname, _ := os.Hostname()
		return hostname, true
	case OptionStringTelemetryEnabled:
		if v := os.Getenv("REDIS_ADBC_TELEMETRY_DISABLED"); v == "1" || strings.EqualFold(v, "true") {
			return "false", true
		}
		return "true", true
	case OptionStringTelemetryInterval:
		return "60s", true
	case OptionStringTelemetryEndpoint:
		return defaultUsageEndpoint, true
	case OptionStringTelemetryToken, OptionStringTelemetryInstallationID,
		OptionStringTelemetryOrganization, OptionStringTelemetryTeam, OptionStringTelemetryApplication,
		OptionStringTelemetryEnvironment, OptionStringTelemetryUseCase, OptionStringTelemetryIntegration:
		return "", true
	}
	return "", false
}
func (s *usageState) set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := usageDefault(key); !ok {
		return errorf(adbc.StatusInvalidArgument, "unknown or read-only telemetry option")
	}
	if s.started || s.closed {
		return errorf(adbc.StatusInvalidState, "set telemetry options before opening a connection")
	}
	if len(value) > 256 && key != OptionStringTelemetryEndpoint && key != OptionStringTelemetryToken {
		return errorf(adbc.StatusInvalidArgument, "telemetry label exceeds 256 bytes")
	}
	switch key {
	case OptionStringTelemetryEnabled:
		if value != "true" && value != "false" {
			return errorf(adbc.StatusInvalidArgument, "telemetry enabled must be true or false")
		}
	case OptionStringTelemetryInterval:
		d, e := time.ParseDuration(value)
		if e != nil || d < time.Second || d > 24*time.Hour {
			return errorf(adbc.StatusInvalidArgument, "telemetry interval must be between 1s and 24h")
		}
	case OptionStringTelemetryEndpoint:
		if value != "" {
			u, e := url.Parse(value)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
				return errorf(adbc.StatusInvalidArgument, "telemetry endpoint must be HTTPS (HTTP is allowed on loopback), without credentials, query or fragment")
			}
		}
	}
	if s.options == nil {
		s.options = map[string]string{}
	}
	s.options[key] = value
	return nil
}
func (s *usageState) get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == OptionStringTelemetrySnapshot {
		if s.reporter == nil {
			return "{}", nil
		}
		return string(s.reporter.snapshot()), nil
	}
	if key == OptionStringTelemetryToken {
		return "", nil
	}
	def, ok := usageDefault(key)
	if !ok {
		return "", errorf(adbc.StatusInvalidArgument, "unknown telemetry option")
	}
	if v, ok := s.options[key]; ok {
		return v, nil
	}
	return def, nil
}
func (s *usageState) start() (*usageTelemetry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errorf(adbc.StatusInvalidState, "database is closed")
	}
	if s.started {
		return s.reporter, nil
	}
	s.started = true
	// Collection is opt-out, including for a zero-value usageState.
	enabled, ok := s.options[OptionStringTelemetryEnabled]
	if !ok {
		enabled, _ = usageDefault(OptionStringTelemetryEnabled)
	}
	if enabled == "false" {
		return nil, nil
	}
	labels := map[string]string{}
	for _, key := range []string{OptionStringTelemetryOrganization, OptionStringTelemetryTeam, OptionStringTelemetryApplication, OptionStringTelemetryEnvironment, OptionStringTelemetryUseCase, OptionStringTelemetryIntegration} {
		if v := s.options[key]; v != "" {
			labels[strings.TrimPrefix(key, usagePrefix)] = v
		}
	}
	id := s.options[OptionStringTelemetryInstallationID]
	idSource := "explicit"
	if id == "" {
		path, supplied := s.options[OptionStringTelemetryInstallationIDFile]
		if !supplied {
			path, _ = usageDefault(OptionStringTelemetryInstallationIDFile)
		}
		id, idSource = usageInstallationID(path)
	}
	interval := time.Minute
	if v := s.options[OptionStringTelemetryInterval]; v != "" {
		interval, _ = time.ParseDuration(v)
	}
	now := time.Now().UTC()
	endpoint, configured := s.options[OptionStringTelemetryEndpoint]
	if !configured {
		endpoint, _ = usageDefault(OptionStringTelemetryEndpoint)
	}
	hostname, supplied := s.options[OptionStringTelemetrySourceHostname]
	if !supplied {
		hostname, _ = usageDefault(OptionStringTelemetrySourceHostname)
	}
	t := &usageTelemetry{endpoint: endpoint, token: s.options[OptionStringTelemetryToken], stop: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), interval: interval,
		client: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		data:   usageSnapshot{InstanceID: usageRuntimeInstanceID, InstallationIDSource: idSource, SourceHostname: hostname, SchemaVersion: 1, SessionID: uuid.NewString(), InstallationID: id, StartedAt: now, DriverVersion: "unknown", OS: runtime.GOOS, Arch: runtime.GOARCH, Labels: labels, Features: map[string]int64{}, Metrics: map[string]*usageMetric{}, LatencyBounds: usageLatencyBounds}}
	s.reporter = t
	t.identityReady = make(chan struct{})
	if t.endpoint != "" {
		ready := usageDiscoverPlatform()
		go func() {
			<-ready
			t.mu.Lock()
			t.data.WorkloadIdentity = usagePlatformDiscovery.result
			t.mu.Unlock()
			close(t.identityReady)
		}()
		go t.run()
	} else {
		close(t.identityReady)
		close(t.done)
	}
	return t, nil
}
func (s *usageState) close() {
	s.mu.Lock()
	s.closed = true
	t := s.reporter
	s.mu.Unlock()
	if t != nil {
		t.once.Do(func() { close(t.stop) })
	} // no network wait in driver Close
}

type usageTelemetry struct {
	mu              sync.Mutex
	sendMu          sync.Mutex
	data            usageSnapshot
	endpoint, token string
	interval        time.Duration
	client          *http.Client
	stop, done      chan struct{}
	identityReady   chan struct{}
	wake            chan struct{}
	once            sync.Once
}

func (t *usageTelemetry) snapshot() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.Sequence++
	t.data.ObservedAt = time.Now().UTC()
	b, _ := json.Marshal(t.data)
	return b
}
func (t *usageTelemetry) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}
func (t *usageTelemetry) connection(ok bool) {
	t.mu.Lock()
	if ok {
		t.data.Connections++
	} else {
		t.data.ConnectionErrors++
	}
	t.mu.Unlock()
	t.signal()
}
func (t *usageTelemetry) run() {
	defer close(t.done)
	timer := time.NewTicker(t.interval)
	defer timer.Stop()
	// The initial connection report is sent promptly. Later events are batched.
	select {
	case <-t.wake:
		t.send()
	case <-t.stop:
		t.send()
		return
	}
	for {
		select {
		case <-timer.C:
			t.send()
		case <-t.stop:
			t.send()
			return
		}
	}
}
func (t *usageTelemetry) send() { _ = t.deliver(context.Background()) }

func (s *usageState) flush(ctx context.Context) error {
	s.mu.Lock()
	t := s.reporter
	s.mu.Unlock()
	if t == nil || t.endpoint == "" {
		return errorf(adbc.StatusInvalidState, "telemetry reporting is not enabled with an endpoint")
	}
	return t.deliver(ctx)
}

func (t *usageTelemetry) deliver(ctx context.Context) error {
	if t.identityReady != nil {
		select {
		case <-t.identityReady:
		case <-ctx.Done():
			return errorf(adbc.StatusCancelled, "telemetry discovery canceled")
		}
	}
	status := 0
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(t.snapshot()))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if t.token != "" {
			req.Header.Set("Authorization", "Bearer "+t.token)
		}
		var res *http.Response
		res, err = t.client.Do(req)
		if err == nil {
			status = res.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return nil
			}
		}
	}
	t.mu.Lock()
	t.data.DeliveryFailures++
	t.mu.Unlock()
	if status != 0 {
		return errorf(adbc.StatusIO, "telemetry delivery failed (HTTP %d)", status)
	}
	return errorf(adbc.StatusIO, "telemetry delivery failed")
}
func newUsageMetric() *usageMetric {
	return &usageMetric{Latency: make([]int64, len(usageLatencyBounds)+1), ReadyLatency: make([]int64, len(usageLatencyBounds)+1)}
}
func usageBucket(d time.Duration) int {
	ms := d.Milliseconds()
	for i, b := range usageLatencyBounds {
		if ms <= b {
			return i
		}
	}
	return len(usageLatencyBounds)
}

type usageOperation struct {
	data        usageExecution
	diagnostics *executionDiagnostics
	metrics     *partitionMetrics
	indexesMu   sync.Mutex
	indexes     map[string]int64
	t           *usageTelemetry
	kind        string
	started     time.Time
	once        sync.Once
}

func (s *statementImpl) startUsage() *usageOperation {
	if s.conn == nil || s.conn.usage == nil {
		return nil
	}
	t := s.conn.usage
	kind := "unknown"
	features := map[string]bool{}
	if s.ingest.TableName != "" {
		kind = "ingest"
	} else {
		parsed := s.parsed
		if parsed == nil {
			parsed, _ = ParseScript(s.query)
		}
		if len(parsed) > 1 {
			kind = "script"
		} else if len(parsed) == 1 {
			kind = usageKind(parsed[0].Stmt)
		}
		for _, ps := range parsed {
			if sel, ok := ps.Stmt.(*SelectStmt); ok {
				usageSelectFeatures(sel, features)
			}
		}
	}
	now := time.Now().UTC()
	t.mu.Lock()
	if t.data.Metrics[kind] == nil {
		t.data.Metrics[kind] = newUsageMetric()
	}
	t.data.Metrics[kind].Attempts++
	t.data.LastQueryAt = &now
	for f := range features {
		t.data.Features[f]++
	}
	t.mu.Unlock()
	names := make([]string, 0, len(features))
	for feature := range features {
		names = append(names, feature)
	}
	sort.Strings(names)
	return &usageOperation{t: t, kind: kind, started: time.Now(), indexes: map[string]int64{},
		data: usageExecution{ID: uuid.NewString(), Kind: kind, Features: names, StartedAt: now}}
}
func usageKind(st Stmt) string {
	switch st.(type) {
	case *SelectStmt:
		return "select"
	case *InsertStmt:
		return "insert"
	case *UpdateStmt:
		return "update"
	case *DeleteStmt:
		return "delete"
	case *MergeStmt:
		return "merge"
	case *CreateTableStmt, *DropTableStmt, *AlterTableStmt, *CreateSchemaStmt, *DropSchemaStmt, *CreateViewStmt, *DropViewStmt, *TruncateStmt, *CommentStmt:
		return "ddl"
	case *ShowStmt:
		return "metadata"
	case *SetStmt, *TransactionStmt:
		return "session"
	default:
		return "other"
	}
}

// Feature names are a fixed vocabulary. No SQL strings or identifiers escape.
func usageSelectFeatures(s *SelectStmt, f map[string]bool) {
	if s == nil {
		return
	}
	if len(s.Joins) > 0 {
		f["join"] = true
	}
	if s.Where != nil {
		f["filter"] = true
	}
	if len(s.GroupBy) > 0 || s.Having != nil {
		f["aggregate"] = true
	}
	if len(s.OrderBy) > 0 {
		f["sort"] = true
	}
	if s.Limit != nil {
		f["limit"] = true
	}
	if s.Distinct || len(s.DistinctOn) > 0 {
		f["distinct"] = true
	}
	if len(s.With) > 0 {
		f["cte"] = true
	}
	if s.SetOp != nil {
		f["set_operation"] = true
	}
	visit := func(e Expr) {
		walkExpr(e, func(e Expr) {
			switch x := e.(type) {
			case *Func:
				if aggregateFuncs[x.Name] {
					f["aggregate"] = true
				}
			case *WindowFunc:
				f["window"] = true
			case *Subquery:
				f["subquery"] = true
			}
		})
	}
	for _, it := range s.Items {
		visit(it.Expr)
	}
	visit(s.Where)
	visit(s.Having)
	visit(s.Qualify)
	for _, o := range s.OrderBy {
		visit(o.Expr)
	}
}
func (o *usageOperation) ready() {
	o.t.mu.Lock()
	defer o.t.mu.Unlock()
	elapsed := time.Since(o.started)
	o.t.data.Metrics[o.kind].ReadyLatency[usageBucket(elapsed)]++
	ms := elapsed.Milliseconds()
	o.data.ReadyMS = &ms
}
func (o *usageOperation) affected(n int64) {
	if n >= 0 {
		o.t.mu.Lock()
		o.t.data.Metrics[o.kind].Affected += n
		o.data.RowsAffected = &n
		o.t.mu.Unlock()
	}
}
func (o *usageOperation) finish(err error, abandoned bool) {
	o.once.Do(func() {
		o.t.mu.Lock()
		defer o.t.mu.Unlock()
		m := o.t.data.Metrics[o.kind]
		var ae adbc.Error
		switch {
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ae) && ae.Code == adbc.StatusCancelled):
			m.Canceled++
			o.data.Status = "canceled"
		case err != nil:
			m.Errors++
			o.data.Status = "error"
		case abandoned:
			m.Abandoned++
			o.data.Status = "abandoned"
		default:
			m.Success++
			o.data.Status = "success"
		}
		elapsed := time.Since(o.started)
		m.Latency[usageBucket(elapsed)]++
		o.data.DurationMS = elapsed.Milliseconds()
		o.data.CompletedAt = time.Now().UTC()
		if o.diagnostics != nil && o.diagnostics.scanObserved.Load() {
			n := o.diagnostics.processed.Load()
			o.data.RowsExamined = &n
		}
		if o.metrics != nil && o.metrics.used.Load() {
			n := o.metrics.rows.Load()
			o.data.RowsRepresented = &n
		}
		o.indexesMu.Lock()
		if len(o.indexes) > 0 {
			var n int64
			for _, count := range o.indexes {
				n += count
			}
			o.data.IndexedRows = &n
		}
		o.indexesMu.Unlock()
		if len(o.t.data.Executions) == usageExecutionLimit {
			copy(o.t.data.Executions, o.t.data.Executions[1:])
			o.t.data.Executions = o.t.data.Executions[:usageExecutionLimit-1]
			o.t.data.SamplesDropped++
		}
		o.t.data.Executions = append(o.t.data.Executions, o.data)
	})
}

// Embed the original reader to preserve schema/batch access and Arrow ownership.
// Next/RecordReader have the original single-consumer contract; Retain/Release
// remain safe across threads. An early final Release is an abandoned execution.
type usageReader struct {
	array.RecordReader
	op   *usageOperation
	refs int64
}

func (r *usageReader) Retain() { atomic.AddInt64(&r.refs, 1); r.RecordReader.Retain() }
func (r *usageReader) Release() {
	if atomic.AddInt64(&r.refs, -1) == 0 {
		r.op.finish(r.Err(), true)
	}
	r.RecordReader.Release()
}
func (r *usageReader) Next() bool {
	if !r.RecordReader.Next() {
		r.op.finish(r.Err(), false)
		return false
	}
	n := r.RecordBatch().NumRows()
	r.op.t.mu.Lock()
	r.op.t.data.Metrics[r.op.kind].Rows += n
	r.op.data.RowsReturned += n
	r.op.t.mu.Unlock()
	return true
}

var _ array.RecordReader = (*usageReader)(nil)

// Reuse index metadata already fetched for execution readiness. No extra Redis
// commands or table names are added to telemetry. Counts are observed indexed
// documents, not authoritative table cardinality or Redis internal scan work.
func usageIndexSize(ctx context.Context, index string, node any, reply any) {
	op, _ := ctx.Value(usageOperationContextKey{}).(*usageOperation)
	if op == nil {
		return
	}
	fields, ok := redisReplyFields(reply)
	if !ok {
		return
	}
	raw, ok := fields["num_docs"]
	if !ok {
		return
	}
	n, err := strconv.ParseInt(fmt.Sprint(raw), 10, 64)
	if err != nil || n < 0 {
		return
	}
	op.indexesMu.Lock()
	op.indexes[index+fmt.Sprintf("/%p", node)] = n
	op.indexesMu.Unlock()
}

// Publish a complete ID file atomically, so concurrent database handles/processes
// all adopt the same value. A read-only/invalid path must never prevent queries.
func usageInstallationID(path string) (string, string) {
	read := func() string {
		file, err := os.Open(path)
		if err != nil {
			return ""
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, 128))
		if err != nil {
			return ""
		}
		id, err := uuid.Parse(strings.TrimSpace(string(raw)))
		if err != nil {
			return ""
		}
		return id.String()
	}
	if id := read(); id != "" {
		return id, "local_file"
	}
	if path == "" || os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return uuid.NewString(), "temporary"
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".installation-id-*")
	if err != nil {
		return uuid.NewString(), "temporary"
	}
	defer os.Remove(file.Name())
	id := uuid.NewString()
	_, writeErr := file.WriteString(id + "\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return uuid.NewString(), "temporary"
	}
	if os.Link(file.Name(), path) == nil {
		return id, "local_file"
	}
	// Another process won publication; read its already complete value.
	if existing := read(); existing != "" {
		return existing, "local_file"
	}
	return uuid.NewString(), "temporary"
}
