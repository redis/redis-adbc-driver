// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"strconv"
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

// GetStatistics exposes Search row counts or authoritative HASH row/NULL
// counts for permanent managed tables. Both paths are best effort under
// concurrent DML; neither provides a source snapshot. Other quantities omit.
func (c *connectionImpl) GetStatistics(ctx context.Context, catalog, dbSchema, tableName *string, approximate bool) (array.RecordReader, error) {
	if err := c.statisticsAccess(ctx); err != nil {
		return nil, err
	}
	ctx, transport := c.executionContext(ctx, &c.streams)
	if transport != nil {
		defer transport.finish()
	}
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
				if approximate {
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
	record := b.NewRecordBatch()
	defer record.Release()
	return array.NewRecordReader(adbc.GetStatisticsSchema, []arrow.RecordBatch{record})
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
					return 0, nil, wrapRedis(err, "column statistics field existence read failed")
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
						return 0, nil, errorf(adbc.StatusInvalidData, "invalid column statistics missing value")
					}
					if v.Null {
						nulls[col]++
					}
				}
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
