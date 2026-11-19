// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// OptionStringLiveTables enables SQL access to registered, application-owned
	// HASHes. Managed tables keep their mandatory statement snapshots.
	OptionStringLiveTables = "adbc.redis.live_tables"
	liveSchema             = "redis_live"
	livePrefix             = metaPrefix + "live:1:"
	liveRegistry           = livePrefix + "tables"
	liveIndexPrefix        = "adbc_live1_{meta}_"
	liveMaxTables          = 1024
)

type liveRecord struct {
	State string     `json:"state"`
	Meta  *tableMeta `json:"meta"`
}

// LiveHashRegistration describes a live table. Index is driver-owned; Prefix
// still belongs to the application. Applied is true only after publication.
type LiveHashRegistration struct {
	Table   string       `json:"table"`
	Prefix  string       `json:"prefix"`
	Index   string       `json:"index"`
	Columns []HashColumn `json:"columns"`
	Applied bool         `json:"applied"`
}

func validLivePrefix(prefix string) error {
	if prefix == "" || !utf8.ValidString(prefix) || strings.Contains(prefix, "{meta}") || strings.HasPrefix(prefix, "adbc:") || strings.HasPrefix("adbc:", prefix) {
		return errorf(adbc.StatusInvalidArgument, "live HASH prefix must be nonempty and outside driver control/storage namespaces")
	}
	return nil
}

func (s *store) checkLiveNamespace(ctx context.Context) error {
	if !s.snapshots {
		return errorf(adbc.StatusInvalidState, "live tables require a normal SQL connection, not a retained-result-only connection")
	}
	sctx, pin, err := s.acquireSnapshot(ctx)
	if err != nil {
		return err
	}
	defer pin.release()
	used, err := s.redis(sctx).SIsMember(sctx, schemasKey, liveSchema).Result()
	if err != nil {
		return err
	}
	if used {
		return errorf(adbc.StatusAlreadyExists, "redis_live already exists as a managed schema")
	}
	return nil
}

// RegisterLiveHashes registers application HASHes without copying or modifying
// them. The shaping options match AdoptHashes, but Schema must be redis_live
// (or empty), and source indexes/TrustStrings are not accepted. Without Apply,
// only schema inference is performed. A failed registration retains its private
// index identity; ResumeLiveHashes observes it without replaying FT.CREATE.
func RegisterLiveHashes(ctx context.Context, options map[string]string, o HashAdoptOptions) (*LiveHashRegistration, error) {
	if err := validLivePrefix(o.Prefix); err != nil {
		return nil, err
	}
	if o.Schema != "" && o.Schema != liveSchema || o.Index != "" || o.TrustStrings || o.operation != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "live registration uses redis_live and a driver-owned index; source indexes, trusted strings and retained operations are unsupported")
	}
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	if c.db.flex {
		return nil, flexUnsupported("live HASH tables")
	}
	name := o.Table
	if name == "" {
		name = TableNameForPrefix(o.Prefix)
	}
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return nil, errorf(adbc.StatusInvalidArgument, "a live table name is required")
	}
	progress := o.Progress
	if progress == nil {
		progress = func(string) {}
	}
	a := &adopter{c: c, o: o, schema: liveSchema, table: name, index: liveIndexPrefix + snapshotID(), progress: progress, res: &HashAdoption{Report: &HashReport{Prefix: o.Prefix}}}
	if err := a.read(ctx); err != nil {
		return nil, err
	}
	meta, err := a.plan()
	if err != nil {
		return nil, err
	}
	meta.Live = true
	if len(meta.Columns) > 1024 {
		return nil, errorf(adbc.StatusInvalidArgument, "live tables support at most 1024 columns")
	}
	for i := range meta.Columns {
		col := &meta.Columns[i]
		if !utf8.ValidString(col.Name) || !utf8.ValidString(col.field()) || strings.HasPrefix(col.Name, "__") || strings.HasPrefix(col.field(), "__") {
			return nil, errorf(adbc.StatusInvalidArgument, "live column/field %q is reserved", col.Name)
		}
		// Application writes can introduce either fact at any time. Never learn
		// an optimistic index encoding from one initial source scan.
		col.NaNs = col.Type.Kind.isFloat()
		if col.Type.Kind == KindString {
			col.TagsChecked, col.TagValues = true, tagsTruncated
		}
	}
	res := &LiveHashRegistration{Table: liveSchema + "." + name, Prefix: o.Prefix, Index: a.index, Columns: a.res.Report.Columns}
	if !o.Apply {
		res.Index = ""
		return res, nil
	}
	if err := c.st.initializeSnapshotImport(ctx); err != nil {
		return res, err
	}
	// Do not hide a managed schema which an older driver allowed to use this
	// name. Catalog access remains under a real metadata pin.
	sctx, pin, err := c.st.acquireSnapshot(ctx)
	if err != nil {
		return res, err
	}
	used, err := c.st.redis(sctx).SIsMember(sctx, schemasKey, liveSchema).Result()
	pin.release()
	if err != nil {
		return res, err
	}
	if used {
		return res, errorf(adbc.StatusAlreadyExists, "redis_live already exists as a managed schema")
	}
	raw, err := json.Marshal(liveRecord{State: "building", Meta: meta})
	if err != nil {
		return res, err
	}
	if len(raw) > 1<<20 {
		return res, errorf(adbc.StatusInvalidArgument, "live descriptor exceeds 1 MiB")
	}
	n, err := once(ctx, c.client, "EVAL", `
if redis.call('HEXISTS',KEYS[1],ARGV[1]) == 1 then return 0 end
if redis.call('HLEN',KEYS[1]) >= tonumber(ARGV[3]) then return -1 end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]); return 1`, 1, liveRegistry, name, string(raw), liveMaxTables).Int()
	if err != nil {
		return res, wrapRedis(err, "live registration outcome is uncertain; inspect the registration before retrying")
	}
	if n != 1 {
		return res, errorf(adbc.StatusAlreadyExists, "live table already registered or live table capacity reached; use ResumeLiveHashes for an unfinished registration")
	}
	if err := c.st.searchDo(ctx, a.index, liveIndexArgs(meta)...).Err(); err != nil {
		return res, wrapRedis(err, "live index creation failed; registration identity retained; do not replay an uncertain create")
	}
	if err := a.waitIndexed(ctx); err != nil {
		return res, err
	}
	if err := c.st.publishLive(ctx, name, string(raw), meta); err != nil {
		return res, err
	}
	res.Applied = true
	return res, nil
}

// Keep exact document keys out of Search expressions: @__key can evaluate to
// NULL for NUL-containing keys. SQL collection membership is checked against the
// original bytes before INKEYS/fetch. The prefix index may also cover nested
// HASHes, but those can never become SQL rows.
func liveIndexArgs(m *tableMeta) []any {
	args := []any{"FT.CREATE", m.index(), "ON", "HASH", "PREFIX", 1, m.prefix(),
		"LANGUAGE_FIELD", "__adbc_language", "SCORE_FIELD", "__adbc_score", "PAYLOAD_FIELD", "__adbc_payload",
		"SCHEMA", rowIDField, "NUMERIC"}
	for _, c := range m.Columns {
		if c.Indexed {
			args = append(args, indexAttrArgs(c)...)
		}
	}
	return args
}

func (s *store) publishLive(ctx context.Context, name, before string, meta *tableMeta) error {
	if err := s.validateLiveIndex(ctx, meta); err != nil {
		return err
	}
	after, _ := json.Marshal(liveRecord{State: "ready", Meta: meta})
	n, err := once(ctx, s.client, "EVAL", `
if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]); return 1`, 1, liveRegistry, name, before, string(after)).Int()
	if err != nil {
		return wrapRedis(err, "live registration publication outcome is uncertain")
	}
	if n != 1 {
		return errorf(adbc.StatusInvalidState, "live registration changed during publication")
	}
	return nil
}

// ResumeLiveHashes completes a registration whose generated index already
// exists. It never recreates a missing index or replays an uncertain mutation.
func ResumeLiveHashes(ctx context.Context, options map[string]string, table string) error {
	c, err := dialHashes(ctx, options)
	if err != nil {
		return err
	}
	defer c.close(ctx)
	raw, r, err := c.st.readLiveRecord(ctx, table)
	if err != nil {
		return err
	}
	if r.State == "ready" {
		return c.st.validateLiveIndex(ctx, r.Meta)
	}
	if r.State != "building" {
		return errorf(adbc.StatusInvalidState, "live table is being removed")
	}
	a := &adopter{c: c, index: r.Meta.index(), o: HashAdoptOptions{Prefix: r.Meta.prefix()}, progress: func(string) {}}
	if err := a.waitIndexed(ctx); err != nil {
		return err
	}
	return c.st.publishLive(ctx, table, raw, r.Meta)
}

func (s *store) readLiveRecord(ctx context.Context, name string) (string, *liveRecord, error) {
	raw, err := s.client.HGet(ctx, liveRegistry, name).Result()
	if errors.Is(err, goredis.Nil) {
		return "", nil, tableNotFound(liveSchema, name)
	}
	if err != nil {
		return "", nil, wrapRedis(err, "failed to read live registration")
	}
	var r liveRecord
	if err := json.Unmarshal([]byte(raw), &r); err != nil || r.Meta == nil {
		return "", nil, errorf(adbc.StatusIO, "invalid live registration")
	}
	m := r.Meta
	if !m.Live || !m.Adopted || !m.KeyRows || m.AppIndex || m.Foreign || m.Schema != liveSchema || m.Name != name || !strings.HasPrefix(m.IndexName, liveIndexPrefix) || len(strings.TrimPrefix(m.IndexName, liveIndexPrefix)) != 32 || validLivePrefix(m.KeyPrefix) != nil || m.SnapshotRows != nil || m.SnapshotOverlay || len(m.SnapshotSegments) != 0 {
		return "", nil, errorf(adbc.StatusIO, "invalid live ownership descriptor")
	}
	return raw, &r, nil
}

func (s *store) validateLiveIndex(ctx context.Context, m *tableMeta) error {
	reply, err := s.searchDo(ctx, m.index(), "FT.INFO", m.index()).Result()
	if err != nil {
		return wrapRedis(err, "live table index is unavailable")
	}
	i := indexInfoOf(reply)
	if i.keyType != "HASH" || len(i.prefixes) != 1 || i.prefixes[0] != m.prefix() || i.filter != "" {
		return errorf(adbc.StatusInvalidState, "live index definition changed")
	}
	if i.indexing || i.percent < 1 || i.failures != 0 {
		return errorf(adbc.StatusInvalidData, "live index is not ready or has indexing failures; check source field encodings")
	}
	return validateLiveIndexDefinition(m, i)
}

func validateLiveIndexDefinition(m *tableMeta, i indexInfo) error {
	if i.keyType != "HASH" || len(i.prefixes) != 1 || i.prefixes[0] != m.prefix() || i.filter != "" {
		return errorf(adbc.StatusInvalidState, "live index definition changed")
	}
	dummy := false
	for _, a := range i.attrs {
		if a.identifier == rowIDField && a.name == rowIDField && a.typ == "NUMERIC" && len(a.options) == 0 {
			dummy = true
		}
	}
	if !dummy {
		return errorf(adbc.StatusInvalidState, "live index key attribute changed")
	}
	want := 1
	for _, c := range m.Columns {
		if !c.Indexed {
			continue
		}
		want++
		args := indexAttrArgs(c)
		// An attribute's identifier and alias must continue to identify the
		// registered HASH field. Type/options are checked against FT.CREATE.
		found := false
		for _, a := range i.attrs {
			if a.identifier == c.field() && a.name == c.field() && strings.EqualFold(a.typ, fmt.Sprint(args[1])) {
				expected := indexAttrOf(append([]any{"identifier", c.field(), "attribute", c.field(), "type", args[1]}, args[2:]...))
				actual := slices.Clone(a.options)
				// Numeric sortable fields are inherently unnormalized; Search
				// may report UNF even when FT.CREATE omitted that redundant flag.
				if a.typ == "NUMERIC" {
					actual = slices.DeleteFunc(actual, func(v string) bool { return v == "UNF" })
				}
				slices.Sort(expected.options)
				slices.Sort(actual)
				found = slices.Equal(expected.options, actual)
			}
		}
		if !found {
			return errorf(adbc.StatusInvalidState, "live index attribute changed")
		}
	}
	if len(i.attrs) != want {
		return errorf(adbc.StatusInvalidState, "live index schema changed")
	}
	return nil
}

func (s *store) liveTable(ctx context.Context, name string) (*tableMeta, error) {
	if !s.liveTables {
		return nil, tableNotFound(liveSchema, name)
	}
	if sharedSnapshotScope(ctx) {
		return nil, errorf(adbc.StatusNotImplemented, "live tables do not participate in shared read snapshots")
	}
	if scope := snapshotOf(ctx); scope != nil && scope.write {
		return nil, errorf(adbc.StatusNotImplemented, "managed writes cannot access live tables")
	}
	_, r, err := s.readLiveRecord(ctx, name)
	if err != nil {
		return nil, err
	}
	if r.State != "ready" {
		return nil, errorf(adbc.StatusInvalidState, "live registration is %s", r.State)
	}
	if err := s.validateLiveIndex(ctx, r.Meta); err != nil {
		return nil, err
	}
	r.Meta.readAt = time.Now()
	hydrateCatalogOIDs(r.Meta)
	return r.Meta, nil
}

func (s *store) liveNames(ctx context.Context) ([]string, error) {
	if !s.liveTables {
		return nil, nil
	}
	n, err := s.client.HLen(ctx, liveRegistry).Result()
	if err != nil {
		return nil, err
	}
	if n > liveMaxTables {
		return nil, errorf(adbc.StatusIO, "live table capacity exceeded")
	}
	entries, err := s.client.HGetAll(ctx, liveRegistry).Result()
	if err != nil {
		return nil, err
	}
	var names []string
	for name, raw := range entries {
		var r liveRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, errorf(adbc.StatusIO, "invalid live registration")
		}
		if r.State == "ready" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func (s *store) checkLiveIdentity(ctx context.Context, m *tableMeta) error {
	_, r, err := s.readLiveRecord(ctx, m.Name)
	if err != nil {
		return err
	}
	if r.State != "ready" || r.Meta.prefix() != m.prefix() || r.Meta.index() != m.index() {
		return errorf(adbc.StatusInvalidState, "live registration changed during execution")
	}
	return nil
}

// Search aggregate LOAD @__key can return NULL for binary document keys. Keep
// exact keys from bounded source SCAN pages, ask Search only for membership,
// and fetch authoritative HASH images. INKEYS avoids deep Search OFFSET caps.
func (s *store) liveCandidates(ctx context.Context, m *tableMeta, query string, keys []string) ([]string, error) {
	if err := s.checkLiveIdentity(ctx, m); err != nil {
		return nil, err
	}
	if query == "" || query == "*" || len(keys) == 0 {
		return keys, nil
	}
	cap := s.paginationCaps(ctx, m.index())[0]
	if cap < 1 {
		return keys, nil
	} // full SQL residual remains active
	page := int(min(int64(128), cap))
	var out []string
	for start := 0; start < len(keys); start += page {
		batch := keys[start:min(start+page, len(keys))]
		args := []any{"FT.SEARCH", m.index(), query, "NOCONTENT", "INKEYS", len(batch)}
		allowed := make(map[string]bool, len(batch))
		for _, key := range batch {
			args = append(args, key)
			allowed[key] = true
		}
		args = append(args, "LIMIT", 0, len(batch), "TIMEOUT", 0, "DIALECT", 2)
		reply, err := s.searchDo(ctx, m.index(), args...).Result()
		if err != nil {
			return nil, wrapRedis(err, "live candidate lookup failed")
		}
		if snapshotReplyWarning(reply) {
			return nil, errorf(adbc.StatusIO, "live candidate lookup returned a partial-result warning")
		}
		matched, err := parseSearchKeys(reply)
		if err != nil {
			return nil, err
		}
		count, err := parseSearchCount(reply)
		if err != nil {
			return nil, err
		}
		if count != int64(len(matched)) {
			return nil, errorf(adbc.StatusIO, "live candidate lookup was incomplete")
		}
		for _, key := range matched {
			if !allowed[key] {
				return nil, errorf(adbc.StatusIO, "live candidate key is outside its requested batch")
			}
			delete(allowed, key)
			out = append(out, key)
		}
	}
	return out, nil
}

func (s *store) deleteLiveKeys(ctx context.Context, m *tableMeta, keys []string) error {
	for _, key := range keys {
		if !isRowKey(m.prefix(), key) {
			return errorf(adbc.StatusIO, "live DELETE key is outside its registered collection")
		}
		if err := s.checkLiveIdentity(ctx, m); err != nil {
			return err
		}
		n, err := once(ctx, s.redis(ctx), "EVAL", `if redis.call('TYPE',KEYS[1]).ok ~= 'hash' then return 0 end; return redis.call('DEL',KEYS[1])`, 1, key).Int()
		if err != nil {
			return wrapRedis(err, "live DELETE outcome is uncertain; earlier row changes may have applied")
		}
		if n != 1 {
			return stateErrorf("40001", "live row disappeared or changed type during DELETE; earlier row changes may have applied")
		}
	}
	return nil
}

// Live writes deliberately run outside managed publication. The isolated store
// can resolve only live tables, so even nested SQL cannot mutate managed rows.
// The execution's original transport still owns cancellation and connection Close.
func (e *executor) tryLiveWrite(ctx context.Context, ps ParsedStmt, params []Value, types []ColType) (bool, execResult, error) {
	if !e.store.liveTables {
		return false, execResult{}, nil
	}
	var target *TableName
	switch st := ps.Stmt.(type) {
	case *InsertStmt:
		target = &st.Table
	case *UpdateStmt:
		target = &st.Table
	case *DeleteStmt:
		target = &st.Table
	}
	if target == nil {
		return false, execResult{}, nil
	}
	schema, _, err := e.resolveTable(*target)
	if err != nil || schema != liveSchema {
		return false, execResult{}, nil
	}
	if snapshotOperationRequestOf(ctx) != nil || e.retained != nil {
		return true, execResult{}, errorf(adbc.StatusNotImplemented, "live writes cannot prepare retained operations")
	}
	for _, cte := range statementWith(ps.Stmt) {
		if cte.Statement != nil {
			return true, execResult{}, errorf(adbc.StatusNotImplemented, "live writes do not support modifying CTEs")
		}
	}
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	local := *e
	local.store = &store{client: e.store.redis(ctx), liveTables: true, liveExecution: true}
	res, err := local.execute(ctx, ps, params, types, nil)
	return true, res, err
}

// UnregisterLiveHashes drops only the driver's index and registration. It
// never uses DD and never deletes, expires, rewrites or scans source HASHes.
// A building registration can be removed only after its matching index exists;
// an unknown create with no observable index retains its recovery identity.
func UnregisterLiveHashes(ctx context.Context, options map[string]string, table string) error {
	c, err := dialHashes(ctx, options)
	if err != nil {
		return err
	}
	defer c.close(ctx)
	raw, r, err := c.st.readLiveRecord(ctx, table)
	if err != nil {
		return err
	}
	if r.State != "building" && r.State != "ready" && r.State != "deleting" {
		return errorf(adbc.StatusInvalidState, "invalid live lifecycle state")
	}
	{
		// An existing, matching index proves that the only FT.CREATE has
		// executed. The creator can still publish only by CAS of this record.
		// Absence cannot prove a lost/delayed create never ran, so retain it.
		reply, err := c.st.searchDo(ctx, r.Meta.index(), "FT.INFO", r.Meta.index()).Result()
		if err != nil && !(r.State != "building" && isUnknownIndex(err)) {
			return wrapRedis(err, "unfinished live index creation is unresolved; registration retained")
		}
		if err == nil {
			if err := validateLiveIndexDefinition(r.Meta, indexInfoOf(reply)); err != nil {
				return err
			}
		}
	}
	if r.State != "deleting" {
		r.State = "deleting"
		next, _ := json.Marshal(r)
		n, err := once(ctx, c.client, "EVAL", `if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end; redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]); return 1`, 1, liveRegistry, table, raw, string(next)).Int()
		if err != nil {
			return wrapRedis(err, "live removal admission outcome is uncertain")
		}
		if n != 1 {
			return errorf(adbc.StatusInvalidState, "live registration changed")
		}
		raw = string(next)
	}
	if err := c.st.searchDo(ctx, r.Meta.index(), "FT.DROPINDEX", r.Meta.index()).Err(); err != nil && !isUnknownIndex(err) {
		return wrapRedis(err, "live index removal outcome is uncertain; registration retained")
	}
	n, err := once(ctx, c.client, "EVAL", `if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end; redis.call('HDEL',KEYS[1],ARGV[1]); return 1`, 1, liveRegistry, table, raw).Int()
	if err != nil {
		return wrapRedis(err, "live removal publication outcome is uncertain")
	}
	if n != 1 {
		return errorf(adbc.StatusInvalidState, "live registration changed")
	}
	return nil
}
