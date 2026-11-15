// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"hash"
	"io"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
)

const snapshotWriterTargetLimit = 256
const snapshotWriterRequestLimit = 1024

// A turn is a bounded process-local scheduling hint, never an admission,
// catalog, permission or lease proof. Different processes and bounded overflow
// retain the original server-controlled admission path. There are no explicit
// client/catalog/credential fields. Active cancellation registration can retain
// the parent execution context indirectly; it is detached and cleared when the
// turn is released. Cardinality bounds do not bound caller-owned context memory.
var snapshotWriteTurns snapshotWriterRegistry

type snapshotWriterRegistry struct {
	mu                        sync.Mutex
	queues                    map[[64]byte]*snapshotWriterQueue
	requests                  int
	targetLimit, requestLimit int // zero uses the production constants
}
type snapshotWriterQueue struct {
	key        [64]byte
	head, tail *snapshotWriterTurn
}
type snapshotWriterTurn struct {
	registry       *snapshotWriterRegistry
	queue          *snapshotWriterQueue
	previous, next *snapshotWriterTurn
	ready          chan struct{}
	stop           func() bool
	released       bool // registry.mu; all fields except ready/registry use this lock
}

func (r *snapshotWriterRegistry) join(ctx context.Context, domain [32]byte, target string) (*snapshotWriterTurn, error) {
	if err := ctx.Err(); err != nil {
		return nil, cancelled(ctx)
	}
	if domain == ([32]byte{}) {
		return nil, nil
	}
	var key [64]byte
	copy(key[:32], domain[:])
	targetHash := sha256.Sum256([]byte(target))
	copy(key[32:], targetHash[:])
	r.mu.Lock()
	targetLimit, requestLimit := r.targetLimit, r.requestLimit
	if targetLimit == 0 {
		targetLimit = snapshotWriterTargetLimit
	}
	if requestLimit == 0 {
		requestLimit = snapshotWriterRequestLimit
	}
	q := r.queues[key]
	if r.requests >= requestLimit || q == nil && len(r.queues) >= targetLimit {
		r.mu.Unlock()
		return nil, nil // Bounded overflow weakens local FIFO, never server authority.
	}
	if q == nil {
		if r.queues == nil {
			r.queues = make(map[[64]byte]*snapshotWriterQueue)
		}
		q = &snapshotWriterQueue{key: key}
		r.queues[key] = q
	}
	turn := &snapshotWriterTurn{registry: r, queue: q, previous: q.tail, ready: make(chan struct{})}
	if q.tail == nil {
		q.head = turn
		close(turn.ready)
	} else {
		q.tail.next = turn
	}
	q.tail = turn
	r.requests++
	r.mu.Unlock()
	// Cancellation also hands off an already-admitted turn. The following writer
	// must still satisfy native server admission; a canceled input that ignores
	// Close cannot strand this local queue after its server pin has expired.
	stop := context.AfterFunc(ctx, turn.release)
	r.mu.Lock()
	released := turn.released
	if !released {
		turn.stop = stop
	}
	r.mu.Unlock()
	if released {
		stop()
	}
	return turn, nil
}
func (t *snapshotWriterTurn) wait(ctx context.Context) error {
	select {
	case <-t.ready:
	case <-ctx.Done():
	}
	if ctx.Err() != nil {
		t.release()
		return cancelled(ctx)
	}
	return nil
}
func (t *snapshotWriterTurn) release() {
	if t == nil {
		return
	}
	r := t.registry
	r.mu.Lock()
	if t.released {
		r.mu.Unlock()
		return
	}
	t.released = true
	q := t.queue
	wasHead := q.head == t
	if t.previous == nil {
		q.head = t.next
	} else {
		t.previous.next = t.next
	}
	if t.next == nil {
		q.tail = t.previous
	} else {
		t.next.previous = t.previous
	}
	if q.head == nil {
		delete(r.queues, q.key)
	} else if wasHead {
		close(q.head.ready)
	}
	r.requests--
	if len(r.queues) == 0 {
		r.queues = nil
	}
	stop := t.stop
	t.stop, t.queue, t.previous, t.next = nil, nil, nil, nil
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Fingerprint only representable static routing/principal/TLS identities. It
// partitions scheduling hints, not authorization. Custom dialers, credentials
// and TLS behavior conservatively retain the existing admission path.
func snapshotWriterDomain(cfg *clientConfig, caPath string) [32]byte {
	var zero [32]byte
	if cfg == nil || cfg.single == nil {
		return zero
	}
	o := cfg.single
	if o.Dialer != nil || o.CredentialsProvider != nil || o.CredentialsProviderContext != nil || o.StreamingCredentialsProvider != nil {
		return zero
	}
	h := sha256.New()
	write := func(s string) { snapshotWriterHashString(h, s) }
	write(o.Network)
	write(o.Addr)
	write(strconv.Itoa(o.DB))
	write(o.Username)
	write(o.Password)
	if cfg.cluster != nil {
		if cfg.cluster.ClusterSlots != nil || cfg.cluster.NewClient != nil {
			return zero
		}
		addresses := slices.Clone(cfg.cluster.Addrs)
		slices.Sort(addresses)
		for _, address := range addresses {
			write(address)
		}
	}
	if c := o.TLSConfig; c != nil {
		// Only the driver's ordinary static trust/server-name/min-version settings
		// are represented. Every other custom exported TLS setting bypasses this
		// optimization, including certificates and present/future callbacks.
		value := reflect.ValueOf(c).Elem()
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" || field.Name == "RootCAs" || field.Name == "ServerName" || field.Name == "MinVersion" {
				continue
			}
			if !value.Field(i).IsZero() {
				return zero
			}
		}
		write("tls")
		write(c.ServerName)
		write(strconv.Itoa(int(c.MinVersion)))
		if c.RootCAs != nil {
			if caPath == "" {
				return zero
			}
			pem, err := os.ReadFile(caPath)
			if err != nil {
				return zero
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) || !roots.Equal(c.RootCAs) {
				return zero
			}
			digest := sha256.Sum256(pem)
			_, _ = h.Write(digest[:])
		} else {
			write("system-roots")
		}
	} else {
		write("plain")
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
func snapshotWriterHashString(h hash.Hash, value string) {
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = io.WriteString(h, value)
}
