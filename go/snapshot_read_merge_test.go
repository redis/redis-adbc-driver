// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSnapshotDirectoryMergeIdentityBoundaries(t *testing.T) {
	for _, id := range []string{"00000000000000000001", "00009007199254740993", "09223372036854775807"} {
		t.Run(id, func(t *testing.T) {
			d := snapshotDeltaScan{meta: &tableMeta{KeyPrefix: "logical:"}, sources: []snapshotDeltaSource{
				{segment: snapshotSegment{Prefix: "older:"}, ids: []string{id}, done: true},
				{segment: snapshotSegment{Prefix: "newer:"}, ids: []string{id}, done: true},
			}}
			logical, physical, err := d.nextKeys(context.Background())
			suffix := strings.TrimLeft(id, "0")
			if err != nil || fmt.Sprint(logical) != "[logical:"+suffix+"]" || fmt.Sprint(physical) != "[newer:"+suffix+"]" {
				t.Fatal("canonical identity/newest winner differs", logical, physical, err)
			}
			if _, _, err := d.nextKeys(context.Background()); !errors.Is(err, io.EOF) {
				t.Fatal("duplicate versions not advanced together", err)
			}
		})
	}
	for _, id := range []string{"00000000000000000000", "+0000000000000000001", "-0000000000000000001", "0000000000000000001x", "09223372036854775808", "0000000000000000001", "000000000000000000001"} {
		t.Run("reject_"+id, func(t *testing.T) {
			d := snapshotDeltaScan{meta: &tableMeta{KeyPrefix: "logical:"}, sources: []snapshotDeltaSource{{ids: []string{id}, done: true}}}
			if _, _, err := d.nextKeys(context.Background()); err == nil || errors.Is(err, io.EOF) {
				t.Fatal("malformed identity accepted", err)
			}
		})
	}
}

func TestSnapshotDirectoryMergeCancellationAndBounds(t *testing.T) {
	d := snapshotDeltaScan{meta: &tableMeta{KeyPrefix: "logical:"}, pageRows: 1, through: "00000000000000000002", sources: []snapshotDeltaSource{{done: true, ids: []string{"00000000000000000001", "00000000000000000002", "00000000000000000003"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := d.nextKeys(ctx); err == nil || d.sources[0].at != 0 {
		t.Fatal("cancelled merge advanced")
	}
	for _, want := range []string{"logical:1", "logical:2"} {
		keys, _, err := d.nextKeys(context.Background())
		if err != nil || len(keys) != 1 || keys[0] != want {
			t.Fatal("page bound changed", keys, err)
		}
	}
	if _, _, err := d.nextKeys(context.Background()); !errors.Is(err, io.EOF) || d.sources[0].at != 2 {
		t.Fatal("partition upper bound advanced", err)
	}
}
