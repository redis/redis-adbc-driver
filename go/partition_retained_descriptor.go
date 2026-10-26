// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Version 2 is reserved for retained results. No source SQL, source identities,
// arbitrary Redis keys, endpoint or credentials travel to independent readers.
// The chunk range preserves ordinal order within each descriptor.
type retainedPartitionID struct {
	Version   int               `json:"version"`
	Reference retainedReference `json:"result"`
	Start     int               `json:"start_chunk"`
	End       int               `json:"end_chunk"`
}

func decodeRetainedPartition(data []byte) (retainedPartitionID, error) {
	var d retainedPartitionID
	bad := func() (retainedPartitionID, error) {
		return retainedPartitionID{}, errorf(adbc.StatusInvalidArgument, "invalid retained partition descriptor")
	}
	if len(data) == 0 || len(data) > 4096 {
		return bad()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF || d.Version != 2 ||
		!retainedNamespace.MatchString(d.Reference.Namespace) || !retainedIdentity.MatchString(d.Reference.JobID) ||
		!retainedDigest.MatchString(d.Reference.Manifest) || d.Reference.Expires <= 0 ||
		d.Start < 0 || d.Start >= d.End || d.End > maxRetainedChunks {
		return bad()
	}
	return d, nil
}

// An independent reader loads the persisted namespace policy using read-only,
// bounded commands. It never initializes a namespace, reserves bytes or runs
// source catalog recovery. Hard limits apply even to a corrupted configuration.
func retainedReaderStore(ctx context.Context, client goredis.UniversalClient, namespace string) (*retainedStore, error) {
	if !retainedNamespace.MatchString(namespace) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained partition namespace")
	}
	raw, err := client.GetRange(ctx, "adbc:{retained:"+namespace+"}:config", 0, 4096).Bytes()
	if err != nil {
		return nil, wrapRedis(err, "retained partition namespace configuration unavailable")
	}
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, errorf(adbc.StatusInvalidState, "retained partition namespace configuration missing or excessive")
	}
	var limits retainedLimits
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&limits) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained partition namespace configuration")
	}
	store, err := newRetainedStore(client, namespace, limits)
	if err != nil {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained partition namespace configuration")
	}
	return store, nil
}
