// Command prefetch-screen measures complete fetch through the public Go ADBC
// APIs. Configuration (including credentials) arrives on stdin, never argv.
// IPC output is independently validated by the Python controller after timing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type config struct {
	URI, CA, API, SQL, Output string
	Queries                   []string
	Readers, BatchRows        int
}

type metric struct {
	Rows, Batches, FirstRows int64
	FirstSeconds             float64
}

func cpu() float64 {
	var r syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
}

func run(c config) (map[string]any, error) {
	if c.Readers != 1 && c.Readers != 4 || c.BatchRows < 1 || (c.API != "regular" && c.API != "partition") {
		return nil, fmt.Errorf("invalid screen configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	db, err := redis.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI: c.URI, redis.OptionStringTLSCAFile: c.CA,
		redis.OptionIntStreamBatchRows: fmt.Sprint(c.BatchRows),
	})
	if err != nil {
		return nil, err
	}
	defer db.Close(context.Background())
	connections := make([]adbc.ConnectionWithContext, c.Readers)
	for i := range connections {
		connections[i], err = db.Open(ctx)
		if err != nil {
			return nil, err
		}
		defer connections[i].Close(context.Background())
	}
	if err = os.MkdirAll(c.Output, 0700); err != nil {
		return nil, err
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	stop, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var sample runtime.MemStats
				runtime.ReadMemStats(&sample)
				for old := peak.Load(); sample.HeapAlloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, sample.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	defer func() { close(stop); <-joined }()
	startedCPU, start := cpu(), time.Now()
	var descriptors [][]byte
	if c.API == "partition" {
		st, err := connections[0].NewStatement(ctx)
		if err != nil {
			return nil, err
		}
		defer st.Close(ctx)
		if err = st.SetOption(ctx, redis.OptionIntPartitionCount, "4"); err != nil {
			return nil, err
		}
		if err = st.SetSqlQuery(ctx, c.SQL); err != nil {
			return nil, err
		}
		_, parts, _, err := st.ExecutePartitions(ctx)
		if err != nil {
			return nil, err
		}
		descriptors = parts.PartitionIDs
	} else if len(c.Queries) != c.Readers {
		return nil, fmt.Errorf("one regular query required per reader")
	}
	count := len(descriptors)
	if c.API == "regular" {
		count = len(c.Queries)
	}
	metrics := make([]metric, count)
	errCh := make(chan error, c.Readers)
	var wg sync.WaitGroup
	for worker := range c.Readers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for task := worker; task < count; task += c.Readers {
				var reader array.RecordReader
				var err error
				var statement adbc.StatementWithContext
				if c.API == "partition" {
					reader, err = connections[worker].ReadPartition(ctx, descriptors[task])
				} else {
					statement, err = connections[worker].NewStatement(ctx)
					if err == nil {
						err = statement.SetSqlQuery(ctx, c.Queries[task])
					}
					if err == nil {
						reader, _, err = statement.ExecuteQuery(ctx)
					}
				}
				if err != nil {
					if statement != nil {
						_ = statement.Close(ctx)
					}
					errCh <- err
					cancel()
					return
				}
				f, err := os.Create(filepath.Join(c.Output, fmt.Sprintf("part-%03d.arrow", task)))
				if err != nil {
					reader.Release()
					if statement != nil {
						_ = statement.Close(ctx)
					}
					errCh <- err
					cancel()
					return
				}
				writer := ipc.NewWriter(f, ipc.WithSchema(reader.Schema()))
				m := &metrics[task]
				for reader.Next() {
					rec := reader.RecordBatch()
					if m.Batches == 0 {
						m.FirstSeconds = time.Since(start).Seconds()
						m.FirstRows = rec.NumRows()
					}
					m.Rows += rec.NumRows()
					m.Batches++
					if err = writer.Write(rec); err != nil {
						break
					}
				}
				if err == nil {
					err = reader.Err()
				}
				reader.Release()
				if e := writer.Close(); err == nil {
					err = e
				}
				if e := f.Close(); err == nil {
					err = e
				}
				if statement != nil {
					if e := statement.Close(ctx); err == nil {
						err = e
					}
				}
				if err != nil {
					errCh <- err
					cancel()
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	seconds, usedCPU := time.Since(start).Seconds(), cpu()-startedCPU
	runtime.ReadMemStats(&after)
	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}
	var usage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	peakRSS := uint64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		peakRSS *= 1024
	}
	return map[string]any{"seconds": seconds, "client_cpu_seconds": usedCPU, "allocated_bytes": after.TotalAlloc - before.TotalAlloc,
		"allocations": after.Mallocs - before.Mallocs, "sampled_peak_go_heap_bytes": max(peak.Load(), after.HeapAlloc),
		"heap_before_bytes": before.HeapAlloc, "heap_after_bytes": after.HeapAlloc, "metrics": metrics,
		"process_lifetime_peak_rss_bytes": peakRSS,
		"api":                             c.API, "readers": c.Readers, "batch_rows": c.BatchRows, "gomaxprocs": runtime.GOMAXPROCS(0),
		"timing":    "public API execute/partition planning plus complete Arrow fetch and IPC output; connections preopened; independent validation excluded",
		"allocator": "Go Arrow allocator; not C shared-library allocation"}, nil
}

func main() {
	var c config
	if err := json.NewDecoder(os.Stdin).Decode(&c); err != nil {
		fmt.Fprintln(os.Stderr, "invalid config JSON")
		os.Exit(1)
	}
	r, err := run(c)
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"error": err.Error()})
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
}
