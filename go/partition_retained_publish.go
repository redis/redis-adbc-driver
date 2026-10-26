// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	goredis "github.com/redis/go-redis/v9"
)

// Test seam after upload and before final source validation/atomic publication.
var testHookRetainedBeforePublish func(*retainedUpload)

type retainedCoordinatorOwner struct{ cancel context.CancelCauseFunc }

func (o *retainedCoordinatorOwner) closeWith(err error) { o.cancel(err) }

type retainedOutputBuffer struct {
	data  []byte
	limit int64
}

func (b *retainedOutputBuffer) Write(data []byte) (int, error) {
	if int64(len(data)) > b.limit-int64(len(b.data)) {
		return 0, codeStateErrorf(adbc.StatusInvalidState, "53200", "retained Arrow chunk exceeds its byte limit")
	}
	b.data = append(b.data, data...)
	return len(data), nil
}
func retainedArrowValueEstimate(value Value) int64 {
	bytes := int64(32 + len(value.S))
	if value.D != nil {
		bytes += 32
	}
	for _, child := range value.fields() {
		bytes += retainedArrowValueEstimate(child)
	}
	return bytes
}
func retainedArrowSchemaEstimate(cols []resultColumn) int64 {
	total := int64(4096)
	var count func(ColType)
	count = func(t ColType) {
		total += 1024 + int64(len(t.TZ))*4
		if t.Rec != nil {
			for _, child := range t.Rec.fields {
				count(child)
			}
		}
	}
	for _, col := range cols {
		total += int64(len(col.Name)) * 4
		count(col.Type)
	}
	return total
}
func retainedPublisherClient(c *connectionImpl) goredis.UniversalClient {
	single, cluster := *c.clientCfg.single, *c.clientCfg.cluster
	single.ContextTimeoutEnabled = true
	cluster.ContextTimeoutEnabled = true
	single.PushNotificationProcessor = nil
	cluster.PushNotificationProcessor = nil
	config := &clientConfig{single: &single, cluster: &cluster}
	_, isCluster := c.store.client.(*goredis.ClusterClient)
	return newClient(config, isCluster, c.timeouts)
}

func (s *statementImpl) executeRetainedPartitions(ctx context.Context) (*arrow.Schema, adbc.Partitions, int64, error) {
	fail := func(err error) (*arrow.Schema, adbc.Partitions, int64, error) { return nil, adbc.Partitions{}, -1, err }
	if err := s.checkExecution(); err != nil {
		return fail(err)
	}
	if s.conn.store == nil || s.conn.store.isClosed() {
		return fail(errConnClosed)
	}
	if s.conn.retainedNamespace == "" {
		return fail(errorf(adbc.StatusInvalidArgument, "retained execution requires partition_result_namespace"))
	}
	if s.ingest.TableName != "" || len(s.query) == 0 || len(s.query) > 32<<10 {
		return fail(errorf(adbc.StatusInvalidArgument, "retained execution requires one SELECT of at most 32 KiB"))
	}
	parsed, err := ParseScript(s.query)
	if err != nil {
		return fail(invalidArg(err))
	}
	if len(parsed) != 1 {
		return fail(errorf(adbc.StatusNotImplemented, "retained execution requires one SELECT"))
	}
	selectStmt, ok := parsed[0].Stmt.(*SelectStmt)
	if !ok {
		return fail(errorf(adbc.StatusNotImplemented, "retained execution requires one SELECT"))
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	owner := &retainedCoordinatorOwner{cancel: cancel}
	s.streams.add(owner)
	s.conn.streams.add(owner)
	if s.conn.store.isClosed() {
		cancel(errConnClosed)
	}
	defer s.streams.remove(owner)
	defer s.conn.streams.remove(owner)
	ctx, exec, execution, close, err := newRetainedExecutor(ctx, s.executor(), s.conn.clientCfg, s.conn.timeouts, s.executionLimits())
	if err != nil {
		return fail(err)
	}
	defer close()
	if err := execution.vet(selectStmt, 0); err != nil {
		return fail(err)
	}
	_, params, types, err := s.partitionParams(ctx)
	if err != nil {
		return fail(err)
	}
	if err := checkPartitionParams(s.query, len(params)); err != nil {
		return fail(err)
	}
	publisher := retainedPublisherClient(s.conn)
	stopPublisher := context.AfterFunc(ctx, func() { _ = publisher.Close() })
	defer func() { stopPublisher(); _ = publisher.Close() }()
	limits := s.conn.retainedLimits
	if limits.JobBytes == 0 {
		limits = defaultRetainedLimits()
	}
	store, err := newRetainedStore(publisher, s.conn.retainedNamespace, limits)
	if err != nil {
		return fail(err)
	}
	if err := store.checkControlPolicy(ctx, s.conn.partitionControlPersistence); err != nil {
		return fail(err)
	}
	upload, err := store.begin(ctx, limits.JobBytes)
	if err != nil {
		return fail(err)
	}
	defer func() {
		if !upload.done {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			// The producer pool may have been closed to interrupt cancelled I/O.
			// Cleanup uses a fresh, deadline-aware pool with the same credentials.
			cleanupClient := retainedPublisherClient(s.conn)
			stopCleanup := context.AfterFunc(cleanup, func() { _ = cleanupClient.Close() })
			defer func() { stopCleanup(); _ = cleanupClient.Close() }()
			cleanupStore := *store
			cleanupStore.client = cleanupClient
			abandoned := *upload
			abandoned.store = &cleanupStore
			_ = abandoned.abort(cleanup)
		}
	}()
	result, err := exec.execute(ctx, parsed[0], params, types, nil)
	if err != nil {
		return fail(err)
	}
	if !result.isQuery || result.stream != nil || len(result.cols) > 4096 {
		return fail(errorf(adbc.StatusInvalidState, "invalid retained coordinator result"))
	}
	schemaEstimate := retainedArrowSchemaEstimate(result.cols)
	if schemaEstimate >= limits.ChunkBytes {
		return fail(codeStateErrorf(adbc.StatusInvalidState, "53200", "retained result schema exceeds chunk budget"))
	}
	schema := resultSchema(result.cols)
	schemaHash := ""
	for start := 0; start < len(result.rows) || start == 0; {
		end := start
		estimated := schemaEstimate
		var batchBytes int64
		for end < len(result.rows) && end-start < min(s.stream.rows(), retainedIPCRows) {
			var cost int64
			if s.stream.batchBytes > 0 {
				cost = batchRowBytes(result.rows[end])
				if end > start && cost > s.stream.batchBytes-batchBytes {
					break
				}
			}
			rowBytes := int64(16)
			for _, value := range result.rows[end] {
				rowBytes += retainedArrowValueEstimate(value)
			}
			if rowBytes > limits.ChunkBytes-schemaEstimate {
				return fail(codeStateErrorf(adbc.StatusInvalidState, "53200", "retained result row exceeds chunk budget"))
			}
			if rowBytes > limits.ChunkBytes-estimated {
				break
			}
			estimated += rowBytes
			batchBytes = addBatchBytes(batchBytes, cost)
			end++
		}
		if err := cancelled(ctx); err != nil {
			return fail(err)
		}
		if err := execution.working.reserve(estimated*4 + 4096); err != nil {
			return fail(err)
		}
		record, err := buildRecord(s.conn.Alloc, result.cols, result.rows[start:end])
		if err != nil {
			return fail(invalidArg(err))
		}
		output := &retainedOutputBuffer{limit: limits.ChunkBytes}
		writer := ipc.NewWriter(output, ipc.WithSchema(schema), ipc.WithAllocator(s.conn.Alloc))
		writeErr := writer.Write(record)
		record.Release()
		closeErr := writer.Close()
		if writeErr != nil {
			return fail(writeErr)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		frame, err := preflightRetainedIPC(output.data)
		if err != nil {
			return fail(err)
		}
		if schemaHash == "" {
			schemaHash = frame.schema
		} else if schemaHash != frame.schema {
			return fail(errorf(adbc.StatusInvalidData, "retained output schema changed between chunks"))
		}
		check, err := decodeRetainedIPC(output.data, schemaHash, int64(end-start))
		if err != nil {
			return fail(err)
		}
		check.Release()
		if err := upload.put(ctx, output.data, int64(end-start)); err != nil {
			return fail(err)
		}
		if end == len(result.rows) {
			break
		}
		start = end
	}
	if testHookRetainedBeforePublish != nil {
		testHookRetainedBeforePublish(upload)
	}
	if err := execution.check(ctx); err != nil {
		return fail(err)
	}
	if err := store.checkControlPolicy(ctx, s.conn.partitionControlPersistence); err != nil {
		return fail(err)
	}
	reference, err := upload.commit(ctx, schemaHash, s.resultTTL())
	if err != nil {
		return fail(err)
	}
	count := min(s.partitions(), len(upload.chunks))
	partitions := adbc.Partitions{NumPartitions: uint64(count), PartitionIDs: make([][]byte, count)}
	for i := range count {
		descriptor := retainedPartitionID{Version: 2, Reference: reference, Start: i * len(upload.chunks) / count, End: (i + 1) * len(upload.chunks) / count}
		data, err := json.Marshal(descriptor)
		if err != nil {
			return fail(err)
		}
		partitions.PartitionIDs[i] = data
	}
	return schema, partitions, int64(len(result.rows)), nil
}

// Retained replay uses a separate, connection-owned pool with bounded socket
// waits even when ordinary SQL timeouts are disabled. Existing readers retain
// their pool across option changes; every cohort closes with the connection.
func (c *connectionImpl) retainedReadStore(ctx context.Context) (*retainedStore, error) {
	if transport, _ := ctx.Value(transportContextKey{}).(*executionTransport); transport != nil && transport.owner == c.store {
		return retainedReaderStore(ctx, c.store.redis(ctx), c.retainedNamespace)
	}
	c.store.mu.Lock()
	if c.store.closed {
		c.store.mu.Unlock()
		return nil, errConnClosed
	}
	client := c.retainedClient
	if client == nil {
		single, cluster := *c.clientCfg.single, *c.clientCfg.cluster
		single.ContextTimeoutEnabled = true
		cluster.ContextTimeoutEnabled = true
		single.MaxRetries = -1
		cluster.MaxRetries = -1
		single.PushNotificationProcessor = nil
		cluster.PushNotificationProcessor = nil
		t := c.timeouts
		if t.read <= 0 || t.read > 30*time.Second {
			t.read = 30 * time.Second
		}
		if t.write <= 0 || t.write > 30*time.Second {
			t.write = 30 * time.Second
		}
		_, isCluster := c.store.client.(*goredis.ClusterClient)
		client = newClient(&clientConfig{single: &single, cluster: &cluster}, isCluster, t)
		c.retainedClient = client
		c.store.retired = append(c.store.retired, client)
	}
	c.store.mu.Unlock()
	return retainedReaderStore(ctx, client, c.retainedNamespace)
}
