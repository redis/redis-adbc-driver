// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

const snapshotSourceKeyField = "__adbc_snapshot_source_key"

// The original application key is provenance, never a physical HASH address.
// It remains a system column, excluded from SELECT * and public table schemas.
func snapshotSourceKeyColumn() columnMeta {
	return columnMeta{Name: keyField, Field: snapshotSourceKeyField, Type: typeString,
		Nullable: true, Indexed: true, TagsChecked: true, TagValues: tagsTruncated}
}

func (m *tableMeta) scanColumns() []columnMeta {
	if !m.SnapshotSourceKey {
		return m.Columns
	}
	return append(append([]columnMeta(nil), m.Columns...), snapshotSourceKeyColumn())
}
