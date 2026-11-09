// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"net/url"
	"strings"
)

const snapshotRowMarker = ":{meta}:snapshot:17:rows:"

func snapshotSchemaPart(schema string) string {
	part := escapeKeyPart(schema)
	// Keep even a schema named adbc outside the metadata ACL namespace.
	if part == "adbc" {
		return "%61dbc"
	}
	return part
}

func snapshotRows(id string, schema ...string) string {
	name := defaultSchema
	if len(schema) > 0 {
		name = schema[0]
	}
	access := id
	if len(schema) > 1 && schema[1] != "" {
		access = schema[1]
	}
	return access + "~" + snapshotSchemaPart(name) + snapshotRowMarker + id + ":"
}

// Return only canonical schema/UUID prefixes. Control records and indexes have
// separate identities; a matching UUID never authorizes another physical prefix.
func snapshotRowPrefix(key string) string {
	part, rest, ok := strings.Cut(key, snapshotRowMarker)
	if !ok || part == "" || len(rest) < 33 || rest[32] != ':' {
		return ""
	}
	access, encoded, ok := strings.Cut(part, "~")
	if !ok || len(access) != 32 || strings.Trim(access, "0123456789abcdef") != "" {
		return ""
	}
	schema, err := url.PathUnescape(encoded)
	if err != nil || snapshotSchemaPart(schema) != encoded {
		return ""
	}
	for _, c := range rest[:32] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return ""
		}
	}
	return part + snapshotRowMarker + rest[:33]
}

// An opaque logical table identity precedes schema and generation components.
// Grants therefore survive physical replacement without matching another table.
func snapshotAccessID(prefix string) string {
	if snapshotRowPrefix(prefix) == "" {
		return ""
	}
	return prefix[:32]
}
func snapshotAccessPrefix(meta *tableMeta) string { return meta.SnapshotAccessID + "~" }

const snapshotRowLayoutLua = `
local function snapshot_row_key(key)
 if type(key)~='string' then return nil end
 local access,schema,id,suffix=string.match(key,'^([0-9a-f]+)~([^:{}]+):{meta}:snapshot:17:rows:([0-9a-f]+):(.*)$')
 if not access or #access~=32 or schema=='adbc' or #id~=32 then return nil end
 return access..'~'..schema..':{meta}:snapshot:17:rows:'..id..':',id,access
end
local function valid_data_prefix(prefix,id)
 local parsed,data=snapshot_row_key(prefix)
 return parsed~=nil and parsed==prefix and data==id
end
`
