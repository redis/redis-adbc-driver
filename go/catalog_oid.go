// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"hash/fnv"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
)

func (s *store) nextCatalogOID(ctx context.Context) (int64, error) {
	n, err := s.client.IncrBy(ctx, metaPrefix+"oid:next", 1).Result()
	if err != nil {
		return 0, wrapRedis(err, "failed to allocate catalog OID")
	}
	if n > 999983615 {
		return 0, stateErrorf(stateNumericRange, "catalog OID space exhausted")
	}
	return 16384 + n, nil
}

func namespaceOIDKey(name string) string { return metaPrefix + "namespace:" + escapeKeyPart(name) }
func (s *store) ensureNamespaceOID(ctx context.Context, name string) error {
	if name == defaultSchema || name == infoSchema || name == pgCatalog || name == foreignSchema || isTempSchema(name) {
		return nil
	}
	key := namespaceOIDKey(name)
	if _, err := s.client.Get(ctx, key).Result(); err == nil {
		return nil
	} else if !errors.Is(err, goredis.Nil) {
		return wrapRedis(err, "failed to read namespace OID")
	}
	id, err := s.nextCatalogOID(ctx)
	if err != nil {
		return err
	}
	return wrapRedis(s.client.SetNX(ctx, key, id, 0).Err(), "failed to save namespace OID")
}

func hydrateCatalogOIDs(m *tableMeta) {
	if m.OID == 0 {
		m.OID = tableOID(m)
	}
	if m.IndexOID == 0 {
		m.IndexOID = indexOID(m)
	}
	for i := range m.Columns {
		if m.Columns[i].Default != "" && m.Columns[i].DefaultOID == 0 {
			m.Columns[i].DefaultOID = defaultOID(m, m.Columns[i])
		}
	}
	for i := range m.Checks {
		if m.Checks[i].OID == 0 {
			m.Checks[i].OID = checkOID(m, m.Checks[i])
		}
	}
	if m.PrimaryKey != nil {
		if m.PrimaryKey.OID == 0 {
			m.PrimaryKey.OID = keyOID(m, false)
		}
		if m.PrimaryKey.IndexOID == 0 {
			m.PrimaryKey.IndexOID = keyOID(m, true)
		}
	}
}

func (s *store) assignCatalogOIDs(ctx context.Context, m *tableMeta) error {
	if err := s.ensureNamespaceOID(ctx, m.Schema); err != nil {
		return err
	}
	ids := []*int64{&m.OID, &m.IndexOID}
	for i := range m.Columns {
		if m.Columns[i].Default != "" {
			ids = append(ids, &m.Columns[i].DefaultOID)
		}
	}
	for i := range m.Checks {
		ids = append(ids, &m.Checks[i].OID)
	}
	if m.PrimaryKey != nil {
		ids = append(ids, &m.PrimaryKey.OID, &m.PrimaryKey.IndexOID)
	}
	for _, id := range ids {
		if *id == 0 {
			n, err := s.nextCatalogOID(ctx)
			if err != nil {
				return err
			}
			*id = n
		}
	}
	return nil
}

// Metadata predating catalog IDs stays readable without a migration write,
// including for ACL readers. Physical prefixes survive a logical rename.
func legacyCatalogOID(parts ...string) int64 {
	h := fnv.New32a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return 1000000000 + int64(h.Sum32()%1000000000)
}
func tableOID(m *tableMeta) int64 {
	if m.Foreign {
		return legacyCatalogOID("foreign-table", m.Schema, m.Name)
	}
	if m.OID != 0 {
		return m.OID
	}
	return legacyCatalogOID("table", m.prefix())
}
func indexOID(m *tableMeta) int64 {
	if m.Foreign {
		return legacyCatalogOID("foreign-index", m.Schema, m.Name)
	}
	if m.IndexOID != 0 {
		return m.IndexOID
	}
	return legacyCatalogOID("index", m.prefix())
}
func viewOID(m *viewMeta) int64 {
	if m.OID != 0 {
		return m.OID
	}
	return legacyCatalogOID("view", m.Schema, m.Name)
}
func namespaceOID(name string) int64 {
	switch name {
	case pgCatalog:
		return 11
	case "public":
		return 2200
	case infoSchema:
		return 13207
	}
	return legacyCatalogOID("schema", name)
}
func defaultOID(m *tableMeta, c columnMeta) int64 {
	if c.DefaultOID != 0 {
		return c.DefaultOID
	}
	return legacyCatalogOID(strconv.FormatInt(tableOID(m), 10), "default", c.field())
}
func keyOID(m *tableMeta, index bool) int64 {
	if index && m.PrimaryKey.IndexOID != 0 {
		return m.PrimaryKey.IndexOID
	}
	if !index && m.PrimaryKey.OID != 0 {
		return m.PrimaryKey.OID
	}
	return legacyCatalogOID(strconv.FormatInt(tableOID(m), 10), "key", strconv.FormatBool(index), m.PrimaryKey.Name)
}
func checkOID(m *tableMeta, c checkMeta) int64 {
	if c.OID != 0 {
		return c.OID
	}
	return legacyCatalogOID(strconv.FormatInt(tableOID(m), 10), "check", c.Name)
}

func postgresTypeOID(t ColType) int64 {
	if n := map[string]int64{"oid": 26, "regclass": 2205, "regtype": 2206, "regproc": 24, "regnamespace": 4089}[t.Alias]; n != 0 {
		return n
	}
	switch t.Kind {
	case KindBool:
		return 16
	case KindBinary:
		return 17
	case KindInt64:
		return 20
	case KindInt16:
		return 21
	case KindInt32:
		return 23
	case KindFloat32:
		return 700
	case KindFloat64:
		return 701
	case KindDecimal:
		return 1700
	case KindString:
		if t.Text {
			return 25
		}
		if t.Fixed {
			return 1042
		}
		return 1043
	case KindDate:
		return 1082
	case KindTime:
		return 1083
	case KindTimeTZ:
		return 1266
	case KindTimestamp:
		if t.TZ != "" {
			return 1184
		}
		return 1114
	case KindInterval:
		return 1186
	}
	return 0
}
func postgresTypmod(t ColType) int64 {
	switch t.Kind {
	case KindString:
		if t.Length > 0 {
			return int64(t.Length) + 4
		}
	case KindDecimal:
		return (int64(t.Precision) << 16) | int64(t.Scale) + 4
	case KindTime, KindTimeTZ, KindTimestamp:
		return int64(t.fracDigits())
	}
	return -1
}
