// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const snapshotTempSchemasKey = metaPrefix + "snapshot_temp_schemas"

// Session liveness is global; table/view metadata and row references are part
// of the versioned manifest. Heartbeats must not revive a retired workspace.
func snapshotTempControlKey(key string) bool {
	return key == tempNextKey || key == tempOwnersKey || strings.HasPrefix(key, metaPrefix+"temp:alive:")
}

func (s *store) registerSnapshotTemp(ctx context.Context, schema string) error {
	if !s.snapshots {
		return nil
	}
	return s.redis(ctx).SAdd(ctx, snapshotTempSchemasKey, schema).Err()
}

// Internal manifest traversal includes every session's temporary schema. Public
// schema discovery continues to expose only the caller's temporary objects.
func (s *store) snapshotSchemas(ctx context.Context) ([]string, error) {
	schemas, err := s.listSchemas(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(schemas, foreignSchema) {
		schemas = append(schemas, foreignSchema)
	}
	temps, err := s.redis(ctx).SMembers(ctx, snapshotTempSchemasKey).Result()
	if err != nil {
		return nil, err
	}
	for _, schema := range temps {
		if !isTempSchema(schema) {
			return nil, errorf(adbc.StatusIO, "invalid temporary snapshot schema")
		}
		if !slices.Contains(schemas, schema) {
			schemas = append(schemas, schema)
		}
	}
	return schemas, nil
}

func (s *store) refreshSnapshotTemp(ctx context.Context) error {
	schema := s.tempSchema()
	if schema == tempAlias {
		return nil
	}
	tables, err := s.listTables(ctx, schema)
	if err != nil {
		return err
	}
	views, err := s.listViews(ctx, schema)
	if err != nil {
		return err
	}
	s.temp.mu.Lock()
	defer s.temp.mu.Unlock()
	s.temp.objects = map[string]bool{}
	for _, name := range append(tables, views...) {
		s.temp.objects[name] = true
	}
	return nil
}

func (s *store) dropTempOwned(ctx context.Context, id string, stillDead func() bool) error {
	if !s.snapshots {
		return s.dropTempObjects(ctx, tempSchemaBase+id, stillDead)
	}
	// A conflict must not remove the liveness/owner records before the descriptor
	// deletion is published. A subsequent Open retries abandoned cleanup.
	for attempt := 0; attempt < 32; attempt++ {
		err := s.dropSnapshotTempAttempt(ctx, id, stillDead)
		var ae adbc.Error
		if err == nil || !asAdbc(err, &ae) || string(ae.SqlState[:]) != "40001" {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return stateErrorf("40001", "temporary schema cleanup conflicted; a later connection will retry")
}

func (s *store) dropSnapshotTempAttempt(ctx context.Context, id string, stillDead func() bool) error {
	sctx, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		return err
	}
	defer pin.release()
	wctx, work, err := s.beginSnapshotWrite(sctx, pin)
	if err != nil {
		return err
	}
	defer s.abortSnapshot(pin, work)
	schema := tempSchemaBase + id
	if err := s.dropTempObjects(wctx, schema, stillDead); err != nil {
		return err
	}
	if stillDead != nil {
		if !stillDead() {
			return errOwnerAlive
		}
		snapshotOf(wctx).tempDead = id
	}
	if err := s.redis(wctx).SRem(wctx, snapshotTempSchemasKey, schema).Err(); err != nil {
		return err
	}
	return s.publishSnapshot(wctx, pin, work)
}
