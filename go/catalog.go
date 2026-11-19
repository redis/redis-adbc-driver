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
	redisclient "github.com/redis/go-redis/v9"
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
	if e.schema == "" && e.sess != nil {
		return e.sess.currentCatalog()
	}
	return schemaCatalog(e.schema)
}

func (s *store) catalogIdentity(ctx context.Context, name string) (string, error) {
	if name == catalogName {
		return catalogName, nil
	}
	if err := validateCatalogName(name); err != nil {
		return "", err
	}
	id, err := s.redis(ctx).HGet(ctx, catalogsKey, name).Result()
	if err == redisclient.Nil {
		return "", errorf(adbc.StatusNotFound, "catalog %q does not exist", name)
	}
	if err != nil {
		return "", wrapRedis(err, "failed to check catalog")
	}
	return id, nil
}
func (s *store) catalogExists(ctx context.Context, name string) (bool, error) {
	if name == catalogName {
		return true, nil
	}
	return s.redis(ctx).HExists(ctx, catalogsKey, name).Result()
}
func (e *executor) validateCurrentCatalog(ctx context.Context) error {
	if e.currentCatalog() == catalogName {
		return nil
	}
	id, err := e.store.catalogIdentity(ctx, e.currentCatalog())
	if err != nil {
		return err
	}
	if e.sess != nil && e.currentCatalog() == e.sess.currentCatalog() && e.sess.catalogID != id {
		return errorf(adbc.StatusInvalidState, "selected catalog changed; select the catalog again")
	}
	return nil
}
func (e *executor) targetCatalog(ctx context.Context, explicit string) (string, error) {
	name := explicit
	if name == "" {
		name = e.currentCatalog()
		if err := e.validateCurrentCatalog(ctx); err != nil {
			return "", err
		}
	}
	if name != catalogName && (e.retained != nil || sharedSnapshotScope(ctx) || e.store.hasSharedReadView()) {
		return "", namedCatalogExecutionUnsupported()
	}
	_, err := e.store.catalogIdentity(ctx, name)
	return name, err
}
func requireCatalogWrite(ctx context.Context) error {
	if scope := snapshotOf(ctx); scope == nil || !scope.write {
		return errorf(adbc.StatusInvalidState, "catalog DDL requires a managed write workspace")
	}
	return nil
}
func (s *store) listCatalogs(ctx context.Context) ([]string, error) {
	names, err := s.redis(ctx).HKeys(ctx, catalogsKey).Result()
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
	if err := requireCatalogWrite(ctx); err != nil {
		return err
	}
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
	if count, err := s.redis(ctx).HLen(ctx, catalogsKey).Result(); err != nil {
		return err
	} else if count >= maxCatalogs-1 {
		return errorf(adbc.StatusInvalidState, "catalog capacity reached")
	}
	if err := s.redis(ctx).HSet(ctx, catalogsKey, name, snapshotID()).Err(); err != nil {
		return wrapRedis(err, "failed to create catalog")
	}
	return s.createSchema(ctx, catalogSchema(name, defaultSchema), false)
}
func (s *store) dropCatalog(ctx context.Context, name string, ifExists, cascade bool) error {
	if err := requireCatalogWrite(ctx); err != nil {
		return err
	}
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
	return s.redis(ctx).HDel(ctx, catalogsKey, name).Err()
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

// resolveCatalogTable records the registry dependency before looking up an
// explicitly qualified relation. Keep resolveTable itself pure for the live
// pre-admission classifier, which runs before a managed snapshot is pinned.
func (e *executor) resolveCatalogTable(ctx context.Context, t TableName) (string, string, error) {
	if _, err := e.targetCatalog(ctx, t.Catalog); err != nil {
		return "", "", err
	}
	return e.resolveTable(t)
}

func namedCatalogExecutionUnsupported() error {
	return errorf(adbc.StatusNotImplemented, "named catalogs do not yet support shared read snapshots, retained execution or deferred partitions")
}
