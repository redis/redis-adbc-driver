// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

const (
	unmanagedPartitionVersion           = 5
	unmanagedPartitionMaxKeys           = 100_000
	unmanagedPartitionMaxBytes    int64 = 64 << 20
	unmanagedPartitionMaxKeyBytes       = 1 << 20
	unmanagedPartitionMaxScans          = 1_000_000
	unmanagedPartitionMaxObserved int64 = 1 << 31
)

// Version 5 describes a disjoint raw-key hash bucket. The shared server pin
// retains catalog/registration metadata and descriptor admission only; application
// HASHes remain live. Every worker authenticates and performs native reads itself.
// Descriptors are opaque query data, not an authorization bypass or signed query.
type unmanagedPartitionID struct {
	Version  int                `json:"version"`
	Snapshot *partitionSnapshot `json:"snapshot"`
	Query    string             `json:"query"`
	Schema   string             `json:"schema"`
	Table    string             `json:"table"`
	Target   string             `json:"target"`
	Zone     string             `json:"zone"`
	Params   []partitionParam   `json:"params,omitempty"`
	Count    int                `json:"count"`
	Ordinal  int                `json:"ordinal"`
	Layouts  resultLayouts      `json:"result_layouts,omitempty"`
}

// State credits are checked before map insertion. SCAN COUNT is a hint, so these
// limits bound retained deduplication/work, not the size of a native SCAN reply.
// Each task scans all prefixes: discovery costs O(partitions * source keys).
type unmanagedPartitionBucket struct {
	count, ordinal  int
	target          string
	scans           int
	observed, bytes int64
}

func (p *unmanagedPartitionBucket) owns(key string) bool {
	digest := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(digest[:8])%uint64(p.count) == uint64(p.ordinal)
}
func (p *unmanagedPartitionBucket) scanPage(count int) error {
	if p.scans >= unmanagedPartitionMaxScans || int64(count) > unmanagedPartitionMaxObserved-p.observed {
		return stateErrorf("53200", "unmanaged partition discovery work limit exceeded")
	}
	p.scans++
	p.observed += int64(count)
	return nil
}
func (p *unmanagedPartitionBucket) retainKey(key string, count int) error {
	size := int64(len(key)) + 64
	if len(key) > unmanagedPartitionMaxKeyBytes || count >= unmanagedPartitionMaxKeys || size > unmanagedPartitionMaxBytes-p.bytes {
		return stateErrorf("53200", "unmanaged partition key/byte limit exceeded; increase partition_count or narrow the source registration")
	}
	p.bytes += size
	return nil
}

func (e *executor) unmanagedPartitionPlan(ctx context.Context, sel *SelectStmt) (*selectPlan, string, error) {
	if e.currentCatalog() != catalogName || sel.From.Catalog != "" && sel.From.Catalog != catalogName {
		return nil, "", namedCatalogExecutionUnsupported()
	}
	schema, name, err := e.resolveCatalogTable(ctx, *sel.From)
	if err != nil {
		return nil, "", err
	}
	if isTempSchema(schema) {
		return nil, "", unmanagedUnsupported("temporary source partitions")
	}
	meta, err := e.store.getTable(ctx, schema, name)
	if err != nil {
		return nil, "", err
	}
	if !meta.Unmanaged || meta.Live || validateUnmanagedDescriptor(meta) != nil {
		return nil, "", errorf(adbc.StatusNotImplemented, "unmanaged partitions require an existing permanent unmanaged registration")
	}
	if len(meta.UnmanagedPrefixes) > 256 {
		return nil, "", errorf(adbc.StatusNotImplemented, "unmanaged partitions support at most 256 source prefixes")
	}
	info, err := e.store.unmanagedIndexInfo(ctx, meta)
	if err != nil {
		return nil, "", err
	}
	target := retainedSourceIdentity(meta, info)
	sel.From.Schema = schema
	plan, err := e.planSelect(ctx, sel, e.paramTypes)
	if err != nil {
		return nil, "", err
	}
	if plan.meta == nil || !plan.meta.Unmanaged || plan.meta.isMem || plan.aggregate || partitionTarget(plan.meta) != partitionTarget(meta) {
		return nil, "", errorf(adbc.StatusInvalidState, "unmanaged partition target changed during planning")
	}
	return plan, target, nil
}

func (s *statementImpl) executeUnmanagedPartitions(ctx context.Context) (*arrow.Schema, adbc.Partitions, int64, error) {
	fail := func(err error) (*arrow.Schema, adbc.Partitions, int64, error) { return nil, adbc.Partitions{}, -1, err }
	if err := s.checkOpen(); err != nil {
		return fail(err)
	}
	if s.ingest.TableName != "" || s.query == "" {
		return fail(errorf(adbc.StatusInvalidState, "unmanaged partitions require a SELECT and no ingest target"))
	}
	if !s.conn.store.snapshots || snapshotOf(ctx) == nil {
		return fail(errorf(adbc.StatusNotImplemented, "unmanaged partitions require the current registration catalog"))
	}
	if s.partitionStrategyName == "balanced" {
		return fail(errorf(adbc.StatusInvalidArgument, "balanced row-ID planning does not apply to unmanaged hash partitions"))
	}
	if len(s.query) > maxPartitionBytes/2 {
		return fail(errorf(adbc.StatusInvalidArgument, "partition query is too large"))
	}
	sel, err := partitionSelect(s.query)
	if err != nil {
		return fail(err)
	}
	encoded, params, types, err := s.partitionParams(ctx)
	if err != nil {
		return fail(err)
	}
	if err := checkPartitionParams(s.query, len(params)); err != nil {
		return fail(err)
	}
	e := s.executor()
	e.params, e.paramTypes = params, types
	plan, target, err := e.unmanagedPartitionPlan(ctx, sel)
	if err != nil {
		return fail(err)
	}
	schema, err := s.resultLayouts.schema(resultSchema(plan.columns()))
	if err != nil {
		return fail(err)
	}
	group, err := e.store.retainPartitionSnapshot(ctx, s.resultTTL())
	if err != nil {
		return fail(err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = e.store.releasePartitionSnapshot(context.Background(), group.Token)
		}
	}()
	n := s.partitions()
	parts := adbc.Partitions{NumPartitions: uint64(n), PartitionIDs: make([][]byte, n)}
	d := unmanagedPartitionID{Version: unmanagedPartitionVersion, Snapshot: group, Query: s.query, Schema: plan.meta.Schema, Table: plan.meta.Name, Target: target, Zone: s.conn.sess.value("timezone").text, Params: encoded, Count: n, Layouts: maps.Clone(s.resultLayouts)}
	for i := range parts.PartitionIDs {
		d.Ordinal = i
		parts.PartitionIDs[i], err = json.Marshal(d)
		if err != nil || len(parts.PartitionIDs[i]) > maxPartitionBytes {
			return fail(errorf(adbc.StatusInvalidArgument, "unmanaged partition descriptor is too large"))
		}
	}
	s.partitionSnapshotID = group.Token
	complete = true
	return schema, parts, -1, nil
}

func decodeUnmanagedPartition(data []byte) (unmanagedPartitionID, error) {
	var d unmanagedPartitionID
	bad := func() (unmanagedPartitionID, error) {
		return d, errorf(adbc.StatusInvalidArgument, "invalid unmanaged partition descriptor")
	}
	if len(data) == 0 || len(data) > maxPartitionBytes {
		return bad()
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return bad()
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return bad()
	}
	if d.Version != unmanagedPartitionVersion || !d.Snapshot.valid() || d.Query == "" || d.Schema == "" || d.Table == "" || d.Zone == "" || len(d.Target) != 64 || d.Count < 1 || d.Count > maxPartitionCount || d.Ordinal < 0 || d.Ordinal >= d.Count {
		return bad()
	}
	if _, err := hex.DecodeString(d.Target); err != nil {
		return bad()
	}
	if err := d.Layouts.partitionCompatible(); err != nil {
		return bad()
	}
	if _, _, err := decodePartitionParams(d.Params); err != nil {
		return bad()
	}
	return d, nil
}

func (c *connectionImpl) readUnmanagedPartition(ctx context.Context, data []byte) (result array.RecordReader, resultErr error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if !c.store.snapshots {
		return nil, errorf(adbc.StatusInvalidState, "unmanaged partitions require the current registration catalog")
	}
	d, err := decodeUnmanagedPartition(data)
	if err != nil {
		return nil, err
	}
	if schemaCatalog(d.Schema) != catalogName {
		return nil, namedCatalogExecutionUnsupported()
	}
	// Reuse the managed catalog lifetime machinery only for the registration.
	// Buffered source rows are ordinary live observations, never snapshot data.
	layoutCtx := ctx
	defer func() {
		if resultErr == nil && result != nil {
			result, resultErr = d.Layouts.wrap(layoutCtx, c.Alloc, result)
		}
	}()
	ctx, stop := context.WithTimeout(ctx, snapshotTimeout)
	ctx, pin, err := c.store.acquirePartitionSnapshot(ctx, d.Snapshot)
	if err != nil {
		stop()
		return nil, err
	}
	defer func() {
		if resultErr == nil && result != nil {
			if reader, ok := result.(*streamReader); ok {
				reader.mu.Lock()
				if !reader.closed {
					reader.snapshot = pin
					reader.snapshotCancel = stop
					reader.snapshotStop = context.AfterFunc(ctx, func() { reader.closeWith(cancelled(ctx)) })
					reader.mu.Unlock()
					return
				}
				reader.mu.Unlock()
			}
		}
		stop()
		pin.release()
	}()
	sel, err := partitionSelect(d.Query)
	if err != nil {
		return nil, err
	}
	if sel.From.Name != d.Table || sel.From.Schema != "" && sel.From.Schema != d.Schema {
		return nil, errorf(adbc.StatusInvalidArgument, "unmanaged partition query target differs from descriptor")
	}
	sel.From.Schema = d.Schema
	zone, err := timeZoneParam(d.Zone)
	if err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid partition time zone")
	}
	params, types, err := decodePartitionParams(d.Params)
	if err != nil {
		return nil, err
	}
	if err := checkPartitionParams(d.Query, len(params)); err != nil {
		return nil, err
	}
	e := c.executor()
	e.params, e.paramTypes = params, types
	e.tz, e.tzKnown = zone.zone, true
	// Foreign registrations are resolved only through this descriptor's pinned
	// catalog; workers need not enable automatic foreign discovery themselves.
	if d.Schema == foreignSchema {
		e.partitionForeignTable = d.Table
	}
	plan, target, err := e.unmanagedPartitionPlan(ctx, sel)
	if err != nil {
		return nil, err
	}
	if target != d.Target {
		return nil, errorf(adbc.StatusInvalidState, "unmanaged partition source definition changed; regenerate partitions")
	}
	if _, err := d.Layouts.schema(resultSchema(plan.columns())); err != nil {
		return nil, err
	}
	need := maps.Clone(plan.extraNeed)
	if need == nil {
		need = map[string]bool{}
	}
	for _, item := range plan.items {
		columnRefs(item.expr, need)
	}
	req := scanRequest{meta: plan.meta, need: need, where: wherePlan{query: "*", original: sel.Where, residual: sel.Where}, unmanagedPartition: &unmanagedPartitionBucket{count: d.Count, ordinal: d.Ordinal, target: d.Target}}
	r, err := e.newScanReader(ctx, plan, req, params, &streamOpts{mem: c.Alloc, batchRows: c.stream.rows(), batchBytes: c.stream.batchBytes})
	if err != nil {
		return nil, err
	}
	r.attachOwners([]*streams{&c.streams})
	return r, nil
}
