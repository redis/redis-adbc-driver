// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// GetObjects binds before discovery/filtering, including catalog-only requests
// that never call a schema/table enumerator. Driverbase finishes materializing
// metadata before returning, so the returned reader needs no server pin.
func (c *connectionImpl) GetObjects(ctx context.Context, depth adbc.ObjectDepth, catalog, schema, table, column *string, types []string) (array.RecordReader, error) {
	if c.store == nil || c.store.isClosed() {
		return nil, errConnClosed
	}
	ctx, pin, err := c.store.acquireSnapshot(ctx)
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
