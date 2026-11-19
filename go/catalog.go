// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Catalogs share one immutable managed root, with independent schema identities.
// The default catalog keeps its existing layout. The NUL-prefixed internal form
// cannot be supplied as a public SQL/ADBC schema name and is never displayed.
const catalogsKey = metaPrefix + "catalogs"
const catalogSchemaPrefix = "\x00adbc:catalog:"
const maxCatalogs = 1024

func catalogSchema(catalog, schema string) string {
	if catalog == "" || catalog == catalogName {
		return schema
	}
	return catalogSchemaPrefix + hex.EncodeToString([]byte(catalog)) + ":" + schema
}

func splitCatalogSchema(schema string) (string, string) {
	if encoded, ok := strings.CutPrefix(schema, catalogSchemaPrefix); ok {
		name, logical, found := strings.Cut(encoded, ":")
		decoded, err := hex.DecodeString(name)
		if found && err == nil && len(decoded) > 0 && utf8.Valid(decoded) && !strings.ContainsRune(string(decoded), 0) {
			return string(decoded), logical
		}
	}
	return catalogName, schema
}
func schemaCatalog(schema string) string { name, _ := splitCatalogSchema(schema); return name }
func logicalSchema(schema string) string { _, name := splitCatalogSchema(schema); return name }

func validateCatalogName(name string) error {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return errorf(adbc.StatusInvalidArgument, "catalog name must be nonempty valid UTF-8 without NUL and at most 1024 bytes")
	}
	return nil
}
func validateLogicalSchema(schema string) error {
	if !utf8.ValidString(schema) || strings.ContainsRune(schema, 0) {
		return errorf(adbc.StatusInvalidArgument, "schema name must be valid UTF-8 without NUL")
	}
	return nil
}

func (s *session) currentCatalog() string {
	if s == nil || s.catalog == "" {
		return catalogName
	}
	return s.catalog
}
func (e *executor) currentCatalog() string {
	if e == nil {
		return catalogName
	}
	if e.sess != nil {
		return e.sess.currentCatalog()
	}
	return schemaCatalog(e.schema)
}

func (s *store) catalogExists(ctx context.Context, name string) (bool, error) {
	if name == catalogName {
		return true, nil
	}
	if err := validateCatalogName(name); err != nil {
		return false, err
	}
	ok, err := s.redis(ctx).SIsMember(ctx, catalogsKey, name).Result()
	if err != nil {
		return false, wrapRedis(err, "failed to check catalog")
	}
	return ok, nil
}
func (s *store) listCatalogs(ctx context.Context) ([]string, error) {
	names, err := s.redis(ctx).SMembers(ctx, catalogsKey).Result()
	if err != nil {
		return nil, wrapRedis(err, "failed to list catalogs")
	}
	if !slices.Contains(names, catalogName) {
		names = append(names, catalogName)
	}
	slices.Sort(names)
	return names, nil
}

// CREATE/DROP DATABASE operate on the existing guarded immutable metadata path.
// Each nondefault catalog starts with its own public schema. No existing default
// schema or row prefix is renamed, copied or reused.
func (s *store) createCatalog(ctx context.Context, name string, ifNotExists bool) error {
	if err := validateCatalogName(name); err != nil {
		return err
	}
	exists, err := s.catalogExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		if ifNotExists {
			return nil
		}
		return errorf(adbc.StatusAlreadyExists, "catalog %q already exists", name)
	}
	if count, err := s.redis(ctx).SCard(ctx, catalogsKey).Result(); err != nil {
		return err
	} else if count >= maxCatalogs-1 {
		return errorf(adbc.StatusInvalidState, "catalog capacity reached")
	}
	if err := s.redis(ctx).SAdd(ctx, catalogsKey, name).Err(); err != nil {
		return wrapRedis(err, "failed to create catalog")
	}
	return s.createSchema(ctx, catalogSchema(name, defaultSchema), false)
}
func (s *store) dropCatalog(ctx context.Context, name string, ifExists, cascade bool) error {
	if name == catalogName {
		return errorf(adbc.StatusInvalidArgument, "the default redis catalog cannot be dropped")
	}
	exists, err := s.catalogExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		if ifExists {
			return nil
		}
		return errorf(adbc.StatusNotFound, "catalog %q does not exist", name)
	}
	schemas, err := s.listSchemas(ctx)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		if schemaCatalog(schema) != name {
			continue
		}
		if err := s.dropSchema(ctx, schema, false, cascade); err != nil {
			return err
		}
	}
	return s.redis(ctx).SRem(ctx, catalogsKey, name).Err()
}

type CreateCatalogStmt struct {
	Name        string
	IfNotExists bool
}
type DropCatalogStmt struct {
	Name              string
	IfExists, Cascade bool
}

func (*CreateCatalogStmt) stmtNode() {}
func (*DropCatalogStmt) stmtNode()   {}
