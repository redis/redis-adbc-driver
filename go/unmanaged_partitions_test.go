// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUnmanagedPartitionDescriptorValidation(t *testing.T) {
	d := unmanagedPartitionID{Version: 5, Snapshot: &partitionSnapshot{Root: strings.Repeat("a", 32), Token: strings.Repeat("b", 32), Expires: 1}, Query: "SELECT n FROM t", Schema: "public", Table: "t", Target: strings.Repeat("c", 64), Zone: "UTC", Count: 4, Ordinal: 3}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeUnmanagedPartition(data); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*unmanagedPartitionID){
		func(d *unmanagedPartitionID) { d.Version = 3 }, func(d *unmanagedPartitionID) { d.Snapshot = nil },
		func(d *unmanagedPartitionID) { d.Count = 0 }, func(d *unmanagedPartitionID) { d.Count = 1025 },
		func(d *unmanagedPartitionID) { d.Ordinal = -1 }, func(d *unmanagedPartitionID) { d.Ordinal = 4 },
		func(d *unmanagedPartitionID) { d.Target = strings.Repeat("z", 64) },
	} {
		changed := d
		mutate(&changed)
		data, _ := json.Marshal(changed)
		if _, err := decodeUnmanagedPartition(data); err == nil {
			t.Fatalf("invalid descriptor accepted: %#v", changed)
		}
	}
	if _, err := decodeUnmanagedPartition(append(data, []byte("{}")...)); err == nil {
		t.Fatal("trailing data accepted")
	}
}
func TestUnmanagedPartitionHashRawBytesAndBounds(t *testing.T) {
	keys := []string{"plain", string([]byte{'a', 0, 'b'}), string([]byte{'a', 255, 'b'})}
	if !strings.ContainsRune(keys[1], 0) {
		t.Fatal("no zero byte")
	}
	for _, key := range keys {
		owned := 0
		for i := 0; i < 13; i++ {
			if (&unmanagedPartitionBucket{count: 13, ordinal: i}).owns(key) {
				owned++
			}
		}
		if owned != 1 {
			t.Fatal("raw key belongs to multiple/no buckets", owned)
		}
	}
	p := &unmanagedPartitionBucket{count: 1}
	if err := p.retainKey("key", unmanagedPartitionMaxKeys); err == nil {
		t.Fatal("key count overflow admitted")
	}
	p.bytes = unmanagedPartitionMaxBytes - 66
	if err := p.retainKey("key", 0); err == nil {
		t.Fatal("byte overflow admitted")
	}
	if p.bytes != unmanagedPartitionMaxBytes-66 {
		t.Fatal("failed admission consumed credits")
	}
	p.scans = unmanagedPartitionMaxScans
	if err := p.scanPage(0); err == nil {
		t.Fatal("empty scan work overflow admitted")
	}
	p.scans = 0
	p.observed = unmanagedPartitionMaxObserved
	if err := p.scanPage(1); err == nil {
		t.Fatal("observed work overflow admitted")
	}
}
func TestUnmanagedPartitionSourceOption(t *testing.T) {
	s := &statementImpl{}
	if got, err := s.GetOption(context.Background(), OptionStringPartitionSourceMode); err != nil || got != "managed" {
		t.Fatal(got, err)
	}
	for _, mode := range []string{"unmanaged", "managed"} {
		if err := s.SetOption(context.Background(), OptionStringPartitionSourceMode, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetOption(context.Background(), OptionStringPartitionSourceMode, "true"); err == nil {
		t.Fatal("invalid source mode accepted")
	}
}
