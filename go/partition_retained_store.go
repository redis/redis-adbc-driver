// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Namespace ACLs authorize retained jobs; knowing an ID confers no access.
type retainedLimits struct {
	JobBytes   int64 `json:"job_bytes"`
	TotalBytes int64 `json:"total_bytes"`
	ChunkBytes int64 `json:"chunk_bytes"`
	Chunks     int   `json:"chunks"`
	Jobs       int   `json:"jobs"`
}

const maxRetainedChunks = 1024

func defaultRetainedLimits() retainedLimits {
	return retainedLimits{JobBytes: 64 << 20, TotalBytes: 256 << 20, ChunkBytes: 1 << 20, Chunks: 1024, Jobs: 1024}
}

var retainedNamespace = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var retainedIdentity = regexp.MustCompile(`^[0-9a-f]{32}$`)
var retainedDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type retainedStore struct {
	client    goredis.UniversalClient
	namespace string
	prefix    string
	limits    retainedLimits
	lease     time.Duration
}

func (limits retainedLimits) valid() bool {
	return !(limits.ChunkBytes < 1 || limits.ChunkBytes > 16<<20 || limits.JobBytes < limits.ChunkBytes ||
		limits.JobBytes > 1<<30 || limits.TotalBytes < limits.JobBytes || limits.TotalBytes > 8<<30 ||
		limits.Chunks < 1 || limits.Chunks > maxRetainedChunks || limits.Jobs < 1 || limits.Jobs > 1024)
}

func newRetainedStore(client goredis.UniversalClient, namespace string, limits retainedLimits) (*retainedStore, error) {
	if client == nil || !retainedNamespace.MatchString(namespace) || !limits.valid() {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained partition namespace or limits")
	}

	return &retainedStore{client: client, namespace: namespace, prefix: "adbc:{retained:" + namespace + "}:",
		limits: limits, lease: 10 * time.Minute}, nil
}

func retainedRandomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func retainedSHA(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Every key touched by a script is declared and has the same tenant hash tag.
// Scripts are deliberately sent once; callers abort uncertain uploads. No
// transport retry may turn a failed publication into a fresh execution.
func (s *retainedStore) eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	command := []any{"EVAL", script, len(keys)}
	for _, key := range keys {
		command = append(command, key)
	}
	command = append(command, args...)
	result, err := once(ctx, s.client, command...).Result()
	if err != nil {
		return nil, wrapRedis(err, "retained partition state operation failed")
	}
	return result, nil
}

const retainedPruneLua = `
local now = redis.call('TIME')
local ms = now[1]*1000 + math.floor(now[2]/1000)
local expired = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', ms)
for _, id in ipairs(expired) do redis.call('ZREM', KEYS[2], id) end
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', ms)
`

const retainedBeginLua = `
local configuration = redis.call('GET', KEYS[1])
if not configuration and redis.call('EXISTS', KEYS[2], KEYS[3]) > 0 then return redis.error_reply('RETAINED_CONTROL_MISSING') end
if redis.call('ZCARD', KEYS[2]) ~= redis.call('ZCARD', KEYS[3]) then return redis.error_reply('RETAINED_QUOTA_CORRUPT') end
` + retainedPruneLua + `
if configuration and configuration ~= ARGV[1] then return redis.error_reply('RETAINED_CONFIG_MISMATCH') end
if redis.call('EXISTS', KEYS[4], KEYS[5]) ~= 0 then return redis.error_reply('RETAINED_JOB_EXISTS') end
if redis.call('ZCARD', KEYS[3]) >= tonumber(ARGV[6]) then return redis.error_reply('RETAINED_JOB_QUOTA') end
local used = 0
local reservations = redis.call('ZRANGE', KEYS[2], 0, -1, 'WITHSCORES')
for i=2,#reservations,2 do used = used + tonumber(reservations[i]) end
if used + tonumber(ARGV[3]) > tonumber(ARGV[5]) then return redis.error_reply('RETAINED_BYTE_QUOTA') end
redis.call('SET', KEYS[1], ARGV[1], 'NX')
local deadline = ms + tonumber(ARGV[7])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[2])
redis.call('ZADD', KEYS[3], deadline, ARGV[2])
local meta = {version=1, job_id=ARGV[2], state='uploading', nonce=ARGV[4],
  reserved=tonumber(ARGV[3]), bytes=0, chunks=0, deadline=string.format('%.0f',deadline)}
redis.call('SET', KEYS[4], cjson.encode(meta), 'PXAT', deadline)
return deadline
`

const retainedPutLua = `
local raw = redis.call('GET', KEYS[1])
if not raw then return redis.error_reply('RETAINED_UPLOAD_INACTIVE') end
local meta = cjson.decode(raw)
if meta.version ~= 1 or meta.state ~= 'uploading' or meta.nonce ~= ARGV[1] or
   meta.deadline ~= ARGV[4] or meta.reserved ~= tonumber(ARGV[5]) or
   meta.bytes ~= tonumber(ARGV[6]) or meta.job_id ~= ARGV[7] then return redis.error_reply('RETAINED_UPLOAD_INACTIVE') end
if meta.chunks ~= tonumber(ARGV[2]) then return redis.error_reply('RETAINED_CHUNK_ORDER') end
local size = string.len(ARGV[3])
local bytes = meta.bytes + size
if bytes > meta.reserved then return redis.error_reply('RETAINED_JOB_BYTES') end
if redis.call('EXISTS', KEYS[2]) ~= 0 then return redis.error_reply('RETAINED_CHUNK_EXISTS') end
redis.call('SET', KEYS[2], ARGV[3], 'PXAT', meta.deadline)
meta.bytes = bytes
meta.chunks = meta.chunks + 1
redis.call('SET', KEYS[1], cjson.encode(meta), 'PXAT', meta.deadline)
return bytes
`

const retainedCommitLua = `
local raw = redis.call('GET', KEYS[3])
if not raw then return redis.error_reply('RETAINED_UPLOAD_INACTIVE') end
local meta = cjson.decode(raw)
if meta.version ~= 1 or meta.job_id ~= ARGV[1] or meta.state ~= 'uploading' or meta.nonce ~= ARGV[2] or
   meta.deadline ~= ARGV[5] or meta.reserved ~= tonumber(ARGV[6]) or
   meta.bytes ~= tonumber(ARGV[7]) then return redis.error_reply('RETAINED_UPLOAD_INACTIVE') end
if meta.chunks ~= #KEYS-4 then return redis.error_reply('RETAINED_CHUNK_ORDER') end
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) or redis.call('EXISTS', KEYS[4]) ~= 0 then return redis.error_reply('RETAINED_PUBLICATION_STATE') end
local now = redis.call('TIME')
if tonumber(ARGV[4]) <= now[1]*1000 + math.floor(now[2]/1000) then return redis.error_reply('RETAINED_EXPIRED') end
for i=5,#KEYS do
  if redis.call('EXISTS', KEYS[i]) ~= 1 then return redis.error_reply('RETAINED_CHUNK_MISSING') end
end
for i=5,#KEYS do redis.call('PEXPIREAT', KEYS[i], ARGV[4]) end
redis.call('ZADD', KEYS[1], meta.bytes, ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[4], ARGV[1])
meta.state = 'committed'
meta.deadline = ARGV[4]
redis.call('SET', KEYS[3], cjson.encode(meta), 'PXAT', ARGV[4])
redis.call('SET', KEYS[4], ARGV[3], 'PXAT', ARGV[4])
return 1
`

const retainedRevokeLua = `
local raw = redis.call('GET', KEYS[3])
local recorded = redis.call('ZSCORE', KEYS[2], ARGV[1])
local meta
if ARGV[2] == '' then
  if not recorded then
    if raw then return redis.error_reply('RETAINED_EXPIRY_MISSING') end
    return 0
  end
  -- Explicit publisher revocation is authorized by namespace key ACLs. Its
  -- authoritative quota deadline also repairs missing/corrupt job metadata.
  meta = {version=1, job_id=ARGV[1], state='revoked', nonce='', reserved=0, bytes=0, chunks=0}
else
  if not raw then return 0 end
  meta = cjson.decode(raw)
  if meta.version ~= 1 or meta.job_id ~= ARGV[1] then return redis.error_reply('RETAINED_PUBLICATION_STATE') end
  if meta.nonce ~= ARGV[2] then return redis.error_reply('RETAINED_OWNER_MISMATCH') end
  if not recorded then return redis.error_reply('RETAINED_EXPIRY_MISSING') end
end
local deadline = string.format('%.0f',tonumber(recorded))
meta.state = 'revoked'
meta.deadline = deadline
redis.call('SET', KEYS[3], cjson.encode(meta), 'PXAT', deadline)
redis.call('DEL', KEYS[4])
for i=5,#KEYS do redis.call('DEL', KEYS[i]) end
redis.call('ZADD', KEYS[1], 0, ARGV[1])
redis.call('ZADD', KEYS[2], deadline, ARGV[1])
return 1
`

type retainedChunk struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Rows   int64  `json:"rows"`
}

type retainedManifest struct {
	Version   int             `json:"version"`
	Namespace string          `json:"namespace"`
	JobID     string          `json:"job_id"`
	Schema    string          `json:"schema_sha256"`
	Expires   int64           `json:"expires_ms"`
	Bytes     int64           `json:"bytes"`
	Rows      int64           `json:"rows"`
	Chunks    []retainedChunk `json:"chunks"`
}

type retainedReference struct {
	Namespace string `json:"namespace"`
	JobID     string `json:"job_id"`
	Manifest  string `json:"manifest_sha256"`
	Expires   int64  `json:"expires_ms"`
}

type retainedUpload struct {
	store    *retainedStore
	id       string
	nonce    string
	reserved int64
	deadline int64
	bytes    int64
	rows     int64
	chunks   []retainedChunk
	done     bool
}

func (s *retainedStore) jobKey(id, suffix string) string {
	return s.prefix + "job:" + id + ":" + suffix
}
func (s *retainedStore) quotaKeys() []string {
	return []string{s.prefix + "config", s.prefix + "quota-bytes", s.prefix + "quota-expiry"}
}

func (s *retainedStore) serverTime(ctx context.Context) (time.Time, error) {
	if cc, ok := s.client.(*goredis.ClusterClient); ok {
		node, err := cc.MasterForKey(ctx, s.prefix+"config")
		if err != nil {
			return time.Time{}, err
		}
		return node.Time(ctx).Result()
	}
	return s.client.Time(ctx).Result()
}

func (s *retainedStore) begin(ctx context.Context, reserved int64) (*retainedUpload, error) {
	if reserved < 1 || reserved > s.limits.JobBytes || s.lease < time.Millisecond || s.lease > 10*time.Minute {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained partition reservation")
	}
	id, err := retainedRandomID()
	if err != nil {
		return nil, err
	}
	nonce, err := retainedRandomID()
	if err != nil {
		return nil, err
	}
	configuration, _ := json.Marshal(s.limits)
	keys := append(s.quotaKeys(), s.jobKey(id, "meta"), s.jobKey(id, "manifest"))
	result, err := s.eval(ctx, retainedBeginLua, keys, string(configuration), id, reserved, nonce,
		s.limits.TotalBytes, s.limits.Jobs, s.lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	deadline, ok := redisReplyInt64(result)
	if !ok || deadline <= 0 {
		return nil, errorf(adbc.StatusInternal, "invalid retained partition lease response")
	}
	return &retainedUpload{store: s, id: id, nonce: nonce, reserved: reserved, deadline: deadline}, nil
}

func (u *retainedUpload) put(ctx context.Context, payload []byte, rows int64) error {
	if u.done || rows < 0 || rows > 1<<31 || len(payload) == 0 || int64(len(payload)) > u.store.limits.ChunkBytes ||
		int64(len(payload)) > u.reserved-u.bytes || len(u.chunks) >= u.store.limits.Chunks || u.rows > (1<<53)-rows {
		return errorf(adbc.StatusInvalidState, "retained partition upload inactive or resource limit exceeded")
	}
	keys := []string{u.store.jobKey(u.id, "meta"), u.store.jobKey(u.id, fmt.Sprint("chunk:", len(u.chunks)))}
	if _, err := u.store.eval(ctx, retainedPutLua, keys, u.nonce, len(u.chunks), payload,
		u.deadline, u.reserved, u.bytes, u.id); err != nil {
		return err
	}
	u.chunks = append(u.chunks, retainedChunk{SHA256: retainedSHA(payload), Bytes: int64(len(payload)), Rows: rows})
	u.bytes += int64(len(payload))
	u.rows += rows
	return nil
}

func (u *retainedUpload) commit(ctx context.Context, schema string, ttl time.Duration) (retainedReference, error) {
	var ref retainedReference
	if u.done || len(u.chunks) == 0 || !retainedDigest.MatchString(schema) || ttl < time.Second || ttl > 24*time.Hour {
		return ref, errorf(adbc.StatusInvalidArgument, "invalid retained partition publication")
	}
	now, err := u.store.serverTime(ctx)
	if err != nil {
		return ref, wrapRedis(err, "retained partition server clock unavailable")
	}
	m := retainedManifest{Version: 1, Namespace: u.store.namespace, JobID: u.id, Schema: schema,
		Expires: now.Add(ttl).UnixMilli(), Bytes: u.bytes, Rows: u.rows, Chunks: u.chunks}
	data, err := json.Marshal(m)
	if err != nil || len(data) > maxPartitionBytes {
		return ref, errorf(adbc.StatusInvalidState, "retained partition manifest is too large")
	}
	keys := []string{u.store.prefix + "quota-bytes", u.store.prefix + "quota-expiry",
		u.store.jobKey(u.id, "meta"), u.store.jobKey(u.id, "manifest")}
	for index := range u.chunks {
		keys = append(keys, u.store.jobKey(u.id, fmt.Sprint("chunk:", index)))
	}
	if _, err := u.store.eval(ctx, retainedCommitLua, keys, u.id, u.nonce, data, m.Expires,
		u.deadline, u.reserved, u.bytes); err != nil {
		return ref, err
	}
	u.done = true
	return retainedReference{Namespace: m.Namespace, JobID: m.JobID, Manifest: retainedSHA(data), Expires: m.Expires}, nil
}

func (s *retainedStore) validReference(ref retainedReference) bool {
	return ref.Namespace == s.namespace && retainedIdentity.MatchString(ref.JobID) && retainedDigest.MatchString(ref.Manifest) && ref.Expires > 0
}

func (s *retainedStore) checkState(ctx context.Context, ref retainedReference) error {
	if !s.validReference(ref) {
		return errorf(adbc.StatusInvalidArgument, "retained partition namespace or identity mismatch")
	}
	now, err := s.serverTime(ctx)
	if err != nil {
		return wrapRedis(err, "retained partition server clock unavailable")
	}
	if now.UnixMilli() >= ref.Expires {
		return errorf(adbc.StatusNotFound, "retained partition result expired")
	}
	data, err := s.client.GetRange(ctx, s.jobKey(ref.JobID, "meta"), 0, 4096).Bytes()
	if err == nil && len(data) == 0 {
		return errorf(adbc.StatusInvalidState, "retained partition metadata missing or evicted")
	}
	if err != nil {
		return wrapRedis(err, "retained partition state unavailable")
	}
	var meta struct {
		Version  int    `json:"version"`
		JobID    string `json:"job_id"`
		State    string `json:"state"`
		Deadline string `json:"deadline"`
	}
	if len(data) > 4096 || json.Unmarshal(data, &meta) != nil || meta.Version != 1 || meta.JobID != ref.JobID {
		return errorf(adbc.StatusInvalidData, "invalid retained partition state metadata")
	}
	if meta.State == "revoked" || meta.State == "uploading" {
		return errorf(adbc.StatusInvalidState, "retained partition result is %s", meta.State)
	}
	if meta.State != "committed" || meta.Deadline != strconv.FormatInt(ref.Expires, 10) {
		return errorf(adbc.StatusInvalidData, "invalid retained partition publication state")
	}
	return nil
}

func (s *retainedStore) manifest(ctx context.Context, ref retainedReference) (*retainedManifest, error) {
	if err := s.checkState(ctx, ref); err != nil {
		return nil, err
	}
	data, err := s.client.GetRange(ctx, s.jobKey(ref.JobID, "manifest"), 0, maxPartitionBytes).Bytes()
	if err == nil && len(data) == 0 {
		return nil, errorf(adbc.StatusInvalidState, "retained partition manifest missing or evicted")
	}
	if err != nil {
		return nil, wrapRedis(err, "retained partition manifest unavailable")
	}
	if len(data) > maxPartitionBytes || retainedSHA(data) != ref.Manifest {
		return nil, errorf(adbc.StatusInvalidData, "retained partition manifest integrity mismatch")
	}
	var m retainedManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version != 1 || m.Namespace != ref.Namespace || m.JobID != ref.JobID ||
		m.Expires != ref.Expires || !retainedDigest.MatchString(m.Schema) || len(m.Chunks) == 0 || len(m.Chunks) > s.limits.Chunks ||
		m.Bytes < 1 || m.Bytes > s.limits.JobBytes || m.Rows < 0 || m.Rows > 1<<53 {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained partition manifest")
	}
	var bytes, rows int64
	for _, chunk := range m.Chunks {
		if chunk.Bytes < 1 || chunk.Bytes > s.limits.ChunkBytes || !retainedDigest.MatchString(chunk.SHA256) || chunk.Rows < 0 || chunk.Rows > 1<<31 {
			return nil, errorf(adbc.StatusInvalidData, "invalid retained partition chunk metadata")
		}
		bytes += chunk.Bytes
		rows += chunk.Rows
	}
	if bytes != m.Bytes || rows != m.Rows {
		return nil, errorf(adbc.StatusInvalidData, "retained partition manifest totals mismatch")
	}
	return &m, nil
}

func (s *retainedStore) chunk(ctx context.Context, ref retainedReference, m *retainedManifest, index int) ([]byte, error) {
	if m == nil || m.JobID != ref.JobID || m.Namespace != ref.Namespace || index < 0 || index >= len(m.Chunks) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid retained partition chunk index")
	}
	current, err := s.manifest(ctx, ref)
	if err != nil {
		return nil, err
	}
	if index >= len(current.Chunks) {
		return nil, errorf(adbc.StatusInvalidData, "retained partition manifest changed")
	}
	data, err := s.client.GetRange(ctx, s.jobKey(ref.JobID, fmt.Sprint("chunk:", index)), 0, current.Chunks[index].Bytes).Bytes()
	if err == nil && len(data) == 0 {
		return nil, errorf(adbc.StatusInvalidData, "retained partition chunk missing or evicted")
	}
	if err != nil {
		return nil, wrapRedis(err, "retained partition chunk unavailable")
	}
	if int64(len(data)) != current.Chunks[index].Bytes || retainedSHA(data) != current.Chunks[index].SHA256 {
		return nil, errorf(adbc.StatusInvalidData, "retained partition chunk integrity mismatch")
	}
	return data, nil
}

func (s *retainedStore) revoke(ctx context.Context, id, nonce string) error {
	if !retainedIdentity.MatchString(id) {
		return errorf(adbc.StatusInvalidArgument, "invalid retained partition job identity")
	}
	keys := []string{s.prefix + "quota-bytes", s.prefix + "quota-expiry", s.jobKey(id, "meta"), s.jobKey(id, "manifest")}
	// Enumerate the bounded complete key space, not a racy observed chunk count.
	// An upload racing revocation cannot leave a new unaccounted chunk behind.
	for i := range maxRetainedChunks {
		keys = append(keys, s.jobKey(id, fmt.Sprint("chunk:", i)))
	}
	_, err := s.eval(ctx, retainedRevokeLua, keys, id, nonce)
	return err
}

func (u *retainedUpload) abort(ctx context.Context) error {
	if u.done {
		return errorf(adbc.StatusInvalidState, "retained partition upload already committed or aborted")
	}
	if err := u.store.revoke(ctx, u.id, u.nonce); err != nil {
		return err
	}
	u.done = true
	return nil
}
