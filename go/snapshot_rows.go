// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"net/url"
	"strings"
)

const snapshotRowMarker = ":{meta}:snapshot:rows:"

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
	return snapshotSchemaPart(name) + snapshotRowMarker + id + ":"
}

// Return only canonical schema/UUID prefixes. Control records and indexes have
// separate identities; a matching UUID never authorizes another physical prefix.
func snapshotRowPrefix(key string) string {
	part, rest, ok := strings.Cut(key, snapshotRowMarker)
	if !ok || part == "" || len(rest) < 33 || rest[32] != ':' {
		return ""
	}
	schema, err := url.PathUnescape(part)
	if err != nil || snapshotSchemaPart(schema) != part {
		return ""
	}
	for _, c := range rest[:32] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return ""
		}
	}
	return part + snapshotRowMarker + rest[:33]
}

const snapshotRowLayoutLua = `
local function snapshot_row_key(key)
 if type(key)~='string' then return nil end
 local schema,id,suffix=string.match(key,'^([^:{}]+):{meta}:snapshot:rows:([0-9a-f]+):(.*)$')
 if not schema or schema=='adbc' or #id~=32 then return nil end
 return schema..':{meta}:snapshot:rows:'..id..':',id
end
local function valid_data_prefix(prefix,id)
 local parsed,data=snapshot_row_key(prefix)
 return parsed~=nil and parsed==prefix and data==id
end
`
