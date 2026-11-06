// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// OptionStringPartitionSnapshotID reports the last successful live partition
// set's release ID. Close does not revoke descriptors handed to other workers.
const OptionStringPartitionSnapshotID = "adbc.redis.partition_snapshot_id"

type partitionSnapshot struct {
	Namespace string `json:"namespace,omitempty"`
	Root      string `json:"root"`
	Token     string `json:"token"`
	Expires   int64  `json:"expires"`
}

func (p *partitionSnapshot) valid() bool {
	return p != nil && (p.Namespace == "" || len(p.Namespace) == 64 && strings.Trim(p.Namespace, "0123456789abcdef") == "") && snapshotDataID(snapshotRows(p.Root)+"1") == p.Root && snapshotDataID(snapshotRows(p.Token)+"1") == p.Token && p.Expires > 0
}
func (s *store) retainPartitionSnapshot(ctx context.Context, ttl time.Duration) (*partitionSnapshot, error) {
	scope := snapshotOf(ctx)
	if scope == nil || scope.pin == nil {
		return nil, errorf(adbc.StatusInvalidState, "partition production requires a snapshot")
	}
	ref := &partitionSnapshot{Root: scope.id, Token: snapshotID()}
	if s.restrictedReader || scope.pin.reader {
		ref.Namespace = s.readerNamespace
	}
	var expiry int64
	var err error
	if ref.Namespace != "" {
		expiry, err = s.readerControl(ctx, "", "retain_partitions", scope.pin.token, ref.Token, ref.Root, strconv.FormatInt(ttl.Milliseconds(), 10)).Int64()
	} else {
		expiry, err = s.snapshotControl(ctx, "retain_partitions", scope.pin.token, ref.Token, ref.Root, strconv.FormatInt(ttl.Milliseconds(), 10)).Int64()
	}
	if err != nil {
		_ = s.releasePartitionSnapshot(context.Background(), ref.Token)
		return nil, wrapRedis(err, "failed to retain partition snapshot")
	}
	ref.Expires = expiry
	return ref, nil
}
func (s *store) releasePartitionSnapshot(ctx context.Context, id string) error {
	if snapshotDataID(snapshotRows(id)+"1") != id {
		return errorf(adbc.StatusInvalidArgument, "invalid partition snapshot ID")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.snapshotControl(ctx, "release_partitions", id, "", "").Err()
}
func (s *store) acquirePartitionSnapshot(ctx context.Context, ref *partitionSnapshot) (context.Context, *snapshotPin, error) {
	if !ref.valid() {
		return ctx, nil, errorf(adbc.StatusInvalidArgument, "live descriptor lacks a shared snapshot; regenerate partitions")
	}
	p := &snapshotPin{ctx: ctx, store: s, token: snapshotID(), root: ref.Root, stop: make(chan struct{}), done: make(chan struct{})}
	var err error
	if s.restrictedReader || ref.Namespace != "" {
		producer := snapshotCatalogKey
		if ref.Namespace != "" {
			producer = snapshotReaderPrefix + ref.Namespace
		}
		p.reader = true
		_, err = s.readerControl(ctx, producer, "acquire_partition", p.token, ref.Token, ref.Root).Text()
	} else {
		_, err = s.snapshotControl(ctx, "acquire_partition", p.token, ref.Token, ref.Root).Text()
	}
	if err != nil && (s.restrictedReader || p.reader) {
		if _, ok := s.resolveReaderAcquire(ctx, p.token, ref.Root, err); ok {
			err = nil
		}
	}
	if err != nil {
		return ctx, nil, wrapRedis(err, "failed to acquire partition snapshot")
	}
	go p.heartbeat()
	sctx := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: ref.Root, pin: p, store: s})
	if err := s.validateSnapshotRead(sctx); err != nil {
		p.release()
		return ctx, nil, err
	}
	return sctx, p, nil
}
