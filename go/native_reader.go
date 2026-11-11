// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// NativeReader reads managed HASH rows without SQL parsing, planning or
// execution. Calls are serialized. Close cancels an in-flight read before
// waiting for it, then releases the shared view and connection resources.
// Use a dedicated reader per independently retained snapshot.
type NativeReader struct {
	mu     sync.Mutex
	db     *databaseImpl
	conn   *connectionImpl
	life   context.Context
	cancel context.CancelFunc
	closed bool
}

const nativeMaxFields = 4096

// OpenNativeReader accepts the same database options as ADBC. It uses the same
// RESP3, TLS, storage-version and backend authorization checks. A read-only
// public API still needs backend reader-control permissions to maintain pins.
func OpenNativeReader(ctx context.Context, options map[string]string) (*NativeReader, error) {
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, options)
	if err != nil {
		return nil, err
	}
	if db.readerOnly || db.foreignIndexes {
		_ = db.Close(context.Background())
		return nil, errorf(adbc.StatusNotImplemented, "native reads do not support retained-reader mode or foreign-index discovery")
	}
	conn, err := db.openConnection(ctx)
	if err != nil {
		_ = db.Close(context.Background())
		return nil, err
	}
	if !conn.store.snapshots || conn.readerOnly || conn.store.foreignIndexes {
		_ = conn.Close(context.Background())
		_ = db.Close(context.Background())
		return nil, errorf(adbc.StatusNotImplemented, "native reads require managed tables without foreign-index discovery or retained-reader mode")
	}
	life, cancel := context.WithCancel(context.Background())
	return &NativeReader{db: db, conn: conn, life: life, cancel: cancel}, nil
}

func (r *NativeReader) Close() error {
	r.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(r.conn.Close(ctx), r.db.Close(ctx))
}

// callContext must be used under mu. No call may inherit a stale snapshot scope.
func (r *NativeReader) callContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if r.closed || r.life.Err() != nil {
		return nil, nil, errConnClosed
	}
	if snapshotOf(ctx) != nil {
		return nil, nil, errorf(adbc.StatusInvalidArgument, "native reads require an unscoped caller context")
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	stop := context.AfterFunc(r.life, cancel)
	return ctx, func() { stop(); cancel() }, nil
}

// AcquireSnapshot binds a fixed read view until ReleaseSnapshot or Close.
// retention must be 120000..3600000 milliseconds; zero selects the default.
func (r *NativeReader) AcquireSnapshot(ctx context.Context, retention time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel, err := r.callContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if retention != 0 {
		if retention%time.Millisecond != 0 {
			return errorf(adbc.StatusInvalidArgument, "retention must be whole milliseconds")
		}
		if err := r.conn.SetOptionInt(ctx, OptionIntReadSnapshotRetention, retention.Milliseconds()); err != nil {
			return err
		}
	}
	return r.conn.setReadSnapshotOption(ctx, OptionStringReadSnapshotAction, "acquire")
}

func (r *NativeReader) ReleaseSnapshot(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel, err := r.callContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return r.conn.setReadSnapshotOption(ctx, OptionStringReadSnapshotAction, "release")
}

// ReadHash is the shared native point-read primitive. rowID is the internal
// positive int64 __rowid, NOT an application primary-key value. Schema/table
// names are exact identifiers. nil fields means all public columns; an empty
// list reads existence only. Unknown fields and SQL NULLs are absent. Values
// are the stored Redis bytes, without SQL/Arrow type conversion.
// The returned boolean distinguishes a missing row from an all-NULL row.
func (r *NativeReader) ReadHash(ctx context.Context, schema, table string, rowID int64, fields []string) (map[string][]byte, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel, err := r.callContext(ctx)
	if err != nil {
		return nil, false, err
	}
	defer cancel()
	if schema == "" || table == "" || rowID <= 0 || isTempSchema(schema) || isTempAlias(schema) {
		return nil, false, errorf(adbc.StatusInvalidArgument, "native reads require an explicit permanent schema/table and a positive internal row ID")
	}
	if len(fields) > nativeMaxFields {
		return nil, false, errorf(adbc.StatusInvalidArgument, "native reads support at most 4096 projected fields")
	}
	ctx, transport := r.conn.executionContext(ctx)
	defer transport.finish()
	ctx, pin, err := r.conn.store.acquireSnapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	defer pin.release()
	return r.conn.store.readSnapshotHash(ctx, schema, table, rowID, fields)
}

// Uses the very same descriptor proofs, server fences, row-directory checks,
// version resolution and lease validation as managed SQL/partition readers.
func (s *store) readSnapshotHash(ctx context.Context, schema, table string, rowID int64, fields []string) (map[string][]byte, bool, error) {
	meta, err := s.getTable(ctx, schema, table)
	if err != nil {
		return nil, false, err
	}
	wanted := make(map[string]bool)
	if fields != nil {
		for _, field := range fields {
			wanted[field] = true
		}
	}
	selected := make([]columnMeta, 0, len(meta.Columns))
	physical := make([]string, 0, len(meta.Columns))
	for _, col := range meta.Columns {
		if fields == nil || wanted[col.Name] {
			selected = append(selected, col)
			physical = append(physical, col.field())
			if col.MissingThrough > 0 {
				physical = append(physical, nullMarker(col.field()))
			}
		}
	}
	if len(selected) > nativeMaxFields {
		return nil, false, errorf(adbc.StatusInvalidArgument, "native reads support at most 4096 projected fields")
	}
	// A non-nil projection avoids HGETALL of arbitrary physical/internal fields.
	ctx = context.WithValue(ctx, snapshotDirectReadKey{}, true)
	rows, err := s.fetchSnapshotVisible(ctx, meta, []string{meta.prefix() + strconv.FormatInt(rowID, 10)}, physical)
	if err != nil {
		return nil, false, err
	}
	if err := snapshotOf(ctx).pin.valid(ctx); err != nil {
		return nil, false, err
	}
	out := make(map[string][]byte)
	if rows[0] == nil {
		return out, false, nil
	}
	bytes := 0
	for _, col := range selected {
		if value, ok := rows[0][col.field()]; ok {
			bytes += len(col.Name) + len(value)
			if bytes > snapshotMaxBytes {
				return nil, false, errorf(adbc.StatusIO, "native HASH result exceeds 64 MiB")
			}
			out[col.Name] = []byte(value)
		} else {
			value, err := col.missingValue(rows[0])
			if err != nil {
				return nil, false, err
			}
			if !value.Null {
				bytes += len(col.Name) + len(col.Missing)
				if bytes > snapshotMaxBytes {
					return nil, false, errorf(adbc.StatusIO, "native HASH result exceeds 64 MiB")
				}
				out[col.Name] = []byte(col.Missing)
			}
		}
	}
	if err := snapshotOf(ctx).pin.valid(ctx); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (r *NativeReader) HGetAll(ctx context.Context, schema, table string, rowID int64) (map[string][]byte, error) {
	row, _, err := r.ReadHash(ctx, schema, table, rowID, nil)
	return row, err
}

func (r *NativeReader) HMGet(ctx context.Context, schema, table string, rowID int64, fields ...string) ([][]byte, error) {
	if len(fields) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "HMGet requires at least one field")
	}
	row, _, err := r.ReadHash(ctx, schema, table, rowID, fields)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(fields))
	for i, field := range fields {
		out[i] = row[field]
	}
	return out, nil
}

func (r *NativeReader) HGet(ctx context.Context, schema, table string, rowID int64, field string) ([]byte, error) {
	values, err := r.HMGet(ctx, schema, table, rowID, field)
	if err != nil {
		return nil, err
	}
	return values[0], nil
}

func (r *NativeReader) HExists(ctx context.Context, schema, table string, rowID int64, field string) (bool, error) {
	v, err := r.HGet(ctx, schema, table, rowID, field)
	return v != nil, err
}

func (r *NativeReader) HLen(ctx context.Context, schema, table string, rowID int64) (int, error) {
	row, err := r.HGetAll(ctx, schema, table, rowID)
	return len(row), err
}

func (r *NativeReader) Exists(ctx context.Context, schema, table string, rowID int64) (bool, error) {
	_, exists, err := r.ReadHash(ctx, schema, table, rowID, []string{})
	return exists, err
}
