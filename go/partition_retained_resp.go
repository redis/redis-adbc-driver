// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const (
	retainedRESPHeader     = 4096
	retainedRESPBulk       = 1 << 20
	retainedRESPCollection = 4096
	retainedRESPDepth      = 32
)

// The RESP guard withholds each length header until its logical allocation has
// been admitted. A raw byte counter after go-redis starts reading is too late:
// its decoder allocates bulk/collection storage from those headers first.
// The wrapper sits outside the dialer's TLS connection, so it sees plaintext
// after normal authentication/TLS setup; it never inspects outgoing credentials.
type retainedRESPConn struct {
	net.Conn
	reader *bufio.Reader
	budget *retainedBudget
	header []byte
	body   int64
	stack  []int64
	err    error
	stop   func() bool
}

func (c *retainedRESPConn) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.Conn.Close()
}
func guardRetainedRESP(conn net.Conn, budget *retainedBudget) net.Conn {
	return &retainedRESPConn{Conn: conn, reader: bufio.NewReaderSize(conn, retainedRESPHeader), budget: budget}
}
func retainedRESPInvalid() error {
	return errorf(adbc.StatusInvalidData, "invalid or excessive retained source RESP reply")
}
func (c *retainedRESPConn) complete() {
	for len(c.stack) > 0 {
		index := len(c.stack) - 1
		c.stack[index]--
		if c.stack[index] > 0 {
			return
		}
		c.stack = c.stack[:index]
	}
}
func (c *retainedRESPConn) prepare() error {
	header, err := c.reader.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return retainedRESPInvalid()
		}
		if errors.Is(err, io.EOF) && (len(header) != 0 || len(c.stack) != 0) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	if len(header) < 3 || header[len(header)-2] != '\r' {
		return retainedRESPInvalid()
	}
	// This covers bounded header/string/error objects even for non-bulk replies.
	if err := c.budget.reserve(int64(len(header)) + 128); err != nil {
		return err
	}
	kind := header[0]
	value := header[1 : len(header)-2]
	switch kind {
	case '+', '-', ':', ',', '#', '_', '(':
		c.complete()
	case '$', '!', '=':
		size, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || size < -1 || size > retainedRESPBulk || size == -1 && kind != '$' {
			return retainedRESPInvalid()
		}
		if size == -1 {
			c.complete()
			break
		}
		// go-redis can copy decoded bytes into strings: charge both plus object
		// overhead before allowing either allocation. Source capture has its own
		// independent budget for the values it retains after this decoder.
		if err := c.budget.reserve(size*2 + 64); err != nil {
			return err
		}
		c.body = size + 2
	case '*', '%', '~', '>', '|':
		count, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || count < -1 || count > retainedRESPCollection || count == -1 && kind != '*' {
			return retainedRESPInvalid()
		}
		if count == -1 {
			c.complete()
			break
		}
		if kind == '%' || kind == '|' {
			count *= 2
		}
		if kind == '|' {
			count++
		} // attributes wrap the following value
		if len(c.stack) >= retainedRESPDepth {
			return retainedRESPInvalid()
		}
		// Charge interface/collection element capacity before the parent decoder
		// can allocate it, including nested arrays whose wire headers are tiny.
		if err := c.budget.reserve(count*64 + 64); err != nil {
			return err
		}
		if count == 0 {
			c.complete()
		} else {
			c.stack = append(c.stack, count)
		}
	default:
		return retainedRESPInvalid()
	}
	c.header = header
	return nil
}
func (c *retainedRESPConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if c.err != nil {
		return 0, c.err
	}
	if len(c.header) > 0 {
		n := copy(dst, c.header)
		c.header = c.header[n:]
		return n, nil
	}
	if c.body > 0 {
		n, err := c.reader.Read(dst[:min(int64(len(dst)), c.body)])
		c.body -= int64(n)
		if c.body == 0 {
			c.complete()
		}
		if errors.Is(err, io.EOF) && c.body > 0 {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			c.err = err
		}
		return n, err
	}
	if err := c.prepare(); err != nil {
		c.err = err
		return 0, err
	}
	n := copy(dst, c.header)
	c.header = c.header[n:]
	return n, nil
}

// Only retained execution will call this factory. It creates fresh pools and
// budgets across all Cluster primaries; replacing ordinary connection timeouts
// cannot silently reset a running coordinator's accounting.
func newRetainedSourceClient(cfg *clientConfig, cluster bool, t timeouts, budget *retainedBudget, lifetime ...context.Context) goredis.UniversalClient {
	wrap := func(options *goredis.Options) *goredis.Client {
		opts := *options
		opts.PushNotificationProcessor = nil
		opts.MaxRetries = -1
		opts.ContextTimeoutEnabled = true
		dial := opts.Dialer
		if dial == nil {
			dial = goredis.NewDialer(&opts)
		}
		opts.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			guarded := guardRetainedRESP(conn, budget).(*retainedRESPConn)
			if len(lifetime) > 0 {
				guarded.stop = context.AfterFunc(lifetime[0], func() { _ = conn.Close() })
			}
			return guarded, nil
		}
		return goredis.NewClient(&opts)
	}
	copyConfig := &clientConfig{}
	if !cluster {
		opts := *cfg.single
		opts.ReadTimeout, opts.WriteTimeout = t.goRedis()
		client := wrap(&opts)
		client.AddHook(errorHook{t: t})
		return client
	}
	options := *cfg.cluster
	options.PushNotificationProcessor = nil
	options.NewClient = wrap
	options.MaxRetries = -1
	options.ContextTimeoutEnabled = true
	options.ReadTimeout, options.WriteTimeout = t.goRedis()
	copyConfig.single = cfg.single
	copyConfig.cluster = &options
	return newClient(copyConfig, true, t)
}

var _ io.Reader = (*retainedRESPConn)(nil)
