// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"math"
	"math/big"
	"testing"
)

func TestSnapshotImportJournalValueRoundTrip(t *testing.T) {
	values := []Value{stringValue(""), nullValue(typeString), stringValue("nul\x00snow雪\xff"), binaryValue("\x00\xff"), intValue(typeInt64, 9007199254740993), decimalValue(big.NewInt(-123456789012345), 20, 4), floatValue(typeFloat64, math.NaN()), floatValue(typeFloat64, math.Inf(1)), floatValue(typeFloat64, math.Copysign(0, -1)), boolValue(true)}
	recordType, err := namedRecordOf([]structField{{Name: "text", Type: typeString, Nullable: true}, {Name: "exact", Type: typeInt64, Nullable: true}})
	if err != nil {
		t.Fatal(err)
	}
	record := recordValue([]Value{stringValue("nested\x00\xff"), intValue(typeInt64, 9007199254740993)})
	record.T = recordType
	values = append(values, record, arrayValue([]Value{intValue(typeInt64, 9007199254740993), nullValue(typeInt64)}, typeInt64))
	meta := &tableMeta{}
	row := map[string]Value{}
	for i, value := range values {
		name := string(rune('a' + i))
		meta.Columns = append(meta.Columns, columnMeta{Name: name, Type: value.T})
		row[name] = value
	}
	encoded := encodeSnapshotImportRow(meta, row)
	decoded, err := decodeSnapshotImportRow(meta, encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range decoded {
		if value.Null != values[i].Null || !value.Null && encodeStored(value) != encodeStored(values[i]) {
			t.Fatalf("journal changed field %d", i)
		}
	}
	for _, bad := range []string{"", encoded[:len(encoded)-1], encoded + "trailing", "\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"} {
		if _, err := decodeSnapshotImportRow(meta, bad); err == nil {
			t.Fatal("malformed journal accepted")
		}
	}
}
