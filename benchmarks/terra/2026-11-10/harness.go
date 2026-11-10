package main

import (
	"context"
	"encoding/json"
	"fmt"
	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func emit(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func update(c adbc.ConnectionWithContext, sql string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	st, e := c.NewStatement(ctx)
	if e != nil {
		return e
	}
	defer st.Close(context.Background())
	if e = st.SetSqlQuery(ctx, sql); e != nil {
		return e
	}
	_, e = st.ExecuteUpdate(ctx)
	return e
}
func query(c adbc.ConnectionWithContext, sql string, expected []string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	st, e := c.NewStatement(ctx)
	if e != nil {
		return 0, e
	}
	defer st.Close(context.Background())
	if e = st.SetSqlQuery(ctx, sql); e != nil {
		return 0, e
	}
	start := time.Now()
	r, _, e := st.ExecuteQuery(ctx)
	if e != nil {
		return 0, e
	}
	defer r.Release()
	var batches []arrow.RecordBatch
	defer func() {
		for _, b := range batches {
			b.Release()
		}
	}()
	for r.Next() {
		b := r.RecordBatch()
		b.Retain()
		batches = append(batches, b)
	}
	elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
	if e = r.Err(); e != nil {
		return 0, e
	}
	// Validate exact complete values outside the execute + full Arrow fetch timer.
	actual := make([]string, 0, len(expected))
	for _, b := range batches {
		for i := 0; i < int(b.NumRows()); i++ {
			v := make([]string, b.NumCols())
			for j, a := range b.Columns() {
				if a.IsNull(i) {
					v[j] = "NULL"
				} else {
					v[j] = a.ValueStr(i)
				}
			}
			actual = append(actual, strings.Join(v, "|"))
		}
	}
	sort.Strings(actual)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if len(actual) != len(want) {
		return 0, fmt.Errorf("row count %d != %d", len(actual), len(want))
	}
	for i := range want {
		if actual[i] != want[i] {
			return 0, fmt.Errorf("row %d: %q != %q", i, actual[i], want[i])
		}
	}
	return elapsed, nil
}
func main() {
	defer func() {
		if v := recover(); v != nil {
			emit(map[string]any{"kind": "fatal", "error": fmt.Sprint(v)})
			os.Exit(1)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := redis.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_URI"), redis.OptionStringTelemetryEnabled: "false", redis.OptionStringQueryWorkers: "4"})
	must(e)
	defer db.Close(context.Background())
	t := time.Now()
	c, e := db.Open(ctx)
	if e != nil {
		emit(map[string]any{"kind": "driver_open", "ok": false, "error": e.Error(), "elapsed_ms": time.Since(t).Milliseconds()})
		return
	}
	defer c.Close(context.Background())
	emit(map[string]any{"kind": "driver_open", "ok": true})
	schema := "terra_bench_" + strconv.FormatInt(time.Now().Unix(), 10)
	must(update(c, "CREATE SCHEMA "+schema))
	must(update(c, "SET search_path TO "+schema))
	defer func() {
		if e := update(c, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			emit(map[string]any{"kind": "cleanup_error", "error": e.Error(), "schema": schema})
		} else {
			emit(map[string]any{"kind": "fixture_cleanup", "ok": true})
		}
	}()
	for _, n := range []int{10000, 100000} {
		tab := fmt.Sprintf("rows_%d", n)
		b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "grp", Type: arrow.BinaryTypes.String}, {Name: "qty", Type: arrow.PrimitiveTypes.Int32}, {Name: "amount", Type: arrow.PrimitiveTypes.Int64}}, nil))
		all := make([]string, 0, n)
		point := []string{}
		filtered := []string{}
		tenth := []string{}
		top := []string{}
		counts := make([]int64, 10)
		sums := make([]int64, 10)
		var rangeCount, total int64
		for i := 1; i <= n; i++ {
			g := i % 10
			q := i%20 + 1
			amount := int64(i) * 7
			group := fmt.Sprintf("g%02d", g)
			b.Field(0).(*array.Int64Builder).Append(int64(i))
			b.Field(1).(*array.StringBuilder).Append(group)
			b.Field(2).(*array.Int32Builder).Append(int32(q))
			b.Field(3).(*array.Int64Builder).Append(amount)
			line := fmt.Sprintf("%d|%s|%d|%d", i, group, q, amount)
			all = append(all, line)
			if i == 4242 {
				point = append(point, line)
			}
			if g == 3 && len(filtered) < 10 {
				filtered = append(filtered, line)
			}
			if i <= n/10 {
				tenth = append(tenth, line)
			}
			if i > n-10 {
				top = append(top, line)
			}
			if q >= 5 && q <= 10 {
				rangeCount++
			}
			counts[g]++
			sums[g] += int64(q)
			total += amount
		}
		rec := b.NewRecordBatch()
		b.Release()
		st, e := c.NewStatement(context.Background())
		must(e)
		must(st.SetOption(context.Background(), adbc.OptionKeyIngestTargetTable, tab))
		must(st.SetOption(context.Background(), adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeCreate))
		must(st.Bind(context.Background(), rec))
		rec.Release()
		ictx, icancel := context.WithTimeout(context.Background(), 10*time.Minute)
		t = time.Now()
		_, e = st.ExecuteUpdate(ictx)
		dt := time.Since(t).Seconds()
		icancel()
		st.Close(context.Background())
		must(e)
		emit(map[string]any{"kind": "ingest", "rows": n, "columns": 4, "seconds": dt, "rows_per_second": float64(n) / dt})
		grouped := []string{}
		for g := 0; g < 10; g++ {
			grouped = append(grouped, fmt.Sprintf("g%02d|%d|%d", g, counts[g], sums[g]))
		}
		columns := "id,grp,qty,amount"
		cases := []struct {
			name, sql string
			expected  []string
		}{
			{"point_lookup", "SELECT " + columns + " FROM " + tab + " WHERE id=4242", point},
			{"indexed_filter_top10", "SELECT " + columns + " FROM " + tab + " WHERE grp='g03' ORDER BY id LIMIT 10", filtered},
			{"indexed_range_count", "SELECT COUNT(*) FROM " + tab + " WHERE qty BETWEEN 5 AND 10", []string{fmt.Sprint(rangeCount)}},
			{"group_count_sum", "SELECT grp,COUNT(*),SUM(qty) FROM " + tab + " GROUP BY grp", grouped},
			{"exact_int64_sum", "SELECT SUM(amount) FROM " + tab, []string{fmt.Sprint(total)}},
			{"fetch_10pct", "SELECT " + columns + " FROM " + tab + " WHERE id<=" + fmt.Sprint(n/10), tenth},
			{"full_arrow_scan", "SELECT " + columns + " FROM " + tab, all},
			{"order_top10", "SELECT " + columns + " FROM " + tab + " ORDER BY amount DESC LIMIT 10", top},
		}
		for _, tc := range cases {
			_, e = query(c, tc.sql, tc.expected)
			if e != nil {
				emit(map[string]any{"kind": "query_error", "rows": n, "query": tc.name, "phase": "warmup", "error": e.Error()})
				continue
			}
			times := []float64{}
			valid := true
			for j := 0; j < 5; j++ {
				ms, e := query(c, tc.sql, tc.expected)
				if e != nil {
					emit(map[string]any{"kind": "query_error", "rows": n, "query": tc.name, "error": e.Error()})
					valid = false
					break
				}
				times = append(times, ms)
			}
			if valid {
				ordered := append([]float64(nil), times...)
				sort.Float64s(ordered)
				emit(map[string]any{"kind": "query", "dataset_rows": n, "query": tc.name, "result_rows": len(tc.expected), "trials_ms": times, "median_ms": ordered[2], "min_ms": ordered[0], "max_ms": ordered[4], "validated": true})
			}
		}
	}
}
