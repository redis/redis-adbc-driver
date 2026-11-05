// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotCompactionTrigger = 4
const snapshotCompactionBatchRows = 64
const snapshotCompactionBatchBytes = 1 << 20
const snapshotCompactionStepTimeout = 2 * time.Second

type snapshotCompactionJob struct {
	QueueOID     string                         `json:"queue_oid"`
	QueueKey     string                         `json:"queue_key"`
	QueueVersion string                         `json:"queue_version"`
	ID           string                         `json:"id"`
	Token        string                         `json:"token"`
	Data         string                         `json:"data"`
	Prefix       string                         `json:"prefix"`
	Source       *tableMeta                     `json:"source"`
	References   []string                       `json:"references"`
	After        string                         `json:"after"`
	Phase        string                         `json:"phase"`
	LastError    string                         `json:"last_error,omitempty"`
	Rows         int64                          `json:"rows"`
	CopyBatch    *snapshotCompactionCopyOutcome `json:"copy_batch,omitempty"`
	CopyPending  *snapshotCompactionCopyOutcome `json:"copy_pending,omitempty"`
}
type snapshotCompactor struct {
	store  *store
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
	token  string
}

func (s *store) startSnapshotCompactor(cfg *clientConfig, t timeouts, hooks ...goredis.Hook) {
	ctx, cancel := context.WithCancel(context.Background())
	// Maintenance has its own pool and bounded contexts; statement cancellation
	// or timeout replacement must never close or race this client's sockets.
	_, cluster := s.client.(*goredis.ClusterClient)
	worker := &store{client: newClient(cfg, cluster, t), snapshots: true}
	addSnapshotHooks(worker.client)
	for _, hook := range hooks {
		worker.client.AddHook(hook)
	}
	c := &snapshotCompactor{store: worker, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), token: snapshotID()}
	s.mu.Lock()
	s.compactor = c
	s.mu.Unlock()
	go c.run(ctx)
	c.wake <- struct{}{}
}
func (s *store) wakeSnapshotCompactor() {
	s.mu.Lock()
	c := s.compactor
	s.mu.Unlock()
	if c != nil {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}
func (s *store) stopSnapshotCompactor() {
	s.mu.Lock()
	c := s.compactor
	s.compactor = nil
	s.mu.Unlock()
	if c == nil {
		return
	}
	c.cancel()
	select {
	case <-c.done:
		return
	case <-time.After(snapshotCompactionStepTimeout + 500*time.Millisecond):
		// Force interruption of an in-flight socket. The durable lease/checkpoint
		// remains recoverable if cooperative release cannot reach Redis.
		_ = c.store.client.Close()
		<-c.done
	}
}
func (c *snapshotCompactor) run(ctx context.Context) {
	defer close(c.done)
	defer c.store.client.Close()
	defer func() {
		bg, cancel := context.WithTimeout(context.Background(), snapshotCompactionStepTimeout)
		defer cancel()
		_ = c.store.releaseSnapshotCompaction(bg, c.token)
	}()
	delay := time.Second
	active := false
	lastOrphanCheck := time.Time{}
	for {
		if !active {
			select {
			case <-ctx.Done():
				return
			case <-c.wake:
			case <-time.After(delay):
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(lastOrphanCheck) > 5*time.Second {
			cleanup, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			_ = c.store.reapSnapshotOrphanIndex(cleanup)
			cancel()
			lastOrphanCheck = time.Now()
		}
		step, cancel := context.WithTimeout(ctx, snapshotCompactionStepTimeout)
		progress, err := c.store.snapshotCompactionStep(step, c.token)
		cancel()
		active = progress && err == nil
		delay = time.Second
		if err != nil {
			// A failing worker must not monopolize the durable lease while a
			// healthy connection could resume it with the same checkpoint.
			release, cancelRelease := context.WithTimeout(context.Background(), snapshotCompactionStepTimeout)
			_ = c.store.releaseSnapshotCompaction(release, c.token)
			cancelRelease()
			delay = 100 * time.Millisecond
		}
	}
}

// Maintenance source lookup needs a pinned manifest but does not read query rows.
// Avoid validating all catalog indexes for each bounded maintenance batch.
func (s *store) acquireCompactionCatalog(ctx context.Context) (context.Context, *snapshotPin, error) {
	p := &snapshotPin{ctx: ctx, store: s, token: snapshotID(), stop: make(chan struct{}), done: make(chan struct{})}
	root, err := s.snapshotControl(ctx, "acquire", p.token, "", "").Text()
	if err != nil {
		return ctx, nil, wrapRedis(err, "failed to pin compaction catalog")
	}
	p.root = root
	go p.heartbeat()
	if err := s.validateSnapshotMetadata(ctx, root); err != nil {
		p.release()
		return ctx, nil, err
	}
	return context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: root, pin: p, store: s}), p, nil
}
func validateSnapshotCompactionJob(j *snapshotCompactionJob) error {
	valid := func(id string) bool { return len(id) == 32 && snapshotDataID(snapshotRows(id)+"1") == id }
	if j.Source == nil || j.QueueOID != strconv.FormatInt(j.Source.OID, 10) || !valid(j.QueueVersion) || j.QueueKey != strings.TrimPrefix(metaKey(j.Source.Schema, j.Source.Name), metaPrefix) || !valid(j.ID) || !valid(j.Data) || !valid(j.Token) || len(j.Source.Columns) == 0 || j.Source.SnapshotRows == nil {
		return errorf(adbc.StatusIO, "invalid durable compaction record")
	}
	if j.Prefix != snapshotRows(j.Data, j.Source.Schema) {
		return errorf(adbc.StatusIO, "invalid durable compaction output prefix")
	}
	if j.Phase != "copy" && j.Phase != "index" && j.Phase != "install" {
		return errorf(adbc.StatusIO, "invalid durable compaction phase")
	}
	segments := j.Source.snapshotSegments()
	if len(segments) > snapshotMaxSegments || len(segments) != len(j.References) {
		return errorf(adbc.StatusIO, "invalid durable compaction sources")
	}
	for i, seg := range segments {
		id := snapshotDataID(seg.Prefix)
		if id == "" || seg.Prefix != snapshotRows(id, j.Source.Schema) || id != j.References[i] || snapshotDataID(seg.Index) != id || seg.Rows < 0 {
			return errorf(adbc.StatusIO, "invalid durable compaction segment")
		}
	}
	if j.After != "" {
		n, err := strconv.ParseInt(j.After, 10, 64)
		if err != nil || n < 1 || fmt.Sprintf("%020d", n) != j.After {
			return errorf(adbc.StatusIO, "invalid durable compaction checkpoint")
		}
	}
	for _, outcome := range []*snapshotCompactionCopyOutcome{j.CopyBatch, j.CopyPending} {
		if outcome == nil {
			continue
		}
		canonical := func(value string) bool {
			n, err := strconv.ParseInt(value, 10, 64)
			return err == nil && n > 0 && fmt.Sprintf("%020d", n) == value
		}
		if !valid(outcome.ID) || !canonical(outcome.After) || (outcome.Before != "" && !canonical(outcome.Before)) || outcome.After <= outcome.Before || outcome.Rows < 0 || outcome.Rows > snapshotCompactionBatchRows {
			return errorf(adbc.StatusIO, "invalid compaction copy outcome")
		}
	}
	if j.CopyBatch != nil && j.CopyBatch.After > j.After {
		return errorf(adbc.StatusIO, "compaction outcome exceeds checkpoint")
	}
	if j.CopyPending != nil && (j.Phase != "copy" || j.CopyPending.Before != j.After) {
		return errorf(adbc.StatusIO, "invalid incomplete compaction copy")
	}
	return nil
}

func (s *store) readSnapshotCompaction(ctx context.Context) (*snapshotCompactionJob, error) {
	raw, err := s.client.HGet(ctx, snapshotCatalogKey, "compaction").Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job snapshotCompactionJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, err
	}
	if err := validateSnapshotCompactionJob(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

func snapshotCompactionPrefix(current *tableMeta, j *snapshotCompactionJob) (matches, installed bool) {
	if current.OID != j.Source.OID {
		return false, false
	}
	sources, segs := j.Source.snapshotSegments(), current.snapshotSegments()
	for _, seg := range segs {
		if snapshotDataID(seg.Prefix) == j.Data {
			return false, true
		}
	}
	if len(segs) < len(sources) {
		return false, false
	}
	for i, seg := range sources {
		if seg != segs[i] {
			return false, false
		}
	}
	return true, false
}
func (s *store) snapshotCompactionCurrent(ctx context.Context, j *snapshotCompactionJob) (*tableMeta, bool, error) {
	cctx, pin, err := s.acquireCompactionCatalog(ctx)
	if err != nil {
		return nil, false, err
	}
	defer pin.release()
	meta, err := s.getTable(cctx, j.Source.Schema, j.Source.Name)
	var ae adbc.Error
	if errors.As(err, &ae) && ae.Code == adbc.StatusNotFound {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	match, installed := snapshotCompactionPrefix(meta, j)
	return meta, installed || !match, nil
}
func (s *store) checkpointSnapshotCompaction(ctx context.Context, j *snapshotCompactionJob) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	n, err := s.snapshotControl(ctx, "compact_checkpoint", j.Token, j.ID, "", string(raw)).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errorf(adbc.StatusIO, "background compaction lease lost")
	}
	return nil
}
func (s *store) finishSnapshotCompaction(ctx context.Context, j *snapshotCompactionJob) error {
	n, err := s.snapshotControl(ctx, "compact_finish", j.Token, j.ID, "").Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return errorf(adbc.StatusIO, "background compaction lease lost")
	}
	return nil
}
func (s *store) releaseSnapshotCompaction(ctx context.Context, token string) error {
	j, err := s.readSnapshotCompaction(ctx)
	if err != nil || j == nil {
		return err
	}
	if j.Token != token {
		return nil
	}
	return s.snapshotControl(ctx, "compact_release", token, j.ID, "").Err()
}
func (s *store) snapshotCompactionStep(ctx context.Context, token string) (progress bool, err error) {
	s.reapSnapshotBudget(ctx, 500*time.Millisecond)
	raw, err := s.snapshotControl(ctx, "compact_acquire", token, "", "").Text()
	if err != nil {
		return false, err
	}
	if raw == "" {
		job, err := s.readSnapshotCompaction(ctx)
		if err != nil || job != nil {
			return false, err
		}
		return s.startSnapshotCompaction(ctx, token)
	}
	var job snapshotCompactionJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return false, err
	}
	j := &job
	if err := validateSnapshotCompactionJob(j); err != nil {
		return false, err
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			// Preserve a checkpoint that may have committed before its reply was
			// lost. Logging must never write our stale local job over it.
			_ = s.snapshotControl(ctx, "compact_error", j.Token, j.ID, "", err.Error()).Err()
		}
	}()
	_, obsolete, err := s.snapshotCompactionCurrent(ctx, j)
	if err != nil {
		return false, err
	}
	if obsolete {
		return true, s.finishSnapshotCompaction(ctx, j)
	}
	switch j.Phase {
	case "copy":
		return true, s.copySnapshotCompactionBatch(ctx, j)
	case "index":
		return s.indexSnapshotCompaction(ctx, j)
	case "install":
		return s.installSnapshotCompaction(ctx, j)
	default:
		return false, errorf(adbc.StatusIO, "invalid background compaction phase")
	}
}
func (s *store) compactionWriteContext(ctx context.Context, j *snapshotCompactionJob) context.Context {
	return context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: j.ID, pin: &snapshotPin{token: j.Token}, write: true, owned: map[string]bool{j.Data: true}})
}
func (s *store) copySnapshotCompactionBatch(ctx context.Context, j *snapshotCompactionJob) error {
	wctx := s.compactionWriteContext(ctx, j)
	scan := newSnapshotDeltaScan(s, j.Source, nil)
	scan.pageRows = snapshotCompactionBatchRows
	for i := range scan.sources {
		if j.After != "" {
			offset, err := s.redis(ctx).ZLexCount(ctx, snapshotMembers(snapshotDataID(scan.sources[i].segment.Prefix)), "-", "["+j.After).Result()
			if err != nil {
				return err
			}
			scan.sources[i].offset = offset
		}
	}
	keys, rows, err := scan.next(wctx)
	if errors.Is(err, io.EOF) {
		j.Phase = "index"
		j.LastError = ""
		return s.checkpointSnapshotCompaction(ctx, j)
	}
	if err != nil {
		return err
	}
	var images []snapshotInsertRow
	bytes := 0
	after := scan.last
	for i, row := range rows {
		id := strings.TrimPrefix(keys[i], j.Source.prefix())
		key := j.Prefix + id
		fields := []any{rowIDField, row[rowIDField]}
		for field, value := range row {
			if field != rowIDField {
				fields = append(fields, field, value)
			}
		}
		member := snapshotDirectoryMember(key)
		size := len(key) + len(member) + len(strconv.Itoa(len(fields)))
		for _, field := range fields {
			size += len(field.(string))
		}
		if i > 0 && bytes+size > snapshotCompactionBatchBytes {
			after = snapshotDirectoryMember(keys[i-1])
			break
		}
		bytes += size
		images = append(images, snapshotInsertRow{key: key, member: member, fields: fields})
	}
	return s.commitSnapshotCompactionCopy(ctx, j, after, images)
}
func (s *store) indexSnapshotCompaction(ctx context.Context, j *snapshotCompactionJob) (bool, error) {
	output := *j.Source
	output.KeyPrefix = j.Prefix
	output.IndexName = snapshotIndexPrefix(j.Data) + "compacted"
	wctx := s.compactionWriteContext(ctx, j)
	reply, err := s.searchDo(wctx, output.index(), "FT.INFO", output.index()).Result()
	if err != nil {
		if !isUnknownIndex(err) {
			return false, err
		}
		if err := s.searchDo(wctx, output.index(), snapshotScannedIndexArgs(&output)...).Err(); err != nil {
			return false, err
		}
		return true, nil
	}
	info := indexInfoOf(reply)
	count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(j.Data)).Result()
	if err != nil {
		return false, err
	}
	if info.failures != 0 {
		return false, errorf(adbc.StatusIO, "background compaction index contains failures")
	}
	if info.indexing || info.percent < 1 {
		return false, nil
	}
	if info.numDocs != count {
		return false, errorf(adbc.StatusIO, "background compaction index row count mismatch")
	}
	if len(info.prefixes) != 1 || info.prefixes[0] != output.prefix() {
		return false, errorf(adbc.StatusIO, "background compaction index prefix mismatch")
	}
	j.Rows = count
	j.Phase = "install"
	j.LastError = ""
	return true, s.checkpointSnapshotCompaction(ctx, j)
}
func (s *store) installSnapshotCompaction(ctx context.Context, j *snapshotCompactionJob) (bool, error) {
	cctx, pin, err := s.acquireCompactionCatalog(ctx)
	if err != nil {
		return false, err
	}
	defer pin.release()
	meta, err := s.getTable(cctx, j.Source.Schema, j.Source.Name)
	if err != nil {
		return false, err
	}
	match, installed := snapshotCompactionPrefix(meta, j)
	if installed || !match {
		return true, s.finishSnapshotCompaction(ctx, j)
	}
	wctx, id, err := s.beginSnapshotWrite(cctx, pin)
	if err != nil {
		return false, err
	}
	defer s.abortSnapshot(pin, id)
	snapshotOf(wctx).maintenance = true
	segments := meta.snapshotSegments()
	tail := segments[len(j.Source.snapshotSegments()):]
	compacted := snapshotSegment{Prefix: j.Prefix, Index: snapshotIndexPrefix(j.Data) + "compacted", Rows: j.Rows}
	if len(tail) == 0 {
		meta.KeyPrefix, meta.IndexName = compacted.Prefix, compacted.Index
		meta.SnapshotRows = &j.Rows
		meta.SnapshotSegments = nil
	} else {
		meta.SnapshotSegments = append([]snapshotSegment{compacted}, tail[:len(tail)-1]...)
	}
	meta.SnapshotOverlay = true
	meta.PrefixGen = 0
	raw, err := json.Marshal(meta)
	if err != nil {
		return false, err
	}
	pipe := s.redis(wctx).Pipeline()
	pipe.Set(wctx, metaKey(meta.Schema, meta.Name), raw, 0)
	if _, err := pipe.Exec(wctx); err != nil {
		return false, err
	}
	if err := s.publishSnapshot(wctx, pin, id); err != nil {
		var ae adbc.Error
		if errors.As(err, &ae) && string(ae.SqlState[:]) == "40001" {
			return false, nil
		}
		return false, err
	}
	return true, s.finishSnapshotCompaction(ctx, j)
}
