// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"sort"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const foreignSchema = "redis_foreign"

func virtualReadOnly(schema string) error {
	return errorf(adbc.StatusInvalidArgument, "schema %q is read-only", schema)
}

func parseForeignIndexes(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, errorf(adbc.StatusInvalidArgument, "%s must be true or false", OptionStringForeignIndexes)
}

// Discovery writes no metadata and uses a bounded index sample, never SCAN.
// Names match search indexes exactly; quote punctuation in SQL identifiers.
func (s *store) foreignNames(ctx context.Context) ([]string, error) {
	if !s.foreignIndexes {
		return nil, nil
	}
	h := &hashConn{st: s, client: s.client}
	indexes, _, err := h.searchIndexes(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ix := range indexes {
		if ix.keyType != "HASH" {
			continue
		}
		owned, err := s.client.SIsMember(ctx, indexesKey, ix.name).Result()
		if err != nil {
			return nil, wrapRedis(err, "failed to check index ownership")
		}
		if !owned {
			names = append(names, ix.name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *store) foreignTable(ctx context.Context, name string) (*tableMeta, error) {
	if !s.foreignIndexes {
		return nil, tableNotFound(foreignSchema, name)
	}
	owned, err := s.client.SIsMember(ctx, indexesKey, name).Result()
	if err != nil {
		return nil, wrapRedis(err, "failed to check index ownership")
	}
	if owned {
		return nil, tableNotFound(foreignSchema, name)
	}
	raw, err := s.searchDo(ctx, name, "FT.INFO", name).Result()
	if err != nil {
		if isUnknownIndex(err) {
			return nil, tableNotFound(foreignSchema, name)
		}
		return nil, wrapRedis(err, "failed to inspect foreign index")
	}
	info := indexInfoOf(raw)
	if info.keyType != "HASH" {
		return nil, tableNotFound(foreignSchema, name)
	}
	rows, err := s.aggregate(ctx, &aggRequest{index: name, query: "*", limit: &[2]int64{0, 128}, load: []string{"__key"}})
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r["__key"]
	}
	fetched, err := s.fetchKeyRows(ctx, keys, nil)
	if err != nil {
		return nil, err
	}
	var hashes []hashRow
	for _, r := range fetched {
		row := hashRow{key: r["__key"]}
		for field := range r {
			if field != "__key" {
				row.fields = append(row.fields, field)
			}
		}
		sort.Strings(row.fields)
		for _, field := range row.fields {
			row.values = append(row.values, r[field])
		}
		hashes = append(hashes, row)
	}
	cols, _, err := guessTable(hashes, guessOptions{})
	if err != nil {
		return nil, err
	}
	m := &tableMeta{Schema: foreignSchema, Name: name, IndexName: name, Foreign: true, Adopted: true, KeyRows: true, AppIndex: true, FoldedIdentifiers: true, readAt: time.Now()}
	if len(info.prefixes) > 0 {
		m.KeyPrefix = info.prefixes[0]
	}
	seen := map[string]bool{}
	for _, c := range cols {
		cm := columnMeta{Name: c.name, Type: c.inPlace, Nullable: true, Encoding: c.encoding}
		if cm.Encoding == "text" {
			cm.TextFormat = c.guess.format
		}
		m.Columns = append(m.Columns, cm)
		seen[c.fieldOrName()] = true
	}
	for _, a := range info.attrs {
		if seen[a.identifier] || a.identifier == "" || isSystemColumn(a.identifier) {
			continue
		}
		t := typeString
		if a.typ == "NUMERIC" {
			t = typeFloat64
		}
		m.Columns = append(m.Columns, columnMeta{Name: a.identifier, Type: t, Nullable: true})
		seen[a.identifier] = true
	}
	sort.Slice(m.Columns, func(i, j int) bool { return m.Columns[i].Name < m.Columns[j].Name })
	applyAppAttrs(m, info.attrs)
	return m, nil
}
