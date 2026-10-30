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
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/google/uuid"
)

const usagePrefix = "adbc.redis.telemetry."
const (
	OptionStringTelemetryEnabled        = usagePrefix + "enabled"
	OptionStringTelemetryEndpoint       = usagePrefix + "endpoint"
	OptionStringTelemetryToken          = usagePrefix + "token"
	OptionStringTelemetryInstallationID = usagePrefix + "installation_id"
	OptionStringTelemetryOrganization   = usagePrefix + "organization"
	OptionStringTelemetryTeam           = usagePrefix + "team"
	OptionStringTelemetryApplication    = usagePrefix + "application"
	OptionStringTelemetryEnvironment    = usagePrefix + "environment"
	OptionStringTelemetryUseCase        = usagePrefix + "use_case"
	OptionStringTelemetryIntegration    = usagePrefix + "integration"
	OptionStringTelemetryInterval       = usagePrefix + "interval"
	OptionStringTelemetrySnapshot       = usagePrefix + "snapshot"
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
type usageSnapshot struct {
	SchemaVersion    int                     `json:"schema_version"`
	SessionID        string                  `json:"session_id"`
	InstallationID   string                  `json:"installation_id"`
	Sequence         uint64                  `json:"sequence"`
	StartedAt        time.Time               `json:"started_at"`
	ObservedAt       time.Time               `json:"observed_at"`
	LastQueryAt      *time.Time              `json:"last_query_at,omitempty"`
	DriverVersion    string                  `json:"driver_version"`
	OS               string                  `json:"os"`
	Arch             string                  `json:"arch"`
	Labels           map[string]string       `json:"labels"`
	Connections      int64                   `json:"connections"`
	ConnectionErrors int64                   `json:"connection_errors"`
	Features         map[string]int64        `json:"features"`
	Metrics          map[string]*usageMetric `json:"metrics"`
	LatencyBounds    []int64                 `json:"latency_bounds_ms"`
	DeliveryFailures int64                   `json:"delivery_failures"`
}

type usageState struct {
	mu              sync.Mutex
	options         map[string]string
	reporter        *usageTelemetry
	started, closed bool
}

func usageDefault(key string) (string, bool) {
	switch key {
	case OptionStringTelemetryEnabled:
		return "false", true
	case OptionStringTelemetryInterval:
		return "60s", true
	case OptionStringTelemetryEndpoint, OptionStringTelemetryToken, OptionStringTelemetryInstallationID,
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
	if s.options[OptionStringTelemetryEnabled] != "true" {
		return nil, nil
	}
	labels := map[string]string{}
	for _, key := range []string{OptionStringTelemetryOrganization, OptionStringTelemetryTeam, OptionStringTelemetryApplication, OptionStringTelemetryEnvironment, OptionStringTelemetryUseCase, OptionStringTelemetryIntegration} {
		if v := s.options[key]; v != "" {
			labels[strings.TrimPrefix(key, usagePrefix)] = v
		}
	}
	id := s.options[OptionStringTelemetryInstallationID]
	if id == "" {
		id = uuid.NewString()
	}
	interval := time.Minute
	if v := s.options[OptionStringTelemetryInterval]; v != "" {
		interval, _ = time.ParseDuration(v)
	}
	now := time.Now().UTC()
	t := &usageTelemetry{endpoint: s.options[OptionStringTelemetryEndpoint], token: s.options[OptionStringTelemetryToken], stop: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), interval: interval,
		client: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		data:   usageSnapshot{SchemaVersion: 1, SessionID: uuid.NewString(), InstallationID: id, StartedAt: now, DriverVersion: "unknown", OS: runtime.GOOS, Arch: runtime.GOARCH, Labels: labels, Features: map[string]int64{}, Metrics: map[string]*usageMetric{}, LatencyBounds: usageLatencyBounds}}
	s.reporter = t
	if t.endpoint != "" {
		go t.run()
	} else {
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
	t       *usageTelemetry
	kind    string
	started time.Time
	once    sync.Once
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
	return &usageOperation{t: t, kind: kind, started: time.Now()}
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
	o.t.data.Metrics[o.kind].ReadyLatency[usageBucket(time.Since(o.started))]++
}
func (o *usageOperation) affected(n int64) {
	if n > 0 {
		o.t.mu.Lock()
		o.t.data.Metrics[o.kind].Affected += n
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
		case err != nil:
			m.Errors++
		case abandoned:
			m.Abandoned++
		default:
			m.Success++
		}
		m.Latency[usageBucket(time.Since(o.started))]++
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
	r.op.t.mu.Unlock()
	return true
}

var _ array.RecordReader = (*usageReader)(nil)
