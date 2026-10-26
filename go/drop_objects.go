// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Validate the entire list before the first destructive operation. Individual
// drop implementations retain their existing ownership/recovery checks.
func (e *executor) runDropObjects(ctx context.Context, names []TableName, ifExists, view bool) error {
	var targets []TableName
	seen := map[TableName]bool{}
	for _, n := range names {
		schema, name, err := e.resolveTable(n)
		if err != nil {
			return err
		}
		resolved := TableName{Schema: schema, Name: name}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		if schema == foreignSchema || isPgCatalog(schema) || isInfoSchema(schema) {
			return virtualReadOnly(schema)
		}
		if view {
			exists, err := e.store.viewExists(ctx, schema, name)
			if err != nil {
				return err
			}
			if !exists {
				if ifExists {
					continue
				}
				return viewNotFound(schema, name)
			}
		} else {
			_, err = e.store.getTable(ctx, schema, name)
			if err != nil {
				var ae adbc.Error
				if ifExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
					continue
				}
				return err
			}
		}
		targets = append(targets, resolved)
	}
	for _, n := range targets {
		var err error
		if view {
			err = e.store.dropView(ctx, n.Schema, n.Name, ifExists)
		} else {
			err = e.store.dropTable(ctx, n.Schema, n.Name, ifExists)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
