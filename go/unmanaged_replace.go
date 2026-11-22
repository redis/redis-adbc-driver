// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Consent is per exact logical target, in addition to the explicit physical
// identity in unmanaged_source. Redis cannot fence arbitrary application writers.
type unmanagedReplaceConsent struct {
	Schema             string `json:"schema"`
	Table              string `json:"table"`
	ExclusiveQuiescent bool   `json:"exclusive_quiescent"`
	DiscardTTLs        bool   `json:"discard_ttls"`
	ResetIndex         bool   `json:"reset_index"`
}

const unmanagedReplaceMaxKeys = 100000
const unmanagedReplaceMaxBytes = 64 << 20

type unmanagedReplaceImage struct {
	key, dump string
	expiry    int64
}
type unmanagedReplaceKey struct{}
type unmanagedReplaceProgress struct {
	phase, outcome string
	live           bool
	deleted        int64
	guard          func(context.Context, []string) error
}

func sameUnmanagedRegistration(a, b *tableMeta) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && bytes.Equal(x, y)
}

func (p *unmanagedReplaceProgress) failure(written int64, err error) error {
	var ae adbc.Error
	if !errors.As(err, &ae) {
		ae = adbc.Error{Code: adbc.StatusIO, Msg: err.Error()}
	}
	mode := "unmanaged"
	if p.live {
		mode = "live"
	}
	ae.Msg = mode + " Arrow replacement stopped in " + p.phase +
		"; inspect source HASHes, index and registration before recovery: " + ae.Msg
	for _, d := range [][2]string{
		{"acknowledged_deletes", strconv.FormatInt(p.deleted, 10)},
		{"acknowledged_rows", strconv.FormatInt(written, 10)},
		{"phase", p.phase}, {"current_outcome", p.outcome},
	} {
		// The append path already attached acknowledged_rows.
		found := false
		for _, existing := range ae.Details {
			if existing.Key() == "adbc.redis.ingest."+d[0] {
				found = true
			}
		}
		if !found {
			ae.Details = append(ae.Details, &adbc.TextErrorDetail{Name: "adbc.redis.ingest." + d[0], Detail: d[1]})
		}
	}
	return ae
}

// Both observations are atomic single-key reads. The guard refuses any observed
// type/value/absolute-expiry change, including expiry, before deleting that key.
const unmanagedReplaceInspectLua = `
for _, cmd in ipairs({'TYPE', 'DUMP', 'PEXPIRETIME', 'DEL', 'HSET', 'EXISTS'}) do
  local args = {cmd, KEYS[1]}
  if cmd == 'HSET' then args[3] = ARGV[1]; args[4] = '0' end
  if not redis.acl_check_cmd(unpack(args)) then return redis.error_reply('NOPERM replacement source command denied') end
end
if redis.call('TYPE', KEYS[1]).ok ~= 'hash' then return redis.error_reply('ERR replacement requires only stable HASH keys') end
local image = redis.call('DUMP', KEYS[1])
if #image > tonumber(ARGV[2]) then return redis.error_reply('ERR replacement inventory exceeds 64 MiB') end
return {image, redis.call('PEXPIRETIME', KEYS[1])}
`
const unmanagedReplaceDeleteLua = `
if redis.call('TYPE', KEYS[1]).ok ~= 'hash' or redis.call('DUMP', KEYS[1]) ~= ARGV[1] or tostring(redis.call('PEXPIRETIME', KEYS[1])) ~= ARGV[2] then return 0 end
return redis.call('DEL', KEYS[1])
`

func unmanagedReplaceScopeKey(prefix, key string) bool {
	return unmanagedSourceKey(key) && strings.HasPrefix(key, prefix) &&
		len(key) > len(prefix) && !strings.Contains(key[len(prefix):], ":")
}

func (s *store) requireEmptyReplacementPrefix(ctx context.Context, prefix string) error {
	err := s.requireEmptyUnmanagedPrefix(ctx, prefix)
	var ae adbc.Error
	if errors.As(err, &ae) && ae.Code == adbc.StatusAlreadyExists {
		return stateErrorf("40001", "source prefix changed during replacement")
	}
	return err
}

// The cap makes inventory retention explicit and bounded. SCAN cursors are
// ephemeral; Search membership is never used as an authoritative key inventory.
func (s *store) unmanagedReplaceInventory(ctx context.Context, meta *tableMeta) ([]unmanagedReplaceImage, error) {
	scan, err := (&hashConn{client: s.client}).scanKeys(ctx, meta.prefix(), "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []unmanagedReplaceImage
	var size int
	for {
		keys, done, err := scan.next(ctx)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if seen[key] {
				continue
			}
			if !unmanagedReplaceScopeKey(meta.prefix(), key) {
				return nil, errorf(adbc.StatusInvalidArgument, "replacement refuses nested or unsafe source keys")
			}
			if len(out) >= unmanagedReplaceMaxKeys {
				return nil, errorf(adbc.StatusInvalidArgument, "replacement inventory exceeds 100000 keys")
			}
			remaining := unmanagedReplaceMaxBytes - size - len(key) - 64
			if remaining < 0 {
				return nil, errorf(adbc.StatusInvalidArgument, "replacement inventory exceeds 64 MiB")
			}
			v, err := once(ctx, s.client, "FCALL", redisFunctionName(unmanagedReplaceInspectLua), 1, key, meta.Columns[0].field(), remaining).Slice()
			if err != nil {
				return nil, wrapRedis(err, "replacement inventory refused")
			}
			if len(v) != 2 {
				return nil, errorf(adbc.StatusIO, "invalid replacement inventory reply")
			}
			dump, ok := v[0].(string)
			expiry, expOK := v[1].(int64)
			if !ok || !expOK {
				return nil, errorf(adbc.StatusIO, "invalid replacement HASH observation")
			}
			size += len(key) + len(dump) + 64
			if size > unmanagedReplaceMaxBytes {
				return nil, errorf(adbc.StatusInvalidArgument, "replacement inventory exceeds 64 MiB")
			}
			seen[key] = true
			out = append(out, unmanagedReplaceImage{key, dump, expiry})
		}
		if done {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
}

func (s *store) currentUnmanagedRegistration(ctx context.Context, expected *tableMeta) error {
	if expected.Live {
		_, r, err := s.readLiveRecord(ctx, expected.Name)
		if err != nil {
			return err
		}
		if r.State != "ready" || !sameUnmanagedRegistration(r.Meta, expected) {
			return stateErrorf("40001", "live registration changed during replacement")
		}
		return nil
	}
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	c, pin, err := s.acquireSnapshot(ctx, true)
	if err != nil {
		return err
	}
	defer pin.release()
	actual, err := s.getTableMetadata(c, expected.Schema, expected.Name)
	if err != nil {
		return err
	}
	if !sameUnmanagedRegistration(actual, expected) {
		return stateErrorf("40001", "unmanaged registration changed during replacement")
	}
	return nil
}

// Include configuration that affects external consumers, rather than volatile
// FT.INFO document counts or indexing progress, in the definition identity.
func (s *store) unmanagedReplaceIndexDefinition(ctx context.Context, index string) (map[string]any, error) {
	r, err := s.searchDo(ctx, index, "FT.INFO", index).Result()
	if err != nil {
		return nil, wrapRedis(err, "replacement index unavailable")
	}
	f, ok := redisReplyFields(r)
	if !ok {
		return nil, errorf(adbc.StatusIO, "invalid replacement index definition")
	}
	out := map[string]any{}
	for _, key := range []string{"index_definition", "attributes", "index_options", "stopwords_list"} {
		out[key] = f[key]
	}
	return out, nil
}

func (s *store) checkUnmanagedReplaceScope(ctx context.Context, meta *tableMeta) error {
	if (!meta.Live && !meta.Unmanaged) || meta.UnmanagedForeign || meta.UnmanagedFilter != "" || len(meta.Columns) == 0 {
		return unmanagedUnsupported("replacement outside a dedicated unfiltered non-foreign source")
	}
	if !meta.Live && (len(meta.UnmanagedPrefixes) != 1 || meta.UnmanagedPrefixes[0] != meta.prefix()) {
		return unmanagedUnsupported("replacement outside a dedicated unfiltered non-foreign source")
	}
	var info indexInfo
	if meta.Live {
		if err := s.validateLiveIndex(ctx, meta); err != nil {
			return err
		}
		r, err := s.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
		if err != nil {
			return err
		}
		info = indexInfoOf(r)
	} else {
		var err error
		info, err = s.unmanagedIndexInfo(ctx, meta)
		if err != nil {
			return err
		}
	}
	for _, attr := range info.attrs {
		if meta.Live && attr.identifier == rowIDField {
			continue
		}
		if !slices.ContainsFunc(meta.Columns, func(c columnMeta) bool { return c.field() == attr.identifier && c.Name == attr.name }) {
			return unmanagedUnsupported("replacement of unmodeled or aliased index attributes")
		}
	}
	indexes, unreadable, err := (&hashConn{client: s.client, st: s}).searchIndexes(ctx)
	if err != nil {
		return err
	}
	if unreadable != 0 {
		return unmanagedUnsupported("replacement with unreadable index definitions")
	}
	for _, index := range indexes {
		if index.name == meta.index() {
			continue
		}
		for _, prefix := range index.prefixes {
			if prefixesOverlap(prefix, meta.prefix()) {
				return unmanagedUnsupported("replacement with overlapping application indexes")
			}
		}
	}
	// Refuse another registered consumer even if it uses the same index.
	schemas, err := s.listSchemas(ctx)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		names, err := s.listTables(ctx, schema)
		if err != nil {
			return err
		}
		for _, name := range names {
			if schema == meta.Schema && name == meta.Name {
				continue
			}
			other, err := s.getTableMetadata(ctx, schema, name)
			if err != nil {
				return err
			}
			if prefixesOverlap(other.prefix(), meta.prefix()) || other.index() == meta.index() {
				return unmanagedUnsupported("replacement with another registered source consumer")
			}
		}
	}
	return nil
}

func (s *statementImpl) executeUnmanagedReplace(ctx context.Context) (int64, error) {
	return s.executeSourceReplace(ctx, s.unmanagedSource, false)
}

func (s *statementImpl) executeSourceReplace(ctx context.Context, cfg *unmanagedIngestSource, live bool) (written int64, resultErr error) {
	p := &unmanagedReplaceProgress{phase: "preflight", outcome: "none", live: live}
	defer func() {
		if resultErr != nil {
			resultErr = p.failure(written, executionCloseError(ctx, resultErr))
		}
		s.clearParams()
	}()
	opts, st := s.ingest, s.conn.store
	consent := cfg.Replace
	schema := opts.SchemaName
	if schema == "" {
		schema = s.conn.sess.currentSchema()
	}
	if consent == nil || consent.Schema != schema || consent.Table != opts.TableName || !consent.ExclusiveQuiescent || !consent.DiscardTTLs || !consent.ResetIndex {
		return 0, errorf(adbc.StatusInvalidArgument, "replacement requires exact schema/table and exclusive_quiescent, discard_ttls and reset_index consent")
	}
	if !st.snapshots || snapshotOf(ctx) != nil || snapshotOperationRequestOf(ctx) != nil || opts.Temporary || isTempAlias(schema) || isTempSchema(schema) {
		return 0, unmanagedUnsupported("temporary, retained or staged replacement")
	}
	if opts.CatalogName != "" && opts.CatalogName != catalogName || opts.CatalogName == "" && s.conn.sess.currentCatalog() != catalogName {
		return 0, errorf(adbc.StatusNotFound, "catalog does not exist")
	}
	if !live {
		if err := st.checkWritableSchema(schema); err != nil {
			return 0, err
		}
	}
	ctx, cancel := statementOperationContext(ctx, s.conn.sess)
	defer cancel()
	c, pin, err := st.acquireSnapshot(ctx, true)
	if err != nil {
		return 0, err
	}
	defer pin.release()
	var old *tableMeta
	var liveBefore string
	if live {
		var record *liveRecord
		liveBefore, record, err = st.readLiveRecord(ctx, opts.TableName)
		if err == nil {
			if record.State != "ready" {
				return 0, errorf(adbc.StatusInvalidState, "live registration is not ready")
			}
			old = record.Meta
		}
	} else {
		old, err = st.getTableMetadata(c, schema, opts.TableName)
	}
	if err != nil {
		return 0, err
	}
	identity := *cfg
	identity.Encodings = nil // replacement encodings describe the new schema
	if live {
		err = identity.matchesLive(old)
	} else {
		err = identity.matches(old)
	}
	if err != nil {
		return 0, err
	}
	if err := st.checkUnmanagedReplaceScope(c, old); err != nil {
		return 0, err
	}
	if err := s.prepareExtensionInput(); err != nil {
		return 0, err
	}
	meta, err := s.newSourceIngestMeta(schema, cfg, live)
	if err != nil {
		return 0, err
	}
	definition, err := st.unmanagedReplaceIndexDefinition(ctx, old.index())
	if err != nil {
		return 0, err
	}
	guard := func(ctx context.Context) error {
		if err := st.currentUnmanagedRegistration(ctx, old); err != nil {
			return err
		}
		actual, err := st.unmanagedReplaceIndexDefinition(ctx, old.index())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, definition) {
			return stateErrorf("40001", "unmanaged index changed during replacement")
		}
		return nil
	}
	createArgs := []any{"FT.CREATE", cfg.Index, "ON", "HASH", "PREFIX", 1, cfg.Prefix, "SKIPINITIALSCAN", "SCHEMA", meta.Columns[0].field(), "TAG", "NOINDEX"}
	if live {
		createArgs = liveIndexArgs(meta)
	}
	// acl_check_cmd authenticates the script's actual principal. These checks
	// don't mutate keys or infer permissions from another connection/user.
	probe := cfg.Prefix + "replacement_permission_probe"
	for _, command := range [][]any{
		{"FT.DROPINDEX", cfg.Index}, createArgs, {"DEL", probe},
		{"HSET", probe, meta.Columns[0].field(), "0"}, {"TYPE", probe},
		{"DUMP", probe}, {"PEXPIRETIME", probe}, {"EXISTS", probe},
	} {
		args := []any{"FCALL", redisFunctionName("if not redis.acl_check_cmd(unpack(ARGV)) then return redis.error_reply('NOPERM replacement command denied') end return 1"), 0}
		args = append(args, command...)
		if err := once(ctx, st.client, args...).Err(); err != nil {
			return 0, wrapRedis(err, "replacement permission preflight failed")
		}
	}
	images, err := st.unmanagedReplaceInventory(ctx, old)
	if err != nil {
		return 0, err
	}
	if err := guard(ctx); err != nil {
		return 0, err
	}
	p.phase = "delete"
	for _, image := range images {
		if err := guard(ctx); err != nil {
			return 0, err
		}
		p.outcome = "unknown"
		n, err := once(ctx, st.client, "FCALL", redisFunctionName(unmanagedReplaceDeleteLua), 1, image.key, image.dump, strconv.FormatInt(image.expiry, 10)).Int()
		if err != nil {
			return 0, wrapRedis(err, "replacement delete was not replayed")
		}
		p.outcome = "acknowledged"
		if n != 1 {
			return 0, stateErrorf("40001", "source HASH changed during replacement")
		}
		p.deleted++
	}
	images = nil
	if err := st.requireEmptyReplacementPrefix(ctx, cfg.Prefix); err != nil {
		return 0, err
	}
	if err := guard(ctx); err != nil {
		return 0, err
	}
	p.phase, p.outcome = "drop_index", "unknown"
	if err := st.searchDo(ctx, cfg.Index, "FT.DROPINDEX", cfg.Index).Err(); err != nil {
		return 0, wrapRedis(err, "replacement index drop was not replayed")
	}
	if live {
		p.phase, p.outcome = "registration", "unknown"
		liveBefore, err = st.stageLiveReplacement(ctx, liveBefore, meta)
		if err != nil {
			return 0, err
		}
	}
	p.phase, p.outcome = "create_index", "unknown"
	if err := st.searchDo(ctx, cfg.Index, createArgs...).Err(); err != nil {
		return 0, wrapRedis(err, "replacement index creation was not replayed")
	}
	p.outcome = "acknowledged"
	if err := st.requireEmptyReplacementPrefix(ctx, cfg.Prefix); err != nil {
		return 0, err
	}
	if err := st.awaitIndexReady(ctx, cfg.Index); err != nil {
		return 0, err
	}
	if live {
		if err := st.validateLiveIndex(ctx, meta); err != nil {
			return 0, err
		}
	} else {
		newInfo, err := st.unmanagedIndexInfo(ctx, meta)
		if err != nil {
			return 0, err
		}
		if len(newInfo.attrs) != 1 || newInfo.attrs[0].identifier != meta.Columns[0].field() ||
			newInfo.attrs[0].name != meta.Columns[0].field() || newInfo.attrs[0].typ != "TAG" ||
			!slices.Contains(newInfo.attrs[0].options, "NOINDEX") {
			return 0, stateErrorf("40001", "replacement membership index changed during creation")
		}
		for _, option := range newInfo.attrs[0].options {
			if option != "NOINDEX" && option != "SEPARATOR ," {
				return 0, stateErrorf("40001", "replacement membership index options changed")
			}
		}
	}
	definition, err = st.unmanagedReplaceIndexDefinition(ctx, cfg.Index)
	if err != nil {
		return 0, err
	}
	p.phase, p.outcome = "registration", "unknown"
	if live {
		if err := st.publishLive(ctx, opts.TableName, liveBefore, meta); err != nil {
			return 0, err
		}
	} else {
		if err := st.registerUnmanagedExpected(ctx, meta, false, old); err != nil {
			return 0, err
		}
		p.outcome = "acknowledged"
		pin.release()
		var writePin *snapshotPin
		c, writePin, err = st.acquireSnapshot(ctx, true)
		if err != nil {
			return 0, err
		}
		defer writePin.release()
		actual, err := st.getTableMetadata(c, schema, opts.TableName)
		if err != nil {
			return 0, err
		}
		if !sameUnmanagedRegistration(meta, actual) {
			return 0, stateErrorf("40001", "unmanaged registration changed after replacement publication")
		}
		meta = actual
	}
	p.outcome = "acknowledged"

	old = meta
	seen, keyBytes := map[string]bool{}, 0
	p.guard = func(ctx context.Context, keys []string) error {
		if err := guard(ctx); err != nil {
			return err
		}
		for _, key := range keys {
			if !unmanagedReplaceScopeKey(cfg.Prefix, key) {
				return errorf(adbc.StatusInvalidArgument, "replacement input key is outside the dedicated collection")
			}
			if seen[key] {
				return stateErrorf(stateUnique, "duplicate replacement __key")
			}
			keyBytes += len(key) + 64
			if len(seen) >= unmanagedReplaceMaxKeys || keyBytes > unmanagedReplaceMaxBytes {
				return errorf(adbc.StatusInvalidArgument, "replacement input key inventory exceeds 100000 keys or 64 MiB")
			}
			seen[key] = true
		}
		return nil
	}
	p.phase, p.outcome = "write", "none"
	c = context.WithValue(c, unmanagedWriteKey{}, metaKey(schema, opts.TableName))
	c = context.WithValue(c, unmanagedReplaceKey{}, p)
	s.ingest.Mode = adbc.OptionValueIngestModeAppend
	defer func() { s.ingest = opts }()
	if live {
		writeCtx := context.WithValue(ctx, unmanagedReplaceKey{}, p)
		written, err = s.appendLiveArrow(writeCtx, meta)
	} else {
		written, err = s.executeIngest(c)
	}
	written = max(written, 0)
	if err != nil {
		return written, err
	}
	p.phase, p.outcome = "verify", "none"
	if err := guard(ctx); err != nil {
		return written, err
	}
	// Re-enumeration catches observed insertions/deletions missed by Search.
	final, err := st.unmanagedReplaceInventory(ctx, meta)
	if err != nil {
		return written, err
	}
	if len(final) != len(seen) {
		return written, stateErrorf("40001", "source key set changed during replacement")
	}
	for _, image := range final {
		if !seen[image.key] || image.expiry != -1 {
			return written, stateErrorf("40001", "source key set or TTL changed during replacement")
		}
	}
	return written, nil
}
