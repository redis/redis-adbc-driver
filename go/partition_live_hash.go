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
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

type liveHashPartitionID struct {
	partitionID
	Bucket  int `json:"bucket"`
	Buckets int `json:"buckets"`
}

func livePartitionTarget(m *tableMeta) string {
	raw, _ := json.Marshal([]string{partitionTarget(m), m.LiveVersion})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (e *executor) liveHashPartitionPlan(ctx context.Context, sel *SelectStmt) (*selectPlan, error) {
	if e.currentCatalog() != catalogName || sel.From.Catalog != "" && sel.From.Catalog != catalogName {
		return nil, namedCatalogExecutionUnsupported()
	}
	schema, name, err := e.resolveCatalogTable(ctx, *sel.From)
	if err != nil {
		return nil, err
	}
	if !e.store.liveTables || e.store.flexEnabled() {
		return nil, errorf(adbc.StatusNotImplemented, "live_hash partitions require an enabled live HASH table on standard Redis Software")
	}
	ref, live, err := e.store.liveRefFor(ctx, schema)
	if err != nil {
		return nil, err
	}
	if !live {
		return nil, errorf(adbc.StatusNotImplemented, "live_hash partitions require an enabled live HASH table on standard Redis Software")
	}
	meta, err := e.store.liveTable(ctx, ref, name)
	if err != nil {
		return nil, err
	}
	sel.From.Schema = schema
	plan, err := e.planSelect(ctx, sel, e.paramTypes)
	if err != nil {
		return nil, err
	}
	if plan.meta == nil || !plan.meta.Live || plan.meta.isMem || plan.aggregate || livePartitionTarget(plan.meta) != livePartitionTarget(meta) {
		return nil, errorf(adbc.StatusInvalidState, "live partition source changed during planning")
	}
	return plan, nil
}
func (s *statementImpl) executeLiveHashPartitions(ctx context.Context, sel *SelectStmt, encoded []partitionParam, params []Value, types []ColType) (*arrow.Schema, adbc.Partitions, int64, error) {
	fail := func(err error) (*arrow.Schema, adbc.Partitions, int64, error) { return nil, adbc.Partitions{}, -1, err }
	if s.partitionStrategy() == "balanced" {
		return fail(errorf(adbc.StatusNotImplemented, "live_hash partitions use stable key-hash buckets, not numeric balancing"))
	}
	e := s.executor()
	e.params, e.paramTypes = params, types
	plan, err := e.liveHashPartitionPlan(ctx, sel)
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
	d := liveHashPartitionID{partitionID: partitionID{Version: 6, Query: s.query, Schema: plan.meta.Schema, Table: plan.meta.Name, Target: livePartitionTarget(plan.meta), Zone: s.conn.sess.value("timezone").text, Params: encoded, Snapshot: group}, Buckets: n}
	if len(s.resultLayouts) > 0 {
		d.Layouts = maps.Clone(s.resultLayouts)
	}
	for i := range parts.PartitionIDs {
		d.Bucket = i
		parts.PartitionIDs[i], err = json.Marshal(d)
		if err != nil || len(parts.PartitionIDs[i]) > maxPartitionBytes {
			return fail(errorf(adbc.StatusInvalidArgument, "live partition descriptor exceeds limit"))
		}
	}
	if err := e.store.checkLiveIdentity(ctx, plan.meta); err != nil {
		return fail(err)
	}
	if err := e.store.validateLiveIndex(ctx, plan.meta); err != nil {
		return fail(err)
	}
	s.partitionSnapshotID = group.Token
	complete = true
	return schema, parts, -1, nil
}
func decodeLiveHashPartition(data []byte) (liveHashPartitionID, error) {
	var d liveHashPartitionID
	bad := func() (liveHashPartitionID, error) {
		return d, errorf(adbc.StatusInvalidArgument, "invalid live HASH partition descriptor")
	}
	if len(data) == 0 || len(data) > maxPartitionBytes {
		return bad()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF {
		return bad()
	}
	if d.Version != 6 || d.Schema != liveSchema && validLiveSchemaName(d.Schema) != nil || d.Query == "" || d.Table == "" || len(d.Target) != 64 || strings.Trim(d.Target, "0123456789abcdef") != "" || d.Zone == "" || d.Buckets < 1 || d.Buckets > maxPartitionCount || d.Bucket < 0 || d.Bucket >= d.Buckets || d.Lo != 0 || d.Hi != 0 || d.ScanCap != 0 || !d.Snapshot.valid() {
		return bad()
	}
	if d.Layouts.partitionCompatible() != nil {
		return bad()
	}
	if _, _, err := decodePartitionParams(d.Params); err != nil {
		return bad()
	}
	return d, nil
}
func (c *connectionImpl) readLiveHashPartition(ctx context.Context, data []byte) (array.RecordReader, error) {
	d, err := decodeLiveHashPartition(data)
	if err != nil {
		return nil, err
	}
	if !c.store.liveTables || c.store.flexEnabled() {
		return nil, errorf(adbc.StatusNotImplemented, "live HASH partition readers require live_tables on standard Redis Software")
	}
	ctx, stop := context.WithTimeout(ctx, snapshotTimeout)
	ctx, pin, err := c.store.acquirePartitionSnapshot(ctx, d.Snapshot)
	if err != nil {
		stop()
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			stop()
			pin.release()
		}
	}()
	sel, err := partitionSelect(d.Query)
	if err != nil {
		return nil, err
	}
	if sel.From.Name != d.Table || sel.From.Schema != "" && sel.From.Schema != d.Schema {
		return nil, errorf(adbc.StatusInvalidArgument, "live partition query target does not match descriptor")
	}
	sel.From.Schema = d.Schema
	params, types, err := decodePartitionParams(d.Params)
	if err != nil {
		return nil, err
	}
	if err := checkPartitionParams(d.Query, len(params)); err != nil {
		return nil, err
	}
	zone, err := timeZoneParam(d.Zone)
	if err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid live partition time zone")
	}
	e := c.executor()
	e.params, e.paramTypes = params, types
	e.tz, e.tzKnown = zone.zone, true
	plan, err := e.liveHashPartitionPlan(ctx, sel)
	if err != nil {
		return nil, err
	}
	if livePartitionTarget(plan.meta) != d.Target {
		return nil, errorf(adbc.StatusInvalidState, "live partition registration changed or belongs to another database")
	}
	if _, err := d.Layouts.schema(resultSchema(plan.columns())); err != nil {
		return nil, err
	}
	need := map[string]bool{}
	for _, item := range plan.items {
		columnRefs(item.expr, need)
	}
	scan := &livePartitionScan{store: e.store, meta: plan.meta, bucket: d.Bucket, buckets: d.Buckets, seen: map[string]bool{}}
	req := scanRequest{meta: plan.meta, need: need, where: wherePlan{original: sel.Where, residual: sel.Where}, livePartition: scan}
	reader, err := e.newScanReader(ctx, plan, req, params, &streamOpts{mem: c.Alloc, batchRows: c.stream.rows(), batchBytes: c.stream.batchBytes})
	if err != nil {
		return nil, err
	}
	reader.attachOwners([]*streams{&c.streams})
	reader.mu.Lock()
	if reader.closed {
		reader.mu.Unlock()
		reader.Release()
		return nil, errConnClosed
	}
	reader.snapshot = pin
	reader.snapshotCancel = stop
	reader.sourceCheck = scan.validate
	reader.snapshotStop = context.AfterFunc(ctx, func() { reader.closeWith(cancelled(ctx)) })
	reader.mu.Unlock()
	owned = true
	wrapped, err := d.Layouts.wrap(ctx, c.Alloc, reader)
	if err != nil {
		return nil, err
	}
	return wrapped, nil
}

// Every worker repeats bounded discovery. No source inventory, persisted SCAN
// cursor or payload needs reclamation. Retries observe current source values.
const livePartitionMaxKeys = 100000
const livePartitionMaxKeyBytes = 64 << 20
const livePartitionMaxWork = 1000000
const livePartitionMaxScanPages = 4096

type livePartitionScan struct {
	store              *store
	meta               *tableMeta
	bucket, buckets    int
	scan               *keyScan
	pending            []string
	seen               map[string]bool
	bytes, work, pages int
	done, closed       bool
}

func (s *livePartitionScan) close() { s.closed = true; s.scan = nil; s.pending = nil; s.seen = nil }
func (s *livePartitionScan) validate(ctx context.Context) error {
	if err := s.store.checkLiveIdentity(ctx, s.meta); err != nil {
		return err
	}
	return s.store.validateLiveIndex(ctx, s.meta)
}
func livePartitionBucket(key string, buckets int) int {
	sum := sha256.Sum256([]byte(key))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(buckets))
}
func (s *livePartitionScan) next(ctx context.Context) (keys []string, rows []aggRow, err error) {
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if s.closed {
		return nil, nil, io.EOF
	}
	if err := cancelled(ctx); err != nil {
		return nil, nil, err
	}
	if err := s.validate(ctx); err != nil {
		return nil, nil, err
	}
	for len(s.pending) == 0 {
		if s.done {
			return nil, nil, io.EOF
		}
		if s.scan == nil {
			s.scan, err = (&hashConn{client: s.store.redis(ctx)}).scanKeys(ctx, s.meta.prefix(), "hash")
			if err != nil {
				return nil, nil, err
			}
			s.scan.match = retainedByteGlob(s.meta.prefix())
		}
		if s.pages >= livePartitionMaxScanPages {
			return nil, nil, stateErrorf("53200", "live partition SCAN page budget exhausted")
		}
		page, done, err := s.scan.next(ctx)
		if err != nil {
			return nil, nil, err
		}
		s.pages++
		s.done = done
		for _, key := range page {
			s.work++
			if s.work > livePartitionMaxWork {
				return nil, nil, stateErrorf("53200", "live partition discovery work budget exhausted")
			}
			if !strings.HasPrefix(key, s.meta.prefix()) {
				return nil, nil, errorf(adbc.StatusInvalidData, "live partition SCAN returned an out-of-scope key")
			}
			if err := cancelled(ctx); err != nil {
				return nil, nil, err
			}
			if !isRowKey(s.meta.prefix(), key) {
				continue
			}
			if len(key) > 1<<20 {
				return nil, nil, stateErrorf("53200", "live partition key exceeds 1 MiB")
			}
			if livePartitionBucket(key, s.buckets) != s.bucket || s.seen[key] {
				continue
			}
			if len(s.seen) >= livePartitionMaxKeys || s.bytes+len(key)+64 > livePartitionMaxKeyBytes {
				return nil, nil, stateErrorf("53200", "live partition key inventory budget exhausted")
			}
			s.seen[key] = true
			s.bytes += len(key) + 64
			s.pending = append(s.pending, key)
		}
	}
	end, requestBytes := 0, 0
	for end < len(s.pending) && end < 128 {
		size := len(s.pending[end]) + 128
		if end > 0 && requestBytes+size > 1<<20 {
			break
		}
		requestBytes += size
		end++
	}
	keys = append([]string(nil), s.pending[:end]...)
	s.pending = s.pending[end:]
	rows, err = (&unmanagedProjectionRead{store: s.store}).native(ctx, keys)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validate(ctx); err != nil {
		return nil, nil, err
	}
	return keys, rows, nil
}
