// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"net"
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
	sockets     map[*executionSocket]bool
	interrupted bool
	once        sync.Once
	owners      []*streams
}

type executionSocket struct {
	net.Conn
	transport *executionTransport
}

func (c *executionSocket) Close() error {
	c.transport.mu.Lock()
	delete(c.transport.sockets, c)
	c.transport.mu.Unlock()
	return c.Conn.Close()
}

func (t *executionTransport) interrupt() {
	t.mu.Lock()
	t.interrupted = true
	sockets := make([]*executionSocket, 0, len(t.sockets))
	for socket := range t.sockets {
		sockets = append(sockets, socket)
	}
	t.mu.Unlock()
	for _, socket := range sockets {
		_ = socket.Close()
	}
}
func (t *executionTransport) closeWith(err error) {
	t.cancel(err)
	t.interrupt()
	// Close waits for command machinery; never call it inside a process hook.
	go t.finish()
}
func (t *executionTransport) finish() {
	t.once.Do(func() {
		t.cancel(nil)
		t.interrupt()
		_ = t.client.Close()
		for _, owner := range t.owners {
			owner.remove(t)
		}
	})
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
func (t *executionTransport) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (t *executionTransport) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		stop := t.arm(ctx)
		defer stop()
		return next(ctx, cmd)
	}
}
func (t *executionTransport) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		stop := t.arm(ctx)
		defer stop()
		return next(ctx, cmds)
	}
}

func (c *connectionImpl) executionContext(ctx context.Context, owners ...*streams) (context.Context, *executionTransport) {
	return c.executionContextWithTimeouts(ctx, c.timeouts, owners...)
}
func (c *connectionImpl) executionContextWithTimeouts(ctx context.Context, timeouts timeouts, owners ...*streams) (context.Context, *executionTransport) {
	if existing, _ := ctx.Value(transportContextKey{}).(*executionTransport); existing != nil && existing.owner == c.store {
		return ctx, nil
	}
	parent, cancel := context.WithCancelCause(ctx)
	t := &executionTransport{owner: c.store, ctx: parent, cancel: cancel, sockets: map[*executionSocket]bool{}, owners: owners}
	single, cluster := *c.clientCfg.single, *c.clientCfg.cluster
	dial := single.Dialer
	if dial == nil {
		dial = goredis.NewDialer(&single)
	}
	wrapped := func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		socket := &executionSocket{Conn: &diagnosticConn{Conn: conn, active: &c.clientCfg.diagnostics}, transport: t}
		t.mu.Lock()
		if t.ctx.Err() != nil || ctx.Err() != nil {
			t.mu.Unlock()
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		t.sockets[socket] = true
		t.mu.Unlock()
		return socket, nil
	}
	single.Dialer, cluster.Dialer = wrapped, wrapped
	// An unknown reply can be a committed write or consumed cursor page.
	single.MaxRetries, cluster.MaxRetries = -1, -1
	single.MinIdleConns, cluster.MinIdleConns = 0, 0
	cfg := &clientConfig{single: &single, cluster: &cluster}
	_, isCluster := c.store.client.(*goredis.ClusterClient)
	t.client = newClient(cfg, isCluster, timeouts)
	t.client.AddHook(t)
	if cc, ok := t.client.(*goredis.ClusterClient); ok {
		cc.OnNewNode(func(node *goredis.Client) { node.AddHook(t) })
	}
	for _, owner := range owners {
		owner.add(t)
	}
	if c.store.isClosed() {
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
	case *streamReader:
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
