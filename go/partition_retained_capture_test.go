// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import "testing"

func TestRetainedSourceByteGlob(t *testing.T) {
	for prefix, expected := range map[string]string{"app:binary\x00\xff:": "app:binary\x00\xff:*", "a[b]*?\\": "a\\[b\\]\\*\\?\\\\*", "é:": "é:*"} {
		if actual := retainedByteGlob(prefix); actual != expected {
			t.Fatalf("glob changed prefix bytes: %q != %q", actual, expected)
		}
	}
}

func TestRetainedSourceIdentityPreservesBinaryDefinitions(t *testing.T) {
	left := &tableMeta{Schema: foreignSchema, Name: "index", Foreign: true, KeyPrefix: "app:\xff:"}
	right := *left
	right.KeyPrefix = "app:\xfe:"
	before := indexInfo{keyType: "HASH", prefixes: []string{left.KeyPrefix}, filter: "@n > 0"}
	after := before
	after.prefixes = []string{right.KeyPrefix}
	if retainedSourceIdentity(left, before) == retainedSourceIdentity(&right, after) {
		t.Fatal("different binary index prefixes collapsed to one identity")
	}
}
