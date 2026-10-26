// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestRetainedPartitionNamespaceAndLimits(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "not-dialed.invalid:1"})
	defer client.Close()
	for _, namespace := range []string{"", "a}other{b", "tenant:*", "a:b", strings.Repeat("a", 65), "tenant\x00", "租户"} {
		if _, err := newRetainedStore(client, namespace, defaultRetainedLimits()); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("unsafe namespace accepted: %q: %v", namespace, err)
		}
	}
	for _, mutate := range []func(*retainedLimits){
		func(l *retainedLimits) { l.JobBytes = 0 },
		func(l *retainedLimits) { l.JobBytes = 1<<30 + 1 },
		func(l *retainedLimits) { l.TotalBytes = l.JobBytes - 1 },
		func(l *retainedLimits) { l.TotalBytes = 8<<30 + 1 },
		func(l *retainedLimits) { l.ChunkBytes = 0 },
		func(l *retainedLimits) { l.ChunkBytes = 16<<20 + 1 },
		func(l *retainedLimits) { l.Chunks = 0 },
		func(l *retainedLimits) { l.Chunks = maxRetainedChunks + 1 },
		func(l *retainedLimits) { l.Jobs = 0 },
		func(l *retainedLimits) { l.Jobs = 1025 },
	} {
		limits := defaultRetainedLimits()
		mutate(&limits)
		if _, err := newRetainedStore(client, "safe", limits); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("invalid resource limits accepted: %+v: %v", limits, err)
		}
	}
	s, err := newRetainedStore(client, strings.Repeat("a", 64), defaultRetainedLimits())
	if err != nil {
		t.Fatal(err)
	}
	ref := retainedReference{Namespace: s.namespace, JobID: strings.Repeat("1", 32), Manifest: strings.Repeat("2", 64), Expires: 1}
	if !s.validReference(ref) {
		t.Fatal("canonical reference rejected")
	}
	for _, mutate := range []func(*retainedReference){
		func(r *retainedReference) { r.Namespace = "other" },
		func(r *retainedReference) { r.JobID = "../../other" },
		func(r *retainedReference) { r.Manifest = "invalid" },
		func(r *retainedReference) { r.Expires = 0 },
	} {
		bad := ref
		mutate(&bad)
		if s.validReference(bad) {
			t.Fatalf("invalid reference accepted: %+v", bad)
		}
	}
}
