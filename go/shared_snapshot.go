// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	OptionStringReadSnapshotAction = "adbc.redis.read_snapshot_action"
	OptionStringReadSnapshotStatus = "adbc.redis.read_snapshot_status"
	OptionIntReadSnapshotRetention = "adbc.redis.read_snapshot_retention_ms"
)

// One ordinary, authenticated server pin owns the generation. The connection
// and each executing statement/live reader hold separate local references. No
// token is exported and no attachment authority is created.
type sharedReadView struct {
	mu       sync.Mutex
	pin      *snapshotPin
	cancel   context.CancelFunc
	deadline time.Time
	refs     int
}

func sharedSnapshotUnsupported() error {
	return errorf(adbc.StatusNotImplemented, "shared read snapshots support connection-local metadata and read-only SELECTs only; sharing, partitions, writes and temporary objects are unsupported")
}

func (v *sharedReadView) release() {
	v.mu.Lock()
	v.refs--
	last := v.refs == 0
	v.mu.Unlock()
	if last {
		v.cancel()
		v.pin.release()
	}
}

func (v *sharedReadView) borrow(ctx context.Context) (context.Context, *snapshotPin, error) {
	if err := v.valid(ctx); err != nil {
		return ctx, nil, err
	}
	v.mu.Lock()
	v.refs++
	v.mu.Unlock()
	child := &snapshotPin{ctx: ctx, store: v.pin.store, root: v.pin.root, token: v.pin.token, shared: v}
	scope := &snapshotScope{id: child.root, pin: child, store: child.store}
	return context.WithValue(ctx, snapshotContextKey{}, scope), child, nil
}

func (s *store) hasSharedReadView() bool {
	if s == nil {
		return false
	}
	s.sharedMu.Lock()
	defer s.sharedMu.Unlock()
	return s.sharedView != nil
}

func (s *store) releaseSharedReadView() {
	s.sharedMu.Lock()
	v := s.sharedView
	s.sharedView = nil
	s.sharedMu.Unlock()
	if v != nil {
		v.release()
	}
}

func (c *connectionImpl) setReadSnapshotOption(ctx context.Context, key, value string) error {
	if c.store == nil || c.store.isClosed() {
		return errConnClosed
	}
	s := c.store
	if key == OptionStringReadSnapshotStatus {
		return errorf(adbc.StatusInvalidArgument, "%s is read-only", key)
	}
	if key == OptionStringReadSnapshotAction && value == "release" {
		s.releaseSharedReadView()
		return nil
	}
	retention, err := c.prepareReadSnapshotBinding(key, value)
	if err != nil || key == OptionIntReadSnapshotRetention {
		return err
	}
	defer func() { s.sharedMu.Lock(); s.sharedAcquiring = false; s.sharedMu.Unlock() }()
	deadline := time.Now().Add(retention)
	// Acquisition obeys caller cancellation, but a successful binding belongs to
	// the connection rather than the short-lived SetOption call context.
	life, cancel := context.WithDeadline(context.Background(), deadline)
	stop := context.AfterFunc(ctx, cancel)
	_, pin, err := s.acquireSnapshotMode(life, true)
	stopped := stop()
	if err != nil || !stopped || ctx.Err() != nil {
		cancel()
		pin.release()
		if err != nil {
			return err
		}
		return cancelled(ctx)
	}
	s.sharedMu.Lock()
	defer s.sharedMu.Unlock()
	if s.isClosed() {
		cancel()
		pin.release()
		return errConnClosed
	}
	s.sharedView = &sharedReadView{pin: pin, cancel: cancel, deadline: deadline, refs: 1}
	return nil
}

// Reserve local acquisition without keeping the mutex through server admission.
// Background compaction can need a fresh pin while foreground acquisition waits.
func (c *connectionImpl) prepareReadSnapshotBinding(key, value string) (time.Duration, error) {
	s := c.store
	s.sharedMu.Lock()
	defer s.sharedMu.Unlock()
	if s.sharedView != nil || s.sharedAcquiring {
		return 0, errorf(adbc.StatusInvalidState, "release the bound read snapshot before acquiring or changing retention")
	}
	if key == OptionIntReadSnapshotRetention {
		ms, err := strconv.ParseInt(value, 10, 64)
		if err != nil || ms < 120000 || ms > 3600000 {
			return 0, errorf(adbc.StatusInvalidArgument, "read snapshot retention must be 120000..3600000 milliseconds")
		}
		c.readSnapshotRetention = time.Duration(ms) * time.Millisecond
		return 0, nil
	}
	if value != "acquire" || !s.snapshots || c.readerOnly || s.hasTempObjects() {
		return 0, sharedSnapshotUnsupported()
	}
	c.streams.mu.Lock()
	busy := len(c.streams.open) != 0
	c.streams.mu.Unlock()
	if busy {
		return 0, errorf(adbc.StatusInvalidState, "close active readers before acquiring a shared read snapshot")
	}
	retention := c.readSnapshotRetention
	if retention == 0 {
		retention = snapshotTimeout
	}
	s.sharedAcquiring = true
	return retention, nil
}

func (c *connectionImpl) readSnapshotOption(ctx context.Context, key string) (string, error) {
	if c.store == nil || c.store.isClosed() {
		return "", errConnClosed
	}
	s := c.store
	s.sharedMu.Lock()
	defer s.sharedMu.Unlock()
	if key == OptionIntReadSnapshotRetention {
		retention := c.readSnapshotRetention
		if retention == 0 {
			retention = snapshotTimeout
		}
		return strconv.FormatInt(retention.Milliseconds(), 10), nil
	}
	if key == OptionStringReadSnapshotAction {
		return "", errorf(adbc.StatusInvalidArgument, "%s is write-only", key)
	}
	if s.sharedView == nil {
		return `{"state":"unbound"}`, nil
	}
	v := s.sharedView
	if err := v.valid(ctx); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		State      string    `json:"state"`
		Generation string    `json:"generation"`
		Deadline   time.Time `json:"deadline"`
	}{"active", v.pin.root, v.deadline})
	return string(raw), err
}

func sharedSnapshotScope(ctx context.Context) bool {
	scope := snapshotOf(ctx)
	return scope != nil && scope.pin != nil && scope.pin.shared != nil
}

// Loss is terminal for the binding, including a transient failure to establish
// whether its authority is still valid. Only an explicit new attempt can bind
// another generation; a successful later network request cannot revive it.
func (v *sharedReadView) valid(ctx context.Context) error {
	err := cancelled(ctx)
	if err == nil {
		if v.pin.ctx.Err() != nil {
			err = errorf(adbc.StatusIO, "shared read snapshot lifetime ended; start a new full read attempt")
		} else {
			err = v.pin.valid(ctx)
		}
	}
	if err != nil && ctx.Err() == nil {
		v.pin.mu.Lock()
		if v.pin.err == nil {
			v.pin.err = err
		}
		v.pin.mu.Unlock()
	}
	return err
}

func (c *connectionImpl) SetOptionInt(ctx context.Context, key string, value int64) error {
	if key == OptionIntReadSnapshotRetention {
		return c.setReadSnapshotOption(ctx, key, strconv.FormatInt(value, 10))
	}
	return c.ConnectionImplBase.SetOptionInt(ctx, key, value)
}
func (c *connectionImpl) GetOptionInt(ctx context.Context, key string) (int64, error) {
	if key == OptionIntReadSnapshotRetention {
		value, err := c.readSnapshotOption(ctx, key)
		if err != nil {
			return 0, err
		}
		return strconv.ParseInt(value, 10, 64)
	}
	return c.ConnectionImplBase.GetOptionInt(ctx, key)
}
