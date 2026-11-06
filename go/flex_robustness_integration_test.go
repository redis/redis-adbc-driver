// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Faults wrap the verified TLS connection returned by go-redis's own dialer.
// They act only on a later FT.SEARCH response, after rows have been delivered.
// No TLS verification, server data or server configuration is changed.
type flexReadFault struct {
	armed   atomic.Bool
	fired   atomic.Int64
	mode    string
	started chan struct{}
}
type flexFaultSocket struct {
	net.Conn
	fault   *flexReadFault
	pending atomic.Bool
	closed  chan struct{}
	once    sync.Once
}

func (c *flexFaultSocket) Write(b []byte) (int, error) {
	if bytes.Contains(bytes.ToUpper(b), []byte("FT.SEARCH")) && c.fault.armed.CompareAndSwap(true, false) {
		c.pending.Store(true)
	}
	return c.Conn.Write(b)
}
func (c *flexFaultSocket) Read(b []byte) (int, error) {
	if c.pending.CompareAndSwap(true, false) {
		c.fault.fired.Add(1)
		close(c.fault.started)
		switch c.fault.mode {
		case "disconnect":
			_ = c.Close()
			return 0, io.ErrUnexpectedEOF
		case "timeout":
			// Let the real socket deadline expire before permitting the next read.
			select {
			case <-time.After(200 * time.Millisecond):
			case <-c.closed:
				return 0, net.ErrClosed
			}
		case "cancel":
			<-c.closed
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Read(b)
}
func (c *flexFaultSocket) Close() error { c.once.Do(func() { close(c.closed) }); return c.Conn.Close() }

func TestFlexReadRobustness(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to an isolated Software/Cloud Flex database")
	}
	// Reuse the normal ADBC database opener; measurement helpers do not alter
	// execution. This test can run independently of the opt-in benchmark.
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_read_faults"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("INSERT INTO " + name + " SELECT i FROM generate_series(1,25000) AS t(i)")
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "robustness fixture indexed", func() bool { n, err := c.store.countMatches(h.ctx, meta.index(), "*"); return err == nil && n == 25000 })
	for _, mode := range []string{"disconnect", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			options := h.conn.(adbc.GetSetOptionsWithContext)
			timeout := "5s"
			if mode == "timeout" {
				timeout = "50ms"
			}
			if err := options.SetOption(h.ctx, OptionStringReadTimeout, timeout); err != nil {
				t.Fatal(err)
			}
			original := c.clientCfg.single.Dialer
			dial := original
			if dial == nil {
				dial = goredis.NewDialer(c.clientCfg.single)
			}
			fault := &flexReadFault{mode: mode, started: make(chan struct{})}
			c.clientCfg.single.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dial(ctx, network, address)
				if err != nil {
					return nil, err
				}
				return &flexFaultSocket{Conn: conn, fault: fault, closed: make(chan struct{})}, nil
			}
			defer func() {
				c.clientCfg.single.Dialer = original
				_ = options.SetOption(h.ctx, OptionStringReadTimeout, "5s")
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			st, err := h.conn.NewStatement(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetOption(ctx, OptionIntStreamBatchRows, "2"); err != nil {
				t.Fatal(err)
			}
			if err := st.SetSqlQuery(ctx, "SELECT n FROM "+name); err != nil {
				t.Fatal(err)
			}
			reader, _, err := st.ExecuteQuery(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if reader != nil {
					reader.Release()
				}
			}()
			// ExecuteQuery must have fetched a complete initial batch before the fault.
			if !reader.Next() {
				t.Fatal("no initial batch", reader.Err())
			}
			delivered := int(reader.RecordBatch().NumRows())
			fault.armed.Store(true)
			done := make(chan error, 1)
			go func() {
				for reader.Next() {
					delivered += int(reader.RecordBatch().NumRows())
				}
				done <- reader.Err()
			}()
			select {
			case <-fault.started:
			case <-time.After(5 * time.Second):
				t.Fatal("fault never reached a range read")
			}
			started := time.Now()
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failed read returned success")
				}
				if mode == "cancel" && !isStatus(err, adbc.StatusCancelled) {
					t.Fatalf("cancel status: %v", err)
				}
				if mode == "timeout" && !strings.Contains(strings.ToLower(err.Error()), "timeout") {
					t.Fatalf("timeout diagnostic: %v", err)
				}
				if mode == "disconnect" && !isStatus(err, adbc.StatusIO) {
					t.Fatalf("disconnect status: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("failed read did not terminate promptly")
			}
			if delivered >= 25000 || delivered == 0 || fault.fired.Load() != 1 {
				t.Fatalf("fault delivery accounting: rows=%d fired=%d", delivered, fault.fired.Load())
			}
			if reader.Next() {
				t.Fatal("terminal reader resumed after failure")
			}
			t.Logf("%s: delivered=%d, terminal lag=%s, faults=%d", mode, delivered, time.Since(started), fault.fired.Load())
			reader.Release()
			reader = nil
			// A whole new SQL statement is the retry contract. Check every row once;
			// partial results from the failed statement must not leak into the retry.
			var want []string
			for i := 1; i <= 25000; i++ {
				want = append(want, strconv.Itoa(i))
			}
			h.expectRows("SELECT n FROM "+name, want...)
		})
	}
	h.expectRows("SELECT count(*),sum(n) FROM "+name, fmt.Sprint("25000|312512500"))
}

// This opt-in test restarts only a caller-designated disposable Software DB.
// It requests a data-preserving restart; it does not attempt HA failover.
func TestFlexServerRestart(t *testing.T) { flexServerTransition(t, "restart") }

// Run only on a caller-designated replicated multi-node Software Flex DB.
func TestFlexServerFailover(t *testing.T) { flexServerTransition(t, "failover") }

func flexServerTransition(t *testing.T, operation string) {
	prefix := "REDIS_FLEX_" + strings.ToUpper(operation)
	container := os.Getenv(prefix + "_CONTAINER")
	dbid := os.Getenv(prefix + "_DB")
	if container == "" || dbid == "" {
		t.Skip("set caller-owned " + prefix + "_CONTAINER and " + prefix + "_DB")
	}
	if n, err := strconv.Atoi(dbid); err != nil || n < 1 {
		t.Fatal("invalid disposable database ID")
	}
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Fatal("REDIS_FLEX_URI is required")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_" + operation
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		st, err := h.conn.NewStatement(ctx)
		if err != nil {
			t.Errorf("transition cleanup statement: %v", err)
			return
		}
		defer st.Close(ctx)
		_ = st.SetSqlQuery(ctx, "DROP TABLE IF EXISTS "+name)
		if _, err = st.ExecuteUpdate(ctx); err != nil {
			t.Errorf("transition cleanup failed: %v", err)
		}
	}()
	h.exec("INSERT INTO " + name + " SELECT i FROM generate_series(1,25000) AS t(i)")
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "transition rows indexed", func() bool { n, err := c.store.countMatches(h.ctx, meta.index(), "*"); return err == nil && n == 25000 })
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionIntStreamBatchRows, "2"); err != nil {
		t.Fatal(err)
	}

	_ = st.SetSqlQuery(h.ctx, "SELECT n FROM "+name)
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatal("missing pre-restart batch", reader.Err())
	}
	seen := make([]bool, 25001)
	count := 0
	check := func() {
		rec := reader.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			n, err := strconv.Atoi(rec.Column(0).ValueStr(i))
			if err != nil || n < 1 || n > 25000 || seen[n] {
				t.Fatal("invalid/duplicate transition row")
			}
			seen[n] = true
			count++
		}
	}
	check()
	restartCtx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	defer cancel()
	started := time.Now()
	args := []string{"exec", container, "rladmin", "-y", operation, "db", "db:" + dbid}
	if operation == "failover" {
		shards := strings.Fields(os.Getenv(prefix + "_SHARDS"))
		if len(shards) == 0 {
			t.Fatal("explicit owned failover shard IDs are required")
		}
		args = append(args, "shard")
		for _, shard := range shards {
			if n, err := strconv.Atoi(shard); err != nil || n < 1 {
				t.Fatal("invalid failover shard ID")
			}
			args = append(args, shard)
		}
	}
	cmd := exec.CommandContext(restartCtx, "docker", args...)
	cmd.WaitDelay = 2 * time.Second
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("disposable transition failed: %v (output length %d)", err, len(output))
	}
	for reader.Next() {
		check()
	}
	err = reader.Err()
	if err == nil && count != 25000 {
		t.Fatalf("transition silently truncated rows: %d", count)
	}
	if err != nil && reader.Next() {
		t.Fatal("transition error was not terminal")
	}
	t.Logf("%s completed in %s; old reader rows=%d, terminal_error=%t", operation, time.Since(started), count, err != nil)
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, e := h.conn.NewStatement(h.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_ = s.SetSqlQuery(h.ctx, "SELECT count(*),sum(n) FROM "+name)
		r, _, e := s.ExecuteQuery(h.ctx)
		if e == nil {
			var got []string
			for r.Next() {
				rec := r.RecordBatch()
				for i := 0; i < int(rec.NumRows()); i++ {
					got = append(got, rec.Column(0).ValueStr(i)+"|"+rec.Column(1).ValueStr(i))
				}
			}
			e = r.Err()
			r.Release()
			_ = s.Close(h.ctx)
			if e == nil {
				if len(got) != 1 || got[0] != "25000|312512500" {
					t.Fatal("post-transition data mismatch", got)
				}
				break
			}
		}
		if e != nil {
			_ = s.Close(h.ctx)
		}
		if time.Now().After(deadline) {
			t.Fatal("post-transition query did not recover", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var want []string
	for n := 1; n <= 25000; n++ {
		want = append(want, strconv.Itoa(n))
	}
	h.expectRows("SELECT n FROM "+name, want...)
}

// Run only after an operator lowers the owned fixture's Search result cap.
// This tests actual server limit discovery, not the synthetic reply-cap hook.
func TestFlexLiveSearchCap(t *testing.T) {
	capText := os.Getenv("REDIS_FLEX_EXPECT_SEARCH_CAP")
	if capText == "" {
		t.Skip("set REDIS_FLEX_EXPECT_SEARCH_CAP after configuring the owned fixture")
	}
	expected, err := strconv.ParseInt(capText, 10, 64)
	if err != nil || expected < 2 {
		t.Fatal("cap must be at least 2 for the capability probe")
	}
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Fatal("REDIS_FLEX_URI is required")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_live_cap"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("INSERT INTO " + name + " SELECT i FROM generate_series(1,100) AS t(i)")
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "live-cap rows indexed", func() bool { n, err := c.store.countMatches(h.ctx, meta.index(), "*"); return err == nil && n == 100 })
	caps := c.store.paginationCaps(h.ctx, meta.index())
	t.Logf("server Search cap=%d, configured expectation=%d", caps[0], expected)
	if caps[0] != expected {
		t.Fatalf("configured Search cap was not observed: %d", caps[0])
	}
	var want []string
	for n := 1; n <= 100; n++ {
		want = append(want, strconv.Itoa(n))
	}
	h.expectRows("SELECT n FROM "+name, want...)
}

func TestFlexRowIDBoundary(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a disposable Flex database")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_id_boundary"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("INSERT INTO " + name + " VALUES (10)")
	if err := c.store.client.Set(h.ctx, seqKey(defaultSchema, name), exactDoubleLimit-2, 0).Err(); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO " + name + " VALUES (20),(30)")
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "boundary rows indexed", func() bool { n, err := c.store.countMatches(h.ctx, meta.index(), "*"); return err == nil && n == 3 })
	h.expectRows("SELECT n FROM "+name, "10", "20", "30")
	cur, err := c.store.openRangeReader(h.ctx, &aggRequest{meta: meta, index: meta.index(), query: "*", load: []string{"n"}})
	if err != nil {
		t.Fatal(err)
	}
	defer cur.close(h.ctx)
	cur.bounded.page = 2
	var boundaryIDs []string
	for {
		rows, err := cur.next(h.ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			boundaryIDs = append(boundaryIDs, row[rowIDField])
		}
	}
	wantIDs := []string{"1", strconv.FormatInt(exactDoubleLimit-1, 10), strconv.FormatInt(exactDoubleLimit, 10)}
	if !slices.Equal(boundaryIDs, wantIDs) {
		t.Fatalf("boundary split lost/duplicated IDs: %v", boundaryIDs)
	}

	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	_ = st.SetSqlQuery(h.ctx, "INSERT INTO "+name+" VALUES (40)")
	if _, err := st.ExecuteUpdate(h.ctx); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatalf("limit insertion: %v", err)
	}
	seq, err := c.store.client.Get(h.ctx, seqKey(defaultSchema, name)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if seq != exactDoubleLimit {
		t.Fatalf("rejected limit insertion advanced the sequence to %d and made existing rows unreadable", seq)
	}
	h.expectRows("SELECT n FROM "+name, "10", "20", "30")
}

func TestFlexConcurrentRowIDLimit(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a disposable Flex database")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_id_race"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.client.Set(h.ctx, seqKey(defaultSchema, name), exactDoubleLimit-1, 0).Err(); err != nil {
		t.Fatal(err)
	}
	type result struct {
		last int64
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { last, _, err := c.store.allocateFlexRowIDs(h.ctx, meta, 1); results <- result{last, err} }()
	}
	accepted, rejected := 0, 0
	for range 2 {
		r := <-results
		if r.err == nil {
			if r.last != exactDoubleLimit {
				t.Fatalf("incorrect final allocation: %d", r.last)
			}
			accepted++
		} else if isStatus(r.err, adbc.StatusNotImplemented) {
			rejected++
		} else {
			t.Fatal(r.err)
		}
	}
	seq, err := c.store.client.Get(h.ctx, seqKey(defaultSchema, name)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || rejected != 1 || seq != exactDoubleLimit {
		t.Fatalf("non-atomic boundary: accepted=%d rejected=%d seq=%d", accepted, rejected, seq)
	}
}
