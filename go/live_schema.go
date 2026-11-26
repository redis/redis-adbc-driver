// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// OptionStringSchemaOwnership selects the ownership of schemas created by
	// SQL CREATE SCHEMA: "managed" (default) or "live". Existing schemas keep
	// their ownership; live requires live_tables=true and the redis catalog.
	OptionStringSchemaOwnership = "adbc.redis.schema_ownership"
	// The live schema name map is managed catalog metadata, published together
	// with the managed schema set so both share one name-uniqueness check. Live
	// table definitions stay in the separate live control namespace.
	liveSchemasKey = metaPrefix + "live_schemas"
	liveMaxSchemas = 1024
)

// liveRef names the registry of one live schema. The built-in redis_live uses
// the original registry (empty id); named schemas are keyed by a stable random
// ID so a schema rename never rewrites table records.
type liveRef struct {
	schema string
	id     string
}

func (r liveRef) registry() string {
	if r.id == "" {
		return liveRegistry
	}
	return livePrefix + "schema:" + r.id + ":tables"
}

// stateKey holds the "dropping" tombstone of a named schema. It is absent for a
// usable schema; registration admission refuses while it exists.
func (r liveRef) stateKey() string { return livePrefix + "schema:" + r.id + ":state" }

func liveRefOf(m *tableMeta) liveRef { return liveRef{schema: m.Schema, id: m.LiveSchemaID} }

func validLiveSchemaID(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}

func schemaOwnershipValue(live bool) string {
	if live {
		return "live"
	}
	return "managed"
}

func parseSchemaOwnership(value string) (bool, error) {
	switch value {
	case "managed":
		return false, nil
	case "live":
		return true, nil
	}
	return false, errorf(adbc.StatusInvalidArgument, "%s must be managed or live", OptionStringSchemaOwnership)
}

// validLiveSchemaName rejects names that are reserved or cannot be stored.
func validLiveSchemaName(name string) error {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return errorf(adbc.StatusInvalidArgument, "live schema names must be 1..1024 bytes of valid UTF-8 without NUL")
	}
	// As in PostgreSQL, the pg_ prefix is reserved for system schemas.
	if name == defaultSchema || name == liveSchema || name == foreignSchema || strings.HasPrefix(name, "pg_") || isPgCatalog(name) || isInfoSchema(name) || reservedSchema(name) {
		return errorf(adbc.StatusInvalidArgument, "schema name %q is reserved", name)
	}
	return nil
}

// namedLiveCandidate cheaply excludes schemas that can never be named live
// schemas, so managed and catalog-qualified lookups never read the live map.
func namedLiveCandidate(schema string) bool {
	return validLiveSchemaName(schema) == nil
}

// liveSchemaRefIn reads the live name map under the caller's metadata scope.
// The read is a publication dependency of the caller's statement.
func (s *store) liveSchemaRefIn(ctx context.Context, schema string) (liveRef, bool, error) {
	if !s.snapshots || !namedLiveCandidate(schema) {
		return liveRef{}, false, nil
	}
	id, err := s.redis(ctx).HGet(ctx, liveSchemasKey, schema).Result()
	if errors.Is(err, goredis.Nil) {
		return liveRef{}, false, nil
	}
	if err != nil {
		return liveRef{}, false, wrapRedis(err, "failed to read live schema map")
	}
	if !validLiveSchemaID(id) {
		return liveRef{}, false, errorf(adbc.StatusIO, "invalid live schema map entry")
	}
	return liveRef{schema: schema, id: id}, true, nil
}

// liveSchemaRef resolves a named live schema, under the caller's scope when
// one exists and otherwise under a short metadata pin. The isolated live
// executor has no catalog access of its own and delegates to its parent store.
func (s *store) liveSchemaRef(ctx context.Context, schema string) (liveRef, bool, error) {
	if !namedLiveCandidate(schema) {
		return liveRef{}, false, nil
	}
	if s.liveExecution {
		if s.liveResolve == nil {
			return liveRef{}, false, nil
		}
		return s.liveResolve(ctx, schema)
	}
	if !s.snapshots {
		return liveRef{}, false, nil
	}
	if snapshotOf(ctx) != nil {
		return s.liveSchemaRefIn(ctx, schema)
	}
	sctx, pin, err := s.acquireSnapshot(context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil)))
	if err != nil {
		return liveRef{}, false, err
	}
	defer pin.release()
	return s.liveSchemaRefIn(sctx, schema)
}

// liveRefFor resolves any live schema this connection can use.
func (s *store) liveRefFor(ctx context.Context, schema string) (liveRef, bool, error) {
	if !s.liveTables {
		return liveRef{}, false, nil
	}
	if schema == liveSchema {
		return liveRef{schema: liveSchema}, true, nil
	}
	return s.liveSchemaRef(ctx, schema)
}

// liveAPIRef resolves the schema of a Go/CLI live administration call. An
// empty schema means redis_live. The connection's live_tables option does not
// apply: these calls administer registrations rather than run SQL.
func (s *store) liveAPIRef(ctx context.Context, schema string) (liveRef, error) {
	if schema == "" || schema == liveSchema {
		return liveRef{schema: liveSchema}, nil
	}
	if err := validLiveSchemaName(schema); err != nil {
		return liveRef{}, err
	}
	if err := s.initializeSnapshotImport(ctx); err != nil {
		return liveRef{}, err
	}
	ref, ok, err := s.liveSchemaRef(context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil)), schema)
	if err != nil {
		return liveRef{}, err
	}
	if !ok {
		return liveRef{}, stateErrorf(stateUndefinedSchema, "live schema %q does not exist", schema)
	}
	return ref, nil
}

// liveSchemaNameOf returns the current name mapped to a schema ID. A schema
// renamed meanwhile is still the same schema; a dropped one is reported absent.
func (s *store) liveSchemaNameOf(ctx context.Context, id string) (string, bool, error) {
	sctx, pin, err := s.acquireSnapshot(context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil)))
	if err != nil {
		return "", false, err
	}
	defer pin.release()
	n, err := s.redis(sctx).HLen(sctx, liveSchemasKey).Result()
	if err != nil {
		return "", false, wrapRedis(err, "failed to read live schema map")
	}
	if n > liveMaxSchemas {
		return "", false, errorf(adbc.StatusIO, "live schema capacity exceeded")
	}
	entries, err := s.redis(sctx).HGetAll(sctx, liveSchemasKey).Result()
	if err != nil {
		return "", false, wrapRedis(err, "failed to read live schema map")
	}
	for name, value := range entries {
		if value == id {
			return name, true, nil
		}
	}
	return "", false, nil
}

// isLiveSchemaName reports whether a managed object would be created in a
// live schema. Under a statement scope this is a publication dependency, so a
// concurrent CREATE SCHEMA cannot admit a managed object into a live schema.
func (s *store) isLiveSchemaName(ctx context.Context, schema string) (bool, error) {
	if s.liveExecution || !s.snapshots || !namedLiveCandidate(schema) {
		return false, nil
	}
	_, ok, err := s.liveSchemaRef(ctx, schema)
	return ok, err
}

func liveSchemaHoldsLiveTables(schema string) error {
	return errorf(adbc.StatusInvalidArgument, "schema %q holds live tables; managed tables, views, sequences and imports cannot be created in it", schema)
}

// resolveLiveSchema maps a stored record onto the registry it was read from.
// redis_live records keep their original schema field. Named records store an
// empty schema and their schema ID, so a schema rename never rewrites records.
func (r *liveRecord) resolveSchema(ref liveRef) bool {
	for _, m := range []*tableMeta{r.Meta, r.Pending} {
		if m == nil {
			continue
		}
		if m.LiveSchemaID != ref.id {
			return false
		}
		if ref.id == "" {
			if m.Schema != liveSchema {
				return false
			}
			continue
		}
		if m.Schema != "" || !validLiveSchemaID(m.LiveSchemaID) {
			return false
		}
		m.Schema = ref.schema
	}
	return true
}

// marshalLiveRecord is the only encoder for registry records.
func marshalLiveRecord(r liveRecord) ([]byte, error) {
	stored := func(m *tableMeta) *tableMeta {
		if m == nil || m.LiveSchemaID == "" {
			return m
		}
		c := *m
		c.Schema = ""
		return &c
	}
	r.Meta, r.Pending = stored(r.Meta), stored(r.Pending)
	return json.Marshal(r)
}

// createLiveSchema publishes a live schema name in one managed metadata
// publication: the name joins the managed schema set (which must not already
// hold it) and the live name map. No live control state is needed until a
// DROP SCHEMA tombstone; an empty registry is a usable schema.
func (s *store) createLiveSchema(ctx context.Context, name string, ifNotExists bool) error {
	if name == liveSchema {
		if ifNotExists {
			return nil
		}
		return stateErrorf(stateDuplicateSchema, "schema %q already exists", name)
	}
	if err := validLiveSchemaName(name); err != nil {
		return err
	}
	return s.liveCatalogWrite(ctx, func(wctx context.Context) (bool, error) {
		r := s.redis(wctx)
		if exists, err := r.HExists(wctx, liveSchemasKey, name).Result(); err != nil {
			return false, wrapRedis(err, "failed to read live schema map")
		} else if exists {
			if ifNotExists {
				return false, nil
			}
			return false, stateErrorf(stateDuplicateSchema, "schema %q already exists", name)
		}
		if n, err := r.HLen(wctx, liveSchemasKey).Result(); err != nil {
			return false, wrapRedis(err, "failed to read live schema map")
		} else if n >= liveMaxSchemas {
			return false, stateErrorf("54000", "at most %d live schemas are supported", liveMaxSchemas)
		}
		added, err := r.SAdd(wctx, schemasKey, name).Result()
		if err != nil {
			return false, wrapRedis(err, "failed to create schema")
		}
		if added == 0 {
			if ifNotExists {
				return false, nil
			}
			return false, stateErrorf(stateDuplicateSchema, "schema %q already exists", name)
		}
		if err := s.requireNoManagedObjects(wctx, name); err != nil {
			return false, err
		}
		if err := r.HSet(wctx, liveSchemasKey, name, snapshotID()).Err(); err != nil {
			return false, wrapRedis(err, "failed to create live schema")
		}
		return true, s.ensureNamespaceOID(wctx, name)
	})
}

// requireNoManagedObjects refuses a name that still has managed metadata.
func (s *store) requireNoManagedObjects(ctx context.Context, schema string) error {
	p := s.redis(ctx).Pipeline()
	tables := p.SCard(ctx, tablesKey(schema))
	views := p.SCard(ctx, viewsKey(schema))
	sequences := p.SCard(ctx, sequencesKey(schema))
	if _, err := p.Exec(ctx); err != nil {
		return wrapRedis(err, "failed to inspect schema")
	}
	if tables.Val()+views.Val()+sequences.Val() != 0 {
		return stateErrorf(stateDuplicateSchema, "schema %q already has managed objects", schema)
	}
	return nil
}

// liveCatalogWrite runs one managed metadata change in its own workspace and
// publishes it before returning. Live DDL never relies on the statement's own
// workspace, whose publication could otherwise fail after a committed change.
func (s *store) liveCatalogWrite(ctx context.Context, write func(context.Context) (bool, error)) error {
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	sctx, pin, err := s.acquireWritableSnapshot(ctx, nil)
	if err != nil {
		return err
	}
	defer pin.release()
	wctx, work, err := s.beginSnapshotWrite(sctx, pin)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			s.abortSnapshot(pin, work)
		}
	}()
	changed, err := write(wctx)
	if err != nil || !changed {
		return err
	}
	if err := s.publishSnapshot(wctx, pin, work); err != nil {
		return err
	}
	published = true
	return nil
}

// liveRegistryNames lists every record in a registry, whatever its state.
func (s *store) liveRegistryNames(ctx context.Context, ref liveRef) ([]string, error) {
	n, err := s.client.HLen(ctx, ref.registry()).Result()
	if err != nil {
		return nil, wrapRedis(err, "failed to read live registry")
	}
	if n > liveMaxTables {
		return nil, errorf(adbc.StatusIO, "live table capacity exceeded")
	}
	names, err := s.client.HKeys(ctx, ref.registry()).Result()
	if err != nil {
		return nil, wrapRedis(err, "failed to read live registry")
	}
	return names, nil
}

const liveSchemaTombstoneLua = `
if redis.call('HLEN',KEYS[1]) > 0 then return 0 end
redis.call('SET',KEYS[2],'dropping'); return 1`

// dropLiveSchema unregisters (with CASCADE) every table, fences new
// registrations with a tombstone, then removes the catalog name. Source HASHes
// are never read or changed. An interrupted drop keeps the tombstone, so the
// schema stays visible but refuses registrations until DROP SCHEMA is rerun.
func (s *store) dropLiveSchema(ctx context.Context, name string, ifExists, cascade bool) error {
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	ref, ok, err := s.liveSchemaRef(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		if ifExists {
			return nil
		}
		return stateErrorf(stateUndefinedSchema, "schema %q does not exist", name)
	}
	names, err := s.liveRegistryNames(ctx, ref)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		if !cascade {
			return stateErrorf("2BP01", "schema %q is not empty (%d live tables); use DROP SCHEMA … CASCADE to unregister them (source HASHes are kept)", name, len(names))
		}
		if err := s.dropLiveTables(ctx, ref, names, true); err != nil {
			return err
		}
	}
	n, err := once(ctx, s.client, "FCALL", redisFunctionName(liveSchemaTombstoneLua), 2, ref.registry(), ref.stateKey()).Int()
	if err != nil {
		return wrapRedis(err, "live schema drop admission outcome is uncertain; rerun DROP SCHEMA")
	}
	if n != 1 {
		return stateErrorf("40001", "a live table was registered in schema %q during DROP SCHEMA", name)
	}
	err = s.liveCatalogWrite(ctx, func(wctx context.Context) (bool, error) {
		r := s.redis(wctx)
		id, err := r.HGet(wctx, liveSchemasKey, name).Result()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return false, wrapRedis(err, "failed to read live schema map")
		}
		if id != ref.id {
			return false, stateErrorf("40001", "live schema %q changed during DROP SCHEMA; rerun DROP SCHEMA", name)
		}
		p := r.Pipeline()
		p.HDel(wctx, liveSchemasKey, name)
		p.SRem(wctx, schemasKey, name)
		p.Del(wctx, namespaceOIDKey(name))
		if _, err := p.Exec(wctx); err != nil {
			return false, wrapRedis(err, "failed to drop live schema")
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	// The schema is gone. A registrar that read the old map rechecks the map
	// after reserving and withdraws, so deleting the fence here is safe; a lost
	// reply leaves only an unreferenced tombstone.
	_ = once(ctx, s.client, "DEL", ref.stateKey(), ref.registry()).Err()
	return nil
}

// renameLiveSchema renames the catalog name in one publication. Records and
// in-flight live statements keep the schema ID and are unaffected.
func (s *store) renameLiveSchema(ctx context.Context, from, to string) error {
	if err := validLiveSchemaName(to); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	ref, ok, err := s.liveSchemaRef(ctx, from)
	if err != nil {
		return err
	}
	if !ok {
		return stateErrorf(stateUndefinedSchema, "schema %q does not exist", from)
	}
	if dropping, err := s.client.Exists(ctx, ref.stateKey()).Result(); err != nil {
		return wrapRedis(err, "failed to read live schema state")
	} else if dropping != 0 {
		return errorf(adbc.StatusInvalidState, "live schema %q is being dropped; rerun DROP SCHEMA", from)
	}
	return s.liveCatalogWrite(ctx, func(wctx context.Context) (bool, error) {
		r := s.redis(wctx)
		id, err := r.HGet(wctx, liveSchemasKey, from).Result()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return false, wrapRedis(err, "failed to read live schema map")
		}
		if id != ref.id {
			return false, stateErrorf("40001", "live schema %q changed during ALTER SCHEMA", from)
		}
		if exists, err := r.HExists(wctx, liveSchemasKey, to).Result(); err != nil {
			return false, wrapRedis(err, "failed to read live schema map")
		} else if exists {
			return false, stateErrorf(stateDuplicateSchema, "schema %q already exists", to)
		}
		added, err := r.SAdd(wctx, schemasKey, to).Result()
		if err != nil {
			return false, wrapRedis(err, "failed to rename schema")
		}
		if added == 0 {
			return false, stateErrorf(stateDuplicateSchema, "schema %q already exists", to)
		}
		if err := s.requireNoManagedObjects(wctx, to); err != nil {
			return false, err
		}
		oid, err := r.Get(wctx, namespaceOIDKey(from)).Result()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return false, wrapRedis(err, "failed to read namespace OID")
		}
		p := r.Pipeline()
		p.SRem(wctx, schemasKey, from)
		p.HDel(wctx, liveSchemasKey, from)
		p.HSet(wctx, liveSchemasKey, to, ref.id)
		if oid != "" {
			p.Set(wctx, namespaceOIDKey(to), oid, 0)
			p.Del(wctx, namespaceOIDKey(from))
		}
		if _, err := p.Exec(wctx); err != nil {
			return false, wrapRedis(err, "failed to rename live schema")
		}
		if oid == "" {
			return true, s.ensureNamespaceOID(wctx, to)
		}
		return true, nil
	})
}

// dropLiveTables preflights every target, then unregisters each one. DROP
// TABLE never reads, deletes, expires or rewrites source HASHes.
func (s *store) dropLiveTables(ctx context.Context, ref liveRef, names []string, ifExists bool) error {
	var targets []string
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		_, r, err := s.readLiveRecord(ctx, ref, name)
		if err != nil {
			var ae adbc.Error
			if ifExists && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
				continue
			}
			return err
		}
		if err := r.unregisterable(); err != nil {
			return err
		}
		targets = append(targets, name)
	}
	for _, name := range targets {
		if err := s.unregisterLive(ctx, ref, name); err != nil {
			return err
		}
	}
	return nil
}

func (r *liveRecord) unregisterable() error {
	if r.Pending != nil {
		return errorf(adbc.StatusInvalidState, "live table %q has an unfinished evolution; resume or cancel it first", r.Meta.Name)
	}
	if r.State != "building" && r.State != "ready" && r.State != "deleting" {
		return errorf(adbc.StatusInvalidState, "invalid live lifecycle state")
	}
	return nil
}

const liveRenameLua = `
if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[3] then return 0 end
if redis.call('HEXISTS',KEYS[1],ARGV[2]) == 1 then return -1 end
redis.call('HSET',KEYS[1],ARGV[2],ARGV[4]); redis.call('HDEL',KEYS[1],ARGV[1]); return 1`

// renameLiveTable moves a ready registration to a new SQL name. The index,
// prefix, source keys and LiveVersion are unchanged; statements still using
// the old name fail their identity check.
func (s *store) renameLiveTable(ctx context.Context, ref liveRef, from, to string) error {
	if to == "" || !utf8.ValidString(to) || strings.ContainsRune(to, 0) {
		return errorf(adbc.StatusInvalidArgument, "a live table name is required")
	}
	raw, r, err := s.readLiveRecord(ctx, ref, from)
	if err != nil {
		return err
	}
	if r.State != "ready" || r.Pending != nil {
		return errorf(adbc.StatusInvalidState, "live registration is %s; finish resume, cancellation or removal first", r.State)
	}
	if from == to {
		return stateErrorf(stateDuplicateTable, "relation %q already exists", to)
	}
	meta := *r.Meta
	meta.Name = to
	after, err := marshalLiveRecord(liveRecord{State: r.State, Meta: &meta})
	if err != nil {
		return err
	}
	n, err := once(ctx, s.client, "FCALL", redisFunctionName(liveRenameLua), 1, ref.registry(), from, to, raw, string(after)).Int()
	if err != nil {
		return wrapRedis(err, "live table rename outcome is uncertain; inspect the registration before retrying")
	}
	switch n {
	case 1:
		return nil
	case -1:
		return stateErrorf(stateDuplicateTable, "relation %q already exists", to)
	}
	return stateErrorf("40001", "live registration changed during rename")
}

// liveWithdrawLua deletes one registry record only if it is unchanged.
const liveWithdrawLua = `if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end; redis.call('HDEL',KEYS[1],ARGV[1]); return 1`

// confirmLiveReservation withdraws a just-reserved record if its schema was
// dropped after the registrar resolved it. Only a lost withdrawal reply can
// leave an unreachable building record; no index has been created yet.
func (s *store) confirmLiveReservation(ctx context.Context, meta *tableMeta, raw string) error {
	if meta.LiveSchemaID == "" {
		return nil
	}
	name, ok, err := s.liveSchemaNameOf(ctx, meta.LiveSchemaID)
	if err != nil {
		return wrapRedis(err, "live schema check after reservation failed; inspect the registration before retrying")
	}
	if ok {
		meta.Schema = name
		return nil
	}
	if _, err := once(ctx, s.client, "FCALL", redisFunctionName(liveWithdrawLua), 1, liveRefOf(meta).registry(), meta.Name, raw).Int(); err != nil {
		return wrapRedis(err, "live schema was dropped during registration; withdrawal outcome is uncertain")
	}
	return stateErrorf("40001", "live schema was dropped during registration")
}
