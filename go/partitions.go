// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// OptionIntPartitionCount is a statement option: the maximum number of deferred
// row-ID ranges returned by ExecutePartitions. It does not start workers.
const OptionIntPartitionCount = "adbc.redis.partition_count"

// OptionIntPartitionScanCap declares a guaranteed lower bound on the server
// aggregate cap for the lifetime of partition reads (0: discover via CONFIG).
const OptionIntPartitionScanCap = "adbc.redis.partition_scan_cap"

const (
	maxPartitionCount = 1024
	maxPartitionBytes = 1 << 20
)

// partitionID describes a deferred read, not a retained result or snapshot.
// Query literals may be sensitive: callers must treat these bytes as query data.
// The table fingerprint identifies the logical target without embedding an
// endpoint or credentials, allowing different cluster seeds/proxy addresses.
type partitionID struct {
	Version int              `json:"version"`
	Query   string           `json:"query"`
	Schema  string           `json:"schema"`
	Table   string           `json:"table"`
	Target  string           `json:"target"`
	Zone    string           `json:"zone"`
	Params  []partitionParam `json:"params,omitempty"`
	ScanCap int64            `json:"scan_cap,omitempty"`
	Lo      int64            `json:"lo"`
	Hi      int64            `json:"hi"`
}

func (s *statementImpl) partitions() int {
	if s.partitionCount == 0 {
		return 8
	}
	return s.partitionCount
}

// partitionSelect deliberately accepts a small, row-local SQL subset. Check
// before planning: subqueries, views and CTEs can otherwise materialize data.
func partitionSelect(query string) (*SelectStmt, error) {
	parsed, err := ParseScript(query)
	if err != nil {
		return nil, invalidArg(err)
	}
	if len(parsed) != 1 {
		return nil, errorf(adbc.StatusNotImplemented, "partitioned reads require one SELECT")
	}
	sel, ok := parsed[0].Stmt.(*SelectStmt)
	if !ok || sel.From == nil || sel.FromSelect != nil || sel.FromFunc != nil || sel.FromLateral ||
		len(sel.FromColumns) > 0 || len(sel.Joins) > 0 || len(sel.With) > 0 || sel.SetOp != nil ||
		len(sel.OrderBy) > 0 || sel.Limit != nil || sel.Offset != nil || len(sel.GroupBy) > 0 ||
		sel.GroupingSets != nil || sel.Having != nil || sel.Distinct || len(sel.DistinctOn) > 0 ||
		len(sel.Windows) > 0 || sel.Qualify != nil {
		return nil, errorf(adbc.StatusNotImplemented, "partitioned reads require a single-table SELECT without global operations")
	}
	safe := func(expr Expr) bool {
		ok := true
		walkExpr(expr, func(x Expr) {
			switch x.(type) {
			case *Literal, *ColumnRef, *Param, *Unary, *Binary, *IsNull, *Case:
			case *Cast:
				if x.(*Cast).T.Kind == KindRecord {
					ok = false
				}
			case *Func:
				f := x.(*Func)
				if !partitionFunctions[f.Name] || f.Star || f.Distinct || len(f.OrderBy) > 0 || f.Filter != nil || f.WithinGroup || f.Nulls != 0 || f.JSON != nil {
					ok = false
				}
			default:
				ok = false
			}
		})
		return ok
	}
	if !safe(sel.Where) {
		return nil, errorf(adbc.StatusNotImplemented, "partition predicate contains an unsupported expression")
	}
	for _, it := range sel.Items {
		if !it.Star {
			if !safe(it.Expr) {
				return nil, errorf(adbc.StatusNotImplemented, "partition projection contains an unsupported expression")
			}
		}
	}
	return sel, nil
}

func partitionTarget(meta *tableMeta) string {
	// Exclude caches/ancillary metadata such as tag-value proofs, comments and
	// constraints. Identity, physical columns and rewrite generation must match.
	cols := append([]columnMeta(nil), meta.Columns...)
	for i := range cols {
		cols[i].TagValues, cols[i].TagsChecked, cols[i].NaNs = "", false, false
		cols[i].Comment, cols[i].Default, cols[i].DefaultOID = "", "", 0
	}
	data, _ := json.Marshal(struct {
		Prefix, Index string
		Generation    int64
		Columns       []columnMeta
	}{meta.prefix(), meta.index(), meta.PrefixGen, cols})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (e *executor) partitionPlan(ctx context.Context, sel *SelectStmt) (*selectPlan, error) {
	schema, name, err := e.resolveTable(*sel.From)
	if err != nil {
		return nil, err
	}
	if isTempSchema(schema) || schema == foreignSchema {
		return nil, errorf(adbc.StatusNotImplemented, "partitioned reads require a permanent driver-managed table")
	}
	meta, err := e.store.getTable(ctx, schema, name)
	if err != nil {
		return nil, err
	}
	if !managedRowIDTable(meta) {
		return nil, errorf(adbc.StatusNotImplemented, "partitioned reads require a permanent managed table with stable row IDs")
	}
	// Pin resolution, including when a worker has a temporary table shadowing
	// the original name. Plan only after refusing views and virtual relations.
	sel.From.Schema = schema
	plan, err := e.planSelect(ctx, sel, e.paramTypes)
	if err != nil {
		return nil, err
	}
	if plan.meta == nil || plan.meta.isMem || plan.aggregate || plan.meta.RekeyTo != "" || partitionTarget(plan.meta) != partitionTarget(meta) {
		return nil, errorf(adbc.StatusInvalidState, "partition table changed during planning")
	}
	return plan, nil
}

func (s *statementImpl) executePartitions(ctx context.Context) (*arrow.Schema, adbc.Partitions, int64, error) {
	fail := func(err error) (*arrow.Schema, adbc.Partitions, int64, error) {
		return nil, adbc.Partitions{}, -1, err
	}
	if err := s.checkOpen(); err != nil {
		return fail(err)
	}
	if s.conn.store == nil || s.conn.store.isClosed() {
		return fail(errConnClosed)
	}
	if s.ingest.TableName != "" {
		return fail(errorf(adbc.StatusNotImplemented, "partitioned reads do not support ingestion"))
	}
	if s.query == "" {
		return fail(errorf(adbc.StatusInvalidState, "no query has been set"))
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
	plan, err := e.partitionPlan(ctx, sel)
	if err != nil {
		return fail(err)
	}
	last, err := e.store.client.Get(ctx, seqKey(plan.meta.Schema, plan.meta.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		last, err = 0, nil // a new table has no allocated IDs
	}
	if err != nil {
		return fail(wrapRedis(err, "reading partition row sequence failed"))
	}
	if last < 0 || last >= exactDoubleLimit {
		return fail(errorf(adbc.StatusNotImplemented, "partition row IDs must be below 2^53"))
	}
	n := int(min(int64(s.partitions()), max(last, 1)))
	lo, hi := int64(1), last
	constrained, known := partitionPredicateSpans(sel.Where, lo, hi, params)
	var spans []partitionSpan
	if known {
		if ids, finite := partitionFiniteIDs(constrained); finite {
			spans = partitionPointSpans(ids, n)
		} else {
			lo, hi = constrained[0].lo, constrained[len(constrained)-1].hi
		}
	}
	if spans == nil {
		spans = equalPartitionSpans(lo, hi, n)
		if s.partitionStrategy() == "balanced" {
			spans, err = e.balancedPartitionSpans(ctx, plan.meta, lo, hi, n, s.partitionScanCap)
			if err != nil {
				return fail(err)
			}
		}
	}
	parts := adbc.Partitions{NumPartitions: uint64(len(spans)), PartitionIDs: make([][]byte, len(spans))}
	d := partitionID{Version: 1, Query: s.query, Schema: plan.meta.Schema, Table: plan.meta.Name,
		ScanCap: s.partitionScanCap, Params: encoded, Target: partitionTarget(plan.meta), Zone: s.conn.sess.value("timezone").text}
	for i, span := range spans {
		d.Lo, d.Hi = span.lo, span.hi
		parts.PartitionIDs[i], err = json.Marshal(d)
		if err != nil || len(parts.PartitionIDs[i]) > maxPartitionBytes {
			return fail(errorf(adbc.StatusInvalidArgument, "partition descriptor is too large"))
		}
	}
	return resultSchema(plan.columns()), parts, -1, nil
}

func decodePartition(data []byte) (partitionID, error) {
	var d partitionID
	bad := func() (partitionID, error) {
		return d, errorf(adbc.StatusInvalidArgument, "invalid Redis ADBC partition descriptor")
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
	if d.ScanCap < 0 || d.ScanCap > 1<<31 || d.Version != 1 || d.Query == "" || d.Schema == "" || d.Table == "" || len(d.Target) != 64 || d.Zone == "" ||
		d.Lo < 1 || d.Hi >= exactDoubleLimit || d.Hi < d.Lo && (d.Lo != 1 || d.Hi != 0) {
		return bad()
	}
	if _, _, err := decodePartitionParams(d.Params); err != nil {
		return bad()
	}
	return d, nil
}

func (c *connectionImpl) ReadPartition(ctx context.Context, data []byte) (array.RecordReader, error) {
	if c.store == nil || c.store.isClosed() {
		return nil, errConnClosed
	}
	if len(data) > 0 && len(data) <= maxPartitionBytes {
		var version struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(data, &version) == nil && version.Version == 2 {
			descriptor, err := decodeRetainedPartition(data)
			if err != nil {
				return nil, err
			}
			if c.retainedNamespace == "" || descriptor.Reference.Namespace != c.retainedNamespace {
				return nil, errorf(adbc.StatusInvalidArgument, "retained result namespace does not match the connection")
			}
			store, err := c.retainedReadStore(ctx)
			if err != nil {
				return nil, err
			}
			reader, err := newRetainedReader(ctx, store, descriptor.Reference, descriptor.Start, descriptor.End, c.stream.rows(), &c.streams)
			if err != nil {
				return nil, err
			}
			reader.batchBytes = c.stream.batchBytes
			// Close marks the store closed before taking its stream snapshot.
			// Cover closure while the first chunk was loading, before registration.
			if c.store.isClosed() {
				reader.closeWith(errConnClosed)
				reader.Release()
				return nil, errConnClosed
			}
			return reader, nil
		}
	}
	if c.readerOnly {
		return nil, errorf(adbc.StatusInvalidArgument, "reader-only connections require a retained result descriptor")
	}
	d, err := decodePartition(data)
	if err != nil {
		return nil, err
	}
	sel, err := partitionSelect(d.Query)
	if err != nil {
		return nil, err
	}
	if sel.From.Name != d.Table || sel.From.Schema != "" && sel.From.Schema != d.Schema {
		return nil, errorf(adbc.StatusInvalidArgument, "partition query target does not match descriptor")
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
	plan, err := e.partitionPlan(ctx, sel)
	if err != nil {
		return nil, err
	}
	if partitionTarget(plan.meta) != d.Target {
		return nil, errorf(adbc.StatusInvalidState, "partition target changed or belongs to another database")
	}
	// Proven small integer ID sets read authoritative HASHes directly. No
	// Search cursor/cap/readiness requirement exists on this bounded path.
	if candidates, known := partitionPredicateSpans(sel.Where, d.Lo, d.Hi, params); known {
		if ids, finite := partitionFiniteIDs(candidates); finite {
			if len(ids) == 0 {
				return recordsReader(c.Alloc, plan.columns(), nil)
			}
			need := maps.Clone(plan.extraNeed)
			if need == nil {
				need = map[string]bool{}
			}
			for _, item := range plan.items {
				columnRefs(item.expr, need)
			}
			req := scanRequest{meta: plan.meta, need: need, where: wherePlan{keys: partitionKeys(plan.meta, ids), residual: sel.Where}}
			r, err := e.newScanReader(ctx, plan, req, params, &streamOpts{mem: c.Alloc, batchRows: c.stream.rows(), batchBytes: c.stream.batchBytes})
			if err != nil {
				return nil, err
			}
			r.attachOwners([]*streams{&c.streams})
			return r, nil
		}
	}
	if len(plan.meta.PendingCleanup) > 0 {
		return nil, errorf(adbc.StatusInvalidState, "partition table has pending dropped-column cleanup; retry after cleanup completes")
	}
	cap, err := e.store.partitionScanCap(ctx, plan.meta.index(), d.ScanCap)
	if err != nil {
		return nil, err
	}
	sc, err := e.prepareScan(ctx, plan, params)
	if err != nil {
		return nil, err
	}
	// Even a direct-key predicate is evaluated within each numeric range.
	// Recheck the full predicate, including any fields added by prepareScan's
	// lowered-cap fallback, rather than trusting an unbounded key materialization.
	sc.req.hashScan = false
	if sc.req.where.keys != nil {
		sc.req.where.query = "*"
	}
	sc.req.where.keys = nil
	sc.req.where.residual = sel.Where
	if d.Hi == 0 || sc.req.where.none {
		return recordsReader(c.Alloc, plan.columns(), nil)
	}
	sc.req.ranges = &rowIDRangeScan{lo: d.Lo, hi: d.Hi, cap: cap, configured: d.ScanCap}
	// Bound the number of Search requests as well as client memory. Sparse IDs
	// with a low cap can otherwise produce billions of empty subranges.
	if (d.Hi-d.Lo)/cap+1 > maxPartitionSubranges {
		return nil, errorf(adbc.StatusNotImplemented, "partition requires too many cap-bounded ranges; increase partition_count or the guaranteed scan cap")
	}
	// No ordering is promised. Avoid a server-side global sort; each task
	// holds only cursor pages and the requested Arrow batch.
	sc.req.sortBy, sc.req.ties, sc.req.limit, sc.req.late = nil, nil, nil, nil
	r, err := e.newScanReader(ctx, plan, sc.req, params, &streamOpts{mem: c.Alloc, batchRows: c.stream.rows(), batchBytes: c.stream.batchBytes})
	if err != nil {
		return nil, err
	}
	r.attachOwners([]*streams{&c.streams})
	return r, nil
}
