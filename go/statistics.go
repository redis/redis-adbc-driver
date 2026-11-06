// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

var _ adbc.ConnectionGetStatistics = (*connectionImpl)(nil)

func (c *connectionImpl) statisticsAccess(ctx context.Context) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if c.store == nil || c.store.isClosed() {
		return errConnClosed
	}
	if c.readerOnly {
		return errorf(adbc.StatusInvalidState, "reader-only connections cannot discover source statistics")
	}
	return nil
}

// GetStatisticNames enumerates custom keys only. The standard row-count key
// does not belong here; an empty stream still carries the canonical schema.
func (c *connectionImpl) GetStatisticNames(ctx context.Context) (array.RecordReader, error) {
	if err := c.statisticsAccess(ctx); err != nil {
		return nil, err
	}
	return array.NewRecordReader(adbc.GetStatisticNamesSchema, nil)
}

// GetStatistics exposes pinned row counts and authoritative column NULL counts
// for permanent managed tables. Other quantities/layouts are omitted.
func (c *connectionImpl) GetStatistics(ctx context.Context, catalog, dbSchema, tableName *string, approximate bool) (array.RecordReader, error) {
	if err := c.statisticsAccess(ctx); err != nil {
		return nil, err
	}
	ctx, transport := c.executionContext(ctx, &c.streams)
	if transport != nil {
		defer transport.finish()
	}
	ctx, pin, err := c.store.acquireSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer pin.release()
	b := array.NewRecordBuilder(c.Alloc, adbc.GetStatisticsSchema)
	defer b.Release()
	if matchPattern(catalogName, catalog) {
		schemas, err := c.GetDBSchemasForCatalog(ctx, catalogName, dbSchema)
		if err != nil {
			return nil, err
		}
		b.Field(0).(*array.StringBuilder).Append(catalogName)
		catalogSchemas := b.Field(1).(*array.ListBuilder)
		catalogSchemas.Append(true)
		schema := catalogSchemas.ValueBuilder().(*array.StructBuilder)
		for _, name := range schemas {
			if isTempAlias(name) || isTempSchema(name) {
				continue
			}
			tables, err := c.store.listTables(ctx, name)
			if err != nil {
				return nil, err
			}
			schema.Append(true)
			schema.FieldBuilder(0).(*array.StringBuilder).Append(name)
			statsList := schema.FieldBuilder(1).(*array.ListBuilder)
			statsList.Append(true)
			stats := statsList.ValueBuilder().(*array.StructBuilder)
			for _, table := range tables {
				if !matchPattern(table, tableName) {
					continue
				}
				meta, err := c.store.getTable(ctx, name, table)
				if err != nil {
					return nil, err
				}
				if meta.Adopted || meta.AppIndex || meta.Foreign || meta.RekeyTo != "" || len(meta.PendingCleanup) > 0 {
					continue
				}
				var n int64
				var nulls []int64
				if snapshotOf(ctx) != nil && snapshotDataID(meta.prefix()) != "" {
					if approximate {
						n, err = c.snapshotStatisticRowCount(ctx, meta, true)
					} else {
						n, nulls, err = c.snapshotColumnNullCounts(ctx, meta)
					}
				} else if approximate {
					if err := c.store.checkRangeReadiness(ctx, meta.index()); err != nil {
						return nil, err
					}
					n, err = c.store.countMatches(ctx, meta.index(), "*")
				} else {
					n, nulls, err = c.columnNullCounts(ctx, meta)
				}
				if err != nil {
					return nil, err
				}
				stats.Append(true)
				stats.FieldBuilder(0).(*array.StringBuilder).Append(table)
				stats.FieldBuilder(1).(*array.StringBuilder).AppendNull()
				stats.FieldBuilder(2).(*array.Int16Builder).Append(adbc.StatisticRowCountKey)
				value := stats.FieldBuilder(3).(*array.DenseUnionBuilder)
				value.Append(0)
				value.Child(0).(*array.Int64Builder).Append(n)
				stats.FieldBuilder(4).(*array.BooleanBuilder).Append(true)
				for col, count := range nulls {
					stats.Append(true)
					stats.FieldBuilder(0).(*array.StringBuilder).Append(table)
					stats.FieldBuilder(1).(*array.StringBuilder).Append(meta.Columns[col].Name)
					stats.FieldBuilder(2).(*array.Int16Builder).Append(adbc.StatisticNullCountKey)
					value.Append(0)
					value.Child(0).(*array.Int64Builder).Append(count)
					stats.FieldBuilder(4).(*array.BooleanBuilder).Append(true)
				}

			}
		}
	}
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if scope := snapshotOf(ctx); scope != nil {
		if err := scope.pin.valid(ctx); err != nil {
			return nil, err
		}
	}
	record := b.NewRecordBatch()
	defer record.Release()
	return array.NewRecordReader(adbc.GetStatisticsSchema, []arrow.RecordBatch{record})
}

// Prefer proven immutable visibility masks for approximate-request counts.
// Inexact IDs or excess mask work fall back to bounded authoritative merging,
// never physical document totals that include shadowed versions/tombstones.
func (c *connectionImpl) snapshotStatisticRowCount(ctx context.Context, meta *tableMeta, approximate bool) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if approximate {
		// A single immutable baseline uses the same proof as a segmented table;
		// its normal scan flag need not force statistics into a row-by-row scan.
		indexed := *meta
		indexed.SnapshotOverlay = true
		queries, ok, err := (&executor{store: c.store}).snapshotIndexQueries(ctx, &indexed)
		if err != nil {
			return 0, err
		}
		if ok {
			var count int64
			for _, query := range queries {
				if err := c.store.checkRangeReadiness(ctx, query.segment.Index); err != nil {
					return 0, err
				}
				n, err := c.store.countMatches(ctx, query.segment.Index, query.query)
				if err != nil {
					return 0, err
				}
				if n < 0 || n > query.segment.Rows || count > math.MaxInt64-n {
					return 0, errorf(adbc.StatusIO, "invalid snapshot statistic row count")
				}
				count += n
			}
			return count, nil
		}
	}
	scan := newSnapshotDeltaScan(c.store, meta, []string{rowIDField})
	var count int64
	for {
		_, rows, err := scan.next(ctx)
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		// Include tombstones and shadowed directory entries in the work budget.
		var work int64
		for _, source := range scan.sources {
			consumed := source.offset - int64(len(source.ids)) + int64(source.at)
			if consumed < 0 || consumed > 2_000_000-work {
				return 0, codeStateErrorf(adbc.StatusInvalidState, "53200", "exact statistics scan budget exhausted")
			}
			work += consumed
		}
		count += int64(len(rows))
	}
}

// Exact means a complete, deduplicated scan of authoritative row identities,
// not a transaction snapshot. Concurrent DML can change observed membership.
// Bound the deduplication memory and scan work rather than truncate a count.
func (c *connectionImpl) columnNullCounts(ctx context.Context, meta *tableMeta) (int64, []int64, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	scan, err := (&hashConn{client: c.store.redis(ctx)}).scanKeys(ctx, meta.prefix(), "hash")
	if err != nil {
		return 0, nil, err
	}
	scan.match = retainedByteGlob(meta.prefix())
	if len(meta.Columns) > 4096 {
		return 0, nil, errorf(adbc.StatusNotImplemented, "column statistics exceed 4096 columns")
	}
	nulls := make([]int64, len(meta.Columns))
	seen := map[string]bool{}
	budget := newRetainedBudget(64 << 20)
	var count int64
	work := 0
	for {
		keys, done, err := scan.next(ctx)
		if err != nil {
			return 0, nil, err
		}
		for _, key := range keys {
			if err := cancelled(ctx); err != nil {
				return 0, nil, err
			}
			work += 1 + 2*len(meta.Columns)
			if work > 2_000_000 {
				return 0, nil, codeStateErrorf(adbc.StatusInvalidState, "53200", "exact statistics scan budget exhausted")
			}
			if seen[key] {
				continue
			}
			if err := budget.reserve(int64(len(key))*2 + 128); err != nil {
				return 0, nil, err
			}
			seen[key] = true
			id, err := c.store.redis(ctx).HGet(ctx, key, rowIDField).Result()
			if isWrongType(err) || err == goredis.Nil {
				continue
			}
			if err != nil {
				return 0, nil, wrapRedis(err, "exact statistics row identity read failed")
			}
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n < 1 || key != meta.prefix()+strconv.FormatInt(n, 10) {
				return 0, nil, errorf(adbc.StatusInvalidData, "exact statistics found an inconsistent physical row identity")
			}
			if err := c.statisticNullFields(ctx, meta, key, id, nulls); err != nil {
				return 0, nil, err
			}
			count++
		}
		if done {
			break
		}
	}
	current, err := c.store.getTable(ctx, meta.Schema, meta.Name)
	if err != nil {
		return 0, nil, err
	}
	if partitionTarget(current) != partitionTarget(meta) {
		return 0, nil, errorf(adbc.StatusInvalidState, "table identity changed during exact statistics scan")
	}
	return count, nulls, nil
}

func (c *connectionImpl) statisticNullFields(ctx context.Context, meta *tableMeta, key, id string, nulls []int64) error {
	// HEXISTS bounds response memory independently of source payloads.
	// Missing fields follow ADD COLUMN's stored default semantics.
	for first := 0; first < len(meta.Columns); first += 128 {
		end := min(first+128, len(meta.Columns))
		pipe := c.store.redis(ctx).Pipeline()
		commands := make([]*goredis.BoolCmd, end-first)
		markers := make([]*goredis.BoolCmd, end-first)
		for col := first; col < end; col++ {
			commands[col-first] = pipe.HExists(ctx, key, meta.Columns[col].field())
			markers[col-first] = pipe.HExists(ctx, key, nullMarker(meta.Columns[col].field()))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "column statistics field existence read failed")
		}
		for col := first; col < end; col++ {
			if commands[col-first].Val() {
				continue
			}
			row := aggRow{rowIDField: id}
			if markers[col-first].Val() {
				row[nullMarker(meta.Columns[col].field())] = "1"
			}
			v, err := meta.Columns[col].missingValue(row)
			if err != nil {
				return errorf(adbc.StatusInvalidData, "invalid column statistics missing value")
			}
			if v.Null {
				nulls[col]++
			}
		}
	}
	return nil
}

// Resolve exact directory visibility before examining physical field presence.
// Only bounded identity/deletion headers and HEXISTS replies cross the wire;
// arbitrary column payloads do not enter the statistics allocation budget.
func (c *connectionImpl) snapshotColumnNullCounts(ctx context.Context, meta *tableMeta) (int64, []int64, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if len(meta.Columns) > 4096 {
		return 0, nil, errorf(adbc.StatusNotImplemented, "column statistics exceed 4096 columns")
	}
	nulls := make([]int64, len(meta.Columns))
	scan := newSnapshotDeltaScan(c.store, meta, []string{rowIDField})
	var count, fieldWork int64
	for {
		logical, physical, err := scan.nextKeys(ctx)
		var directoryWork int64
		for _, source := range scan.sources {
			consumed := source.offset - int64(len(source.ids)) + int64(source.at)
			if consumed < 0 || consumed > 2_000_000-directoryWork-fieldWork {
				return 0, nil, codeStateErrorf(adbc.StatusInvalidState, "53200", "exact statistics scan budget exhausted")
			}
			directoryWork += consumed
		}
		if errors.Is(err, io.EOF) {
			return count, nulls, nil
		}
		if err != nil {
			return 0, nil, err
		}
		rows, err := c.store.fetchSnapshotPhysical(ctx, physical, scan.fields)
		if err != nil {
			return 0, nil, err
		}
		for i, row := range rows {
			id := strings.TrimPrefix(logical[i], meta.prefix())
			if row == nil || row[rowIDField] != id {
				return 0, nil, errorf(adbc.StatusIO, "snapshot statistics references a missing or invalid row")
			}
			if row[snapshotDeletedField] == "1" {
				continue
			}
			probes := 2 * int64(len(meta.Columns))
			if probes > 2_000_000-directoryWork-fieldWork {
				return 0, nil, codeStateErrorf(adbc.StatusInvalidState, "53200", "exact statistics scan budget exhausted")
			}
			fieldWork += probes
			if err := c.statisticNullFields(ctx, meta, physical[i], id, nulls); err != nil {
				return 0, nil, err
			}
			count++
		}
	}
}
