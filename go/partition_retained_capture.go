// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Internal source capture for the retained coordinator. It never migrates or
// owns source HASHes. SCAN identifies physical keys, not floating-point ID
// intervals; application/foreign indexes additionally gate each key's membership
// so their prefixes/filters remain authoritative. This freezes observed rows for
// one coordinator execution, not an atomic snapshot of concurrent source writes.
type retainedCapture struct {
	store            *store
	budget           *retainedBudget
	maxRows, maxKeys int
	rows, keys       int
	sources          []retainedCapturedSource
}
type retainedCapturedSource struct {
	meta       *tableMeta
	identity   string
	topology   string
	rows       int64
	checkCount bool
}

func retainedSourceIdentity(meta *tableMeta, info indexInfo) string {
	attrs := make([]any, len(info.attrs))
	for i, a := range info.attrs {
		options := make([][]byte, len(a.options))
		for j, option := range a.options {
			options[j] = []byte(option)
		}
		attrs[i] = []any{[]byte(a.identifier), []byte(a.name), []byte(a.typ), options}
	}
	prefixes := make([][]byte, len(info.prefixes))
	for i, prefix := range info.prefixes {
		prefixes[i] = []byte(prefix)
	}
	columns := make([]any, len(meta.Columns))
	for i, column := range meta.Columns {
		columns[i] = []any{[]byte(column.Name), []byte(column.Field), []byte(column.Encoding), []byte(column.TextFormat)}
	}
	data, _ := json.Marshal([]any{[]byte(meta.prefix()), []byte(meta.index()), columns, meta.Schema, meta.Name, meta.OID, meta.IndexOID, partitionTarget(meta),
		meta.Adopted, meta.KeyRows, meta.AppIndex, meta.Foreign, meta.FoldedIdentifiers, meta.PendingCleanup, meta.RekeyTo,
		[]byte(info.keyType), prefixes, []byte(info.filter), attrs})
	return retainedSHA(data)
}
func retainedByteGlob(prefix string) string {
	var out strings.Builder
	for i := 0; i < len(prefix); i++ {
		switch prefix[i] {
		case '*', '?', '[', ']', '\\':
			out.WriteByte('\\')
		}
		out.WriteByte(prefix[i])
	}
	return out.String() + "*"
}
func (c *retainedCapture) index(ctx context.Context, meta *tableMeta) (indexInfo, error) {
	if err := c.store.checkRangeReadiness(ctx, meta.index()); err != nil {
		return indexInfo{}, err
	}
	raw, err := c.store.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
	if err != nil {
		return indexInfo{}, wrapRedis(err, "retained source index unavailable")
	}
	info := indexInfoOf(raw)
	if info.keyType != "HASH" {
		return indexInfo{}, errorf(adbc.StatusNotImplemented, "retained source requires a HASH index")
	}
	return info, nil
}

func (c *retainedCapture) table(ctx context.Context, meta *tableMeta) (*tableMeta, error) {
	if c.budget == nil || c.maxRows < 1 || c.maxKeys < 1 || len(meta.Columns) > 1024 {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained source capture limits")
	}
	if meta.isMem || isTempSchema(meta.Schema) && !c.store.ownTempSchema(meta.Schema) || meta.RekeyTo != "" || len(meta.PendingCleanup) > 0 || meta.Adopted && !meta.KeyRows {
		return nil, errorf(adbc.StatusNotImplemented, "retained source layout is unowned temporary, moving, pending cleanup or requires explicit re-adoption")
	}
	topology, err := c.store.retainedTopology(ctx)
	if err != nil {
		return nil, err
	}
	info, err := c.index(ctx, meta)
	if err != nil {
		return nil, err
	}
	if !meta.AppIndex && !meta.Foreign && (len(info.prefixes) != 1 || info.prefixes[0] != meta.prefix()) {
		return nil, errorf(adbc.StatusInvalidState, "retained source index does not match its physical prefix")
	}
	identity := retainedSourceIdentity(meta, info)
	checkCount := !meta.KeyRows || meta.Foreign
	var initialCount int64
	if checkCount {
		initialCount, err = c.store.countMatches(ctx, meta.index(), "*")
		if err != nil {
			return nil, err
		}
	}

	prefixes := []string{meta.prefix()}
	if meta.Foreign {
		prefixes = info.prefixes
		if len(prefixes) == 0 {
			prefixes = []string{""}
		}
	}
	if err := c.budget.reserve(int64(len(meta.Columns))*1024 + 4096); err != nil {
		return nil, err
	}
	fields := []string{rowIDField}
	for _, col := range meta.Columns {
		fields = append(fields, col.field(), nullMarker(col.field()))
	}
	var rows []map[string]Value
	seen := map[string]bool{}
	hashes := &hashConn{client: c.store.client, st: c.store}
	for _, prefix := range prefixes {
		scan, err := hashes.scanKeys(ctx, prefix, "hash")
		if err != nil {
			return nil, err
		}
		// Preserve byte-oriented Redis keys, including invalid UTF-8 prefixes. The
		// ordinary discovery helper's rune escaping is not used for this match.
		scan.match = retainedByteGlob(prefix)
		for {
			keys, done, err := scan.next(ctx)
			if err != nil {
				return nil, err
			}
			eligible := make([]string, 0, len(keys))
			for _, key := range keys {
				if err := cancelled(ctx); err != nil {
					return nil, err
				}
				c.keys++
				if c.keys > c.maxKeys {
					return nil, c.budget.reserve(-1)
				}
				if seen[key] {
					continue
				}
				if err := c.budget.reserve(int64(len(key))*2 + 128); err != nil {
					return nil, err
				}
				seen[key] = true
				if !strings.HasPrefix(key, prefix) {
					return nil, errorf(adbc.StatusInvalidData, "retained source SCAN returned an out-of-scope key")
				}
				if meta.KeyRows && !meta.Foreign && !isRowKey(meta.prefix(), key) {
					continue
				}
				eligible = append(eligible, key)
			}
			eligible, err = c.members(ctx, meta, eligible)
			if err != nil {
				return nil, err
			}
			for first := 0; first < len(eligible); first += 128 {
				batch := eligible[first:min(first+128, len(eligible))]
				captured, err := c.readBatch(ctx, meta, fields, batch)
				if err != nil {
					return nil, err
				}
				rows = append(rows, captured...)
			}

			if done {
				break
			}
		}
	}
	// Preserve ordinary managed-table insertion order for LIMIT/OFFSET,
	// order-dependent aggregates and stable sort ties. Compare exact int64 IDs.
	if !meta.KeyRows {
		sort.Slice(rows, func(i, j int) bool { return rows[i][rowIDField].I < rows[j][rowIDField].I })
	}
	// Validate schema/layout after capture and again immediately before public
	// publication. Committed readers will never consult these sources afterward.
	if checkCount && initialCount != int64(len(rows)) {
		return nil, errorf(adbc.StatusInvalidState, "retained source membership changed or capture was incomplete")
	}
	source := retainedCapturedSource{meta: meta, identity: identity, topology: topology, rows: int64(len(rows)), checkCount: checkCount}
	if err := c.check(ctx, source); err != nil {
		return nil, err
	}
	c.sources = append(c.sources, source)
	snapshot := *meta
	snapshot.isMem = true
	snapshot.retainedSource = true
	snapshot.mem = rows
	return &snapshot, nil
}
func (c *retainedCapture) check(ctx context.Context, source retainedCapturedSource) error {
	topology, err := c.store.retainedTopology(ctx)
	if err != nil {
		return err
	}
	if topology != source.topology {
		return errorf(adbc.StatusInvalidState, "retained source Cluster routing changed during execution")
	}
	current, err := c.store.getTable(ctx, source.meta.Schema, source.meta.Name)
	if err != nil {
		return err
	}
	if current.AppIndex {
		if err := c.store.loadAppIndex(ctx, current); err != nil {
			return err
		}
	}
	info, err := c.index(ctx, current)
	if err != nil {
		return err
	}
	if retainedSourceIdentity(current, info) != source.identity {
		return errorf(adbc.StatusInvalidState, "retained source schema or layout changed during execution")
	}
	if source.checkCount {
		count, err := c.store.countMatches(ctx, current.index(), "*")
		if err != nil {
			return err
		}
		if count != source.rows {
			return errorf(adbc.StatusInvalidState, "retained source cardinality changed during execution")
		}
	}
	return nil
}
func (c *retainedCapture) checkAll(ctx context.Context) error {
	for _, source := range c.sources {
		if err := c.check(ctx, source); err != nil {
			return err
		}
	}
	return nil
}

func (c *retainedCapture) members(ctx context.Context, meta *tableMeta, keys []string) ([]string, error) {
	if !meta.AppIndex && !meta.Foreign {
		return keys, nil
	}
	node, err := c.store.search(ctx, meta.index())
	if err != nil {
		return nil, err
	}
	source, ok := node.c.(interface{ Pipeline() goredis.Pipeliner })
	if !ok {
		return nil, errorf(adbc.StatusInternal, "retained search client cannot pipeline")
	}
	var members []string
	for first := 0; first < len(keys); first += 128 {
		batch := keys[first:min(first+128, len(keys))]
		pipe := source.Pipeline()
		commands := make([]*goredis.Cmd, len(batch))
		for i, key := range batch {
			commands[i] = pipe.Do(ctx, "FT.SEARCH", meta.index(), "*", "INKEYS", 1, key, "LIMIT", 0, 0, "TIMEOUT", 0, "DIALECT", 2)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, wrapRedis(err, "retained source membership check failed")
		}
		for i, command := range commands {
			reply, err := command.Result()
			if err != nil {
				return nil, wrapRedis(err, "retained source membership check failed")
			}
			count, err := parseSearchCount(reply)
			if err != nil {
				return nil, err
			}
			if count == 0 {
				continue
			}
			if count != 1 {
				return nil, errorf(adbc.StatusInvalidData, "invalid retained source membership count")
			}
			members = append(members, batch[i])
		}
	}
	return members, nil
}
func (c *retainedCapture) readBatch(ctx context.Context, meta *tableMeta, fields, keys []string) ([]map[string]Value, error) {
	pipe := c.store.client.Pipeline()
	commands := make([]*goredis.SliceCmd, len(keys))
	types := make([]*goredis.StatusCmd, len(keys))
	for i, key := range keys {
		commands[i] = pipe.HMGet(ctx, key, fields...)
		if meta.KeyRows {
			types[i] = pipe.Type(ctx, key)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil && !isWrongType(err) {
		return nil, wrapRedis(err, "retained source HASH read failed")
	}
	var rows []map[string]Value
	for index, key := range keys {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		values, err := commands[index].Result()
		if isWrongType(err) {
			continue
		}
		if err != nil {
			return nil, wrapRedis(err, "retained source HASH read failed")
		}

		if len(values) != len(fields) {
			return nil, errorf(adbc.StatusInvalidData, "invalid retained source HASH reply")
		}
		if meta.KeyRows {
			kind, err := types[index].Result()
			if err != nil {
				return nil, wrapRedis(err, "retained source existence check failed")
			}
			if kind != "hash" {
				continue
			}

		} else {
			if values[0] == nil {
				continue
			}
			raw, ok := values[0].(string)
			id, err := strconv.ParseInt(raw, 10, 64)
			if !ok || err != nil || id < 1 || key != meta.prefix()+strconv.FormatInt(id, 10) {
				return nil, errorf(adbc.StatusInvalidData, "retained managed source has an inconsistent physical row identity")
			}
		}
		if c.rows >= c.maxRows {
			return nil, c.budget.reserve(-1)
		}
		bytes := int64(len(fields))*256 + 512
		for _, value := range values {
			if text, ok := value.(string); ok {
				bytes += int64(len(text)) * 2
			}
		}
		if err := c.budget.reserve(bytes); err != nil {
			return nil, err
		}
		raw := make(aggRow, len(fields)+1)
		for i, value := range values {
			if text, ok := value.(string); ok {
				raw[fields[i]] = text
			}
		}
		raw["__key"] = key
		row, err := decodeRow(meta, raw, nil)
		if err != nil {
			return nil, err
		}
		c.rows++
		rows = append(rows, row)
	}
	return rows, nil
}
