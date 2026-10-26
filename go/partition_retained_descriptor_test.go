// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestRetainedPartitionDescriptor(t *testing.T) {
	d := retainedPartitionID{Version: 2, Reference: retainedReference{Namespace: "tenant_one", JobID: strings.Repeat("a", 32), Manifest: strings.Repeat("b", 64), Expires: 12345678}, Start: 0, End: 1024}
	raw, _ := json.Marshal(d)
	got, err := decodeRetainedPartition(raw)
	if err != nil || got != d {
		t.Fatal(got, err)
	}
	for _, mutate := range []func(*retainedPartitionID){
		func(d *retainedPartitionID) { d.Version = 1 }, func(d *retainedPartitionID) { d.Version = 3 },
		func(d *retainedPartitionID) { d.Reference.Namespace = "bad}slot{" },
		func(d *retainedPartitionID) { d.Reference.JobID = "../foreign" },
		func(d *retainedPartitionID) { d.Reference.Manifest = "not-a-digest" },
		func(d *retainedPartitionID) { d.Reference.Expires = 0 },
		func(d *retainedPartitionID) { d.Start = -1 }, func(d *retainedPartitionID) { d.Start = d.End },
		func(d *retainedPartitionID) { d.End = 1025 },
	} {
		bad := d
		mutate(&bad)
		data, _ := json.Marshal(bad)
		if _, err := decodeRetainedPartition(data); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal(string(data), err)
		}
	}
	for _, bad := range [][]byte{nil, bytes.Repeat([]byte(" "), 4097), append(bytes.Clone(raw), []byte(" {}")...), bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":2,"query":"SELECT 1"`), 1), bytes.Replace(raw, []byte(`"namespace":"tenant_one"`), []byte(`"namespace":"tenant_one","key":"foreign"`), 1)} {
		if _, err := decodeRetainedPartition(bad); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal("invalid descriptor accepted", err)
		}
	}
}
