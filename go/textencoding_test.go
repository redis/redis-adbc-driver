// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import "testing"

func TestTextEncodingPreservesApplicationFormat(t *testing.T) {
	for _, tt := range []struct {
		typ                 ColType
		value, format, want string
	}{
		{typeBool, "false", "YES", "NO"},
		{typeBool, "true", "False", "True"},
		{typeBool, "true", "0", "1"},
		{typeTimestampTZ, "2024-01-02T12:00:00Z", "2024-01-01T14:00:00.000+02:00", "2024-01-02T14:00:00.000+02:00"},
		{typeTimestampTZ, "2024-01-02T12:00:00Z", "2024-01-01 12:00:00.000", "2024-01-02 12:00:00.000"},
		{typeTimestampTZ, "2024-01-02T12:00:00.123456Z", "2024-01-01T12:00:00.000Z", "2024-01-02T12:00:00.123456Z"},
	} {
		c := columnMeta{Type: tt.typ, Encoding: "text"}
		v, err := c.decode(tt.value)
		if err != nil {
			t.Fatal(err)
		}
		got := c.encode(v, tt.format)
		if got != tt.want {
			t.Errorf("encode %s as %s = %s, want %s", tt.value, tt.format, got, tt.want)
		}
		back, err := c.decode(got)
		if err != nil {
			t.Fatal(err)
		}
		if cmp, ok := compareValues(v, back); !ok || cmp != 0 {
			t.Errorf("encoding changed value %s -> %s", v.Text(), back.Text())
		}
	}
}
