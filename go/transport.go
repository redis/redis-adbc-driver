// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

type transportContextKey struct{}

// An execution owns these sockets through its final streamed result. Closing
// them cannot affect another statement, background catalog maintenance or a
// pooled connection used by the connection's ordinary metadata operations.
type executionTransport struct {
	owner       *store
	client      goredis.UniversalClient
	ctx         context.Context
	cancel      context.CancelCauseFunc
	mu          sync.Mutex
	pool        *executionPool
	failed      bool
	commands    int
	interrupted bool
	finishing   bool
	background  map[<-chan struct{}]struct{}
	once        sync.Once
	finished    sync.Once
	done        chan struct{}
	owners      []*streams
	matviews    *matviewReads
}

// A completed execution can lend its fully drained pool to the next execution.
// The lease (context, cancellation and stream ownership) is never reused. Pools
// are exclusive while active, including while a streamed result remains open.
// Keeping one idle pool per connection bounds retained sockets; concurrent
// executions allocate independent pools and cannot interrupt each other.
type executionPool struct {
	client   goredis.UniversalClient
	cfg      *clientConfig
	timeouts timeouts
	mu       sync.Mutex
	active   *executionTransport
	sockets  map[*executionSocket]bool
}

type executionSocket struct {
	net.Conn
	pool *executionPool
}

func (c *executionSocket) Close() error {
	c.pool.mu.Lock()
	delete(c.pool.sockets, c)
	c.pool.mu.Unlock()
	return c.Conn.Close()
}

func (p *executionPool) interrupt(t *executionTransport) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active != t {
		return
	}
	// Detachment and interruption must be atomic with respect to each other.
	// Closing a copied socket list after unlocking could otherwise interrupt a
	// subsequent lease of this same pool. Close the underlying connection here;
	// the wrapper's eventual Close is idempotent and removes no new socket.
	for socket := range p.sockets {
		delete(p.sockets, socket)
		_ = socket.Conn.Close()
	}
}

func (s *store) closeIdleExecutionPool() {
	s.mu.Lock()
	p := s.idleExecution
	s.idleExecution = nil
	s.mu.Unlock()
	if p != nil {
		_ = p.client.Close()
	}
}

func (t *executionTransport) interrupt() {
	t.mu.Lock()
	t.interrupted = true
	t.mu.Unlock()
	if t.pool != nil {
		t.pool.interrupt(t)
	}
}
func (t *executionTransport) closeWith(err error) {
	t.cancel(err)
	t.interrupt()
	// Close waits for command machinery; never call it inside a process hook.
	go t.closeClient(false)
}
func (t *executionTransport) closeClient(allowReuse bool) {
	t.once.Do(func() {
		t.mu.Lock()
		t.finishing = true
		pending := make([]<-chan struct{}, 0, len(t.background))
		for done := range t.background {
			pending = append(pending, done)
		}
		reuse := allowReuse && t.pool != nil && !t.interrupted && !t.failed && t.ctx.Err() == nil && len(pending) == 0 && t.commands == 0
		t.mu.Unlock()
		t.cancel(nil)
		if !reuse {
			t.interrupt()
		}
		for _, done := range pending {
			<-done
		}
		if t.matviews != nil {
			t.matviews.close()
		}
		for _, owner := range t.owners {
			owner.remove(t)
		}
		if t.pool != nil {
			t.pool.mu.Lock()
			t.mu.Lock()
			reuse = reuse && !t.interrupted && !t.failed
			t.mu.Unlock()
			t.pool.active = nil
			t.pool.mu.Unlock()
			// A Close racing completion marks the store before checking active owners.
			// Never park a client after closure, and never keep more than one idle pool.
			if reuse && t.owner != nil {
				t.owner.mu.Lock()
				if !t.owner.closed && t.owner.idleExecution == nil {
					t.owner.idleExecution = t.pool
					t.owner.mu.Unlock()
					return
				}
				t.owner.mu.Unlock()
			}
		}
		_ = t.client.Close()
	})
}

// Socket interruption may finish before SQL/Arrow unwinds its abort and reader
// lease defers. Keep that operation registered until its normal owner finishes.
func (t *executionTransport) finish() {
	t.closeClient(true)
	t.finished.Do(func() {
		if t.owner != nil {
			t.owner.mu.Lock()
			delete(t.owner.executions, t)
			t.owner.mu.Unlock()
		}
		if t.done != nil {
			close(t.done)
		}
	})
}

func (s *store) activeExecutions() []*executionTransport {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*executionTransport, 0, len(s.executions))
	for execution := range s.executions {
		out = append(out, execution)
	}
	return out
}

// Registration and finish share mu, so a worker cannot start after the pool
// has begun closing. Socket interruption precedes joining blocked network work.
func (t *executionTransport) addBackground(done <-chan struct{}) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finishing || t.ctx.Err() != nil {
		return false
	}
	if t.background == nil {
		t.background = make(map[<-chan struct{}]struct{})
	}
	t.background[done] = struct{}{}
	return true
}

func (t *executionTransport) completeBackground(done chan struct{}) {
	t.mu.Lock()
	close(done)
	delete(t.background, done)
	t.mu.Unlock()
}

// Arm cancellation for exactly the period during which a command owns sockets.
// Joining an already-started callback avoids a callback surviving command return.
func (t *executionTransport) arm(ctx context.Context) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { t.interrupt(); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}
}

// A native error (including an optional handshake-command refusal) is a
// completely consumed reply, just like a missing value. Transport/context or
// wrapped errors are conservatively uncertain and discard the pool.
func executionPoolUncertain(err error) bool {
	if err == nil || err == goredis.Nil {
		return false
	}
	_, native := err.(interface {
		error
		RedisError()
	})
	return !native
}

// A complete native refusal can still leave connection-local transaction
// state armed. Cleanup must positively report its expected status, never QUEUED
// (which means it was appended to a still-open MULTI). Unknown status shapes
// are conservatively discarded as well.
func executionPoolCleanupFailed(cmd goredis.Cmder, resultErr error) bool {
	expected := "OK"
	switch strings.ToLower(cmd.Name()) {
	case "unwatch", "discard":
	case "reset":
		expected = "RESET"
	default:
		return false
	}
	if resultErr != nil || cmd.Err() != nil {
		return true
	}
	var value string
	switch c := cmd.(type) {
	case interface{ Val() string }:
		value = c.Val()
	case interface{ Val() any }:
		value, _ = c.Val().(string)
	}
	return value != expected
}

// A stale execution context must not submit work to a pool that now belongs
// to another execution. Count admitted commands under the completion mutex so
// finish cannot lend an execution pool while its machinery is still in flight.
func (p *executionPool) beginCommand(ctx context.Context) (*executionTransport, error) {
	t, _ := ctx.Value(transportContextKey{}).(*executionTransport)
	if t == nil || t.pool != p {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	if p.active != t || t.finishing {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	t.commands++
	return t, nil
}
func (t *executionTransport) endCommand() {
	t.mu.Lock()
	t.commands--
	t.mu.Unlock()
}

func (p *executionPool) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (p *executionPool) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		t, err := p.beginCommand(ctx)
		if err != nil {
			cmd.SetErr(err)
			return err
		}
		if t == nil {
			return next(ctx, cmd)
		}
		defer t.endCommand()
		stop := t.arm(ctx)
		defer stop()
		err = next(ctx, cmd)
		if executionPoolUncertain(err) || executionPoolUncertain(cmd.Err()) || executionPoolCleanupFailed(cmd, err) {
			t.mu.Lock()
			t.failed = true
			t.mu.Unlock()
		}
		return err
	}
}
func (p *executionPool) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		t, err := p.beginCommand(ctx)
		if err != nil {
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
			return err
		}
		if t == nil {
			return next(ctx, cmds)
		}
		defer t.endCommand()
		stop := t.arm(ctx)
		defer stop()
		err = next(ctx, cmds)
		uncertain := executionPoolUncertain(err)
		transaction := false
		failed := err != nil
		// A pipeline can report its first native error while a later reply is
		// lost. Inspect every reply, and never lend a failed transaction even
		// when all errors are native: EXEC refusal can leave MULTI armed.
		for _, cmd := range cmds {
			name := strings.ToLower(cmd.Name())
			transaction = transaction || name == "multi" || name == "exec"
			failed = failed || cmd.Err() != nil
			uncertain = uncertain || executionPoolUncertain(cmd.Err()) || executionPoolCleanupFailed(cmd, cmd.Err())
		}
		uncertain = uncertain || transaction && failed
		if uncertain {
			t.mu.Lock()
			t.failed = true
			t.mu.Unlock()
		}
		return err
	}
}

func (c *connectionImpl) newExecutionPool(timeouts timeouts) *executionPool {
	p := &executionPool{cfg: c.clientCfg, timeouts: timeouts, sockets: map[*executionSocket]bool{}}
	single, cluster := *c.clientCfg.single, *c.clientCfg.cluster
	dial := single.Dialer
	if dial == nil {
		dial = goredis.NewDialer(&single)
	}
	wrapped := func(ctx context.Context, network, address string) (net.Conn, error) {
		p.mu.Lock()
		t := p.active
		p.mu.Unlock()
		if t == nil {
			return nil, net.ErrClosed
		}
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		socket := &executionSocket{Conn: &diagnosticConn{Conn: conn, active: &c.clientCfg.diagnostics}, pool: p}
		p.mu.Lock()
		if p.active != t || t.ctx.Err() != nil || ctx.Err() != nil {
			p.mu.Unlock()
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		p.sockets[socket] = true
		p.mu.Unlock()
		return socket, nil
	}
	single.Dialer, cluster.Dialer = wrapped, wrapped
	// Never replay a consumed cursor page or an uncertain mutation.
	single.MaxRetries, cluster.MaxRetries = -1, -1
	single.MinIdleConns, cluster.MinIdleConns = 0, 0
	cfg := &clientConfig{single: &single, cluster: &cluster}
	_, isCluster := c.store.client.(*goredis.ClusterClient)
	p.client = newClient(cfg, isCluster, timeouts)
	if c.store.snapshots {
		addSnapshotHooks(p.client)
	}
	p.client.AddHook(p)
	if cc, ok := p.client.(*goredis.ClusterClient); ok {
		cc.OnNewNode(func(node *goredis.Client) { node.AddHook(p) })
	}
	return p
}

func (c *connectionImpl) executionContext(ctx context.Context, owners ...*streams) (context.Context, *executionTransport) {
	return c.executionContextWithTimeouts(ctx, c.timeouts, owners...)
}
func (c *connectionImpl) executionContextWithTimeouts(ctx context.Context, timeouts timeouts, owners ...*streams) (context.Context, *executionTransport) {
	if existing, _ := ctx.Value(transportContextKey{}).(*executionTransport); existing != nil && existing.owner == c.store {
		return ctx, nil
	}
	parent, cancel := context.WithCancelCause(ctx)
	t := &executionTransport{owner: c.store, ctx: parent, cancel: cancel, owners: owners, done: make(chan struct{})}
	t.matviews = &matviewReads{store: c.store}
	c.store.mu.Lock()
	pool := c.store.idleExecution
	c.store.idleExecution = nil
	c.store.mu.Unlock()
	if pool != nil && (pool.timeouts != timeouts || pool.cfg != c.clientCfg) {
		_ = pool.client.Close()
		pool = nil
	}
	if pool == nil {
		pool = c.newExecutionPool(timeouts)
	}
	t.pool, t.client = pool, pool.client
	pool.mu.Lock()
	pool.active = t
	pool.mu.Unlock()
	for _, owner := range owners {
		owner.add(t)
	}
	c.store.mu.Lock()
	closed := c.store.closed
	if !closed {
		if c.store.executions == nil {
			c.store.executions = map[*executionTransport]struct{}{}
		}
		c.store.executions[t] = struct{}{}
	}
	c.store.mu.Unlock()
	if closed || c.store.isClosed() {
		t.closeWith(errConnClosed)
	}
	return context.WithValue(parent, transportContextKey{}, t), t
}

func (s *store) redis(ctx context.Context) goredis.UniversalClient {
	if t, _ := ctx.Value(transportContextKey{}).(*executionTransport); t != nil && t.owner == s {
		// Cleanup with WithoutCancel uses the connection pool after interruption.
		t.mu.Lock()
		interrupted := t.interrupted
		t.mu.Unlock()
		if interrupted && ctx.Done() == nil {
			return s.client
		}
		if t.ctx.Err() == nil || ctx.Err() != nil {
			return t.client
		}
	}
	return s.client
}

// Only the public result reader owns the execution pool. Child/union readers
// share it without closing it when one child ends. Keep the concrete reader so
// its existing Arrow, cancellation and cursor lifecycle remain authoritative.
func (t *executionTransport) reader(reader array.RecordReader) array.RecordReader {
	switch r := reader.(type) {
	case *layoutReader:
		r.source = t.reader(r.source)
	case *streamReader:
		r.mu.Lock()
		if r.closed {
			t.finish()
		} else {
			r.transport = t
		}
		r.mu.Unlock()
	case *subReader:
		r.mu.Lock()
		if r.closed {
			t.finish()
		} else {
			r.transport = t
		}
		r.mu.Unlock()
	case *retainedReader:
		r.mu.Lock()
		if r.closed {
			t.finish()
		} else {
			r.transport = t
		}
		r.mu.Unlock()
	default:
		t.finish()
	}
	return reader
}
