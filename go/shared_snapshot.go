// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

const (
	// OptionStringReadSnapshot acquires or releases a connection-local read view.
	OptionStringReadSnapshot = "adbc.redis.read_snapshot"
	// OptionStringReadSnapshotID reports the generation for diagnostics, not attachment.
	OptionStringReadSnapshotID = "adbc.redis.read_snapshot_id"
	// OptionStringReadSnapshotRetention sets the fixed lifetime in milliseconds.
	OptionStringReadSnapshotRetention = "adbc.redis.read_snapshot_retention_ms"
)

// sharedReadSnapshot owns one normal renewable pin. Children borrow it locally;
// they cannot export its token or extend its fixed deadline. The mutex serializes
// acquisition/release with binding, not SQL execution or Arrow consumption.
type sharedReadSnapshot struct {
	mu        sync.Mutex
	pin       *snapshotPin
	ctx       context.Context
	cancel    context.CancelFunc
	retention time.Duration
}

func (v *sharedReadSnapshot) active() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.pin != nil
}

func sharedSnapshotUnsupported() error {
	return errorf(adbc.StatusNotImplemented, "shared read snapshots support connection-local read-only SQL and metadata; writes, temporary objects, partitions and attachment are unsupported")
}

func (c *connectionImpl) sharedSnapshotOption(ctx context.Context, key, value string) error {
	v := &c.readSnapshot
	v.mu.Lock()
	defer v.mu.Unlock()
	if c.store == nil || c.store.isClosed() {
		return errConnClosed
	}
	switch key {
	case OptionStringReadSnapshotID:
		return errorf(adbc.StatusInvalidArgument, "%s is read-only; snapshot attachment is unsupported", key)
	case OptionStringReadSnapshotRetention:
		if v.pin != nil {
			return errorf(adbc.StatusInvalidState, "release the shared read snapshot before changing retention")
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1000 || n > 86400000 {
			return errorf(adbc.StatusInvalidArgument, "read_snapshot_retention_ms must be 1000–86400000")
		}
		v.retention = time.Duration(n) * time.Millisecond
		return nil
	}
	switch value {
	case "acquire":
		if v.pin != nil {
			return errorf(adbc.StatusInvalidState, "shared read snapshot already acquired; release it before starting a new full refresh")
		}
		if !c.store.snapshots || c.readerOnly || c.store.hasTempObjects() {
			return sharedSnapshotUnsupported()
		}
		ttl := v.retention
		if ttl == 0 {
			ttl = time.Hour
		}
		// Acquisition observes caller cancellation; after success the parent survives
		// the SetOption call and is owned by release, Close or its fixed deadline.
		parent, cancel := context.WithTimeout(context.WithoutCancel(ctx), ttl)
		stop := context.AfterFunc(ctx, cancel)
		_, pin, err := c.store.acquireSnapshot(parent)
		stopped := stop()
		if err != nil || !stopped || ctx.Err() != nil {
			cancel()
			pin.release()
			if err != nil {
				return err
			}
			return cancelled(ctx)
		}
		v.ctx, v.cancel, v.pin = parent, cancel, pin
		return nil
	case "release":
		if v.pin != nil {
			v.cancel()
			v.pin.release()
			v.pin, v.ctx, v.cancel = nil, nil, nil
		}
		return nil
	default:
		return errorf(adbc.StatusInvalidArgument, "read_snapshot must be acquire or release; cross-connection attachment is unsupported")
	}
}

func (c *connectionImpl) acquireReadSnapshot(ctx context.Context) (context.Context, *snapshotPin, error) {
	if c.store == nil || c.store.isClosed() {
		return ctx, nil, errConnClosed
	}
	return c.readSnapshot.acquire(ctx, c.store)
}

func (v *sharedReadSnapshot) acquire(ctx context.Context, s *store) (context.Context, *snapshotPin, error) {
	if snapshotOf(ctx) != nil {
		return ctx, nil, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pin == nil {
		return s.acquireSnapshot(ctx)
	}
	if err := cancelled(v.ctx); err != nil {
		return ctx, nil, errorf(adbc.StatusInvalidState, "shared read snapshot expired or released; restart the full refresh")
	}
	if err := v.pin.valid(ctx); err != nil {
		return ctx, nil, err
	}
	deadline, _ := v.ctx.Deadline()
	child, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(v.ctx, cancel)
	p := &snapshotPin{parent: v.pin, root: v.pin.root, token: v.pin.token, store: s, reader: v.pin.reader, childRelease: func() { stop(); cancel() }}
	scope := &snapshotScope{id: p.root, pin: p, store: s, sharedRead: true}
	return context.WithValue(child, snapshotContextKey{}, scope), p, nil
}

func (v *sharedReadSnapshot) close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pin != nil {
		v.cancel()
		v.pin.release()
		v.pin, v.ctx, v.cancel = nil, nil, nil
	}
}

// GetObjects finishes construction under one binding, including discovery with
// only catalog depth. The base enumerator materializes its Arrow result before
// returning, so no Redis lease is needed by the returned reader.
func (c *connectionImpl) GetObjects(ctx context.Context, depth adbc.ObjectDepth, catalog, schema, table, column *string, types []string) (array.RecordReader, error) {
	ctx, pin, err := c.acquireReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer pin.release()
	helper := &snapshotObjects{connection: c, scope: snapshotOf(ctx)}
	reader, err := driverbase.NewConnectionBuilder(c).WithDbObjectsEnumerator(helper).Connection().GetObjects(ctx, depth, catalog, schema, table, column, types)
	if err == nil {
		err = pin.valid(ctx)
	}
	if err != nil && reader != nil {
		reader.Release()
		reader = nil
	}
	return reader, err
}

type snapshotObjects struct {
	connection *connectionImpl
	scope      *snapshotScope
}

func (o *snapshotObjects) bind(ctx context.Context) context.Context {
	return context.WithValue(ctx, snapshotContextKey{}, o.scope)
}
func (o *snapshotObjects) GetCatalogs(ctx context.Context, filter *string) ([]string, error) {
	return o.connection.GetCatalogs(o.bind(ctx), filter)
}
func (o *snapshotObjects) GetDBSchemasForCatalog(ctx context.Context, catalog string, filter *string) ([]string, error) {
	return o.connection.GetDBSchemasForCatalog(o.bind(ctx), catalog, filter)
}
func (o *snapshotObjects) GetTablesForDBSchema(ctx context.Context, catalog, schema string, table, column *string, columns bool) ([]driverbase.TableInfo, error) {
	return o.connection.GetTablesForDBSchema(o.bind(ctx), catalog, schema, table, column, columns)
}
