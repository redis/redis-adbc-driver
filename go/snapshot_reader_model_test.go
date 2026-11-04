// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Staged protocol model only. This file is excluded from the driver binary;
// it cannot activate a format change or reclaim a real driver generation.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adbc-drivers/redis/go/internal/tlstrust"
	goredis "github.com/redis/go-redis/v9"
)

const readerLeaseModelHelpers = `
local base=ARGV[1]
if not string.match(base,'^adbc:{meta}:snapshot:reader%-model:[0-9a-f]+:') then return redis.error_reply('not an isolated reader lease model') end
for _,key in ipairs(KEYS) do if string.sub(key,1,#base)~=base then return redis.error_reply('model key escaped isolated prefix') end end
local function hex(value,n) return type(value)=='string' and #value==n and not string.find(value,'[^0-9a-f]') end
local function pin(raw,now)
 if not raw then return nil end
 local ok,value=pcall(cjson.decode,raw)
 if not ok or type(value)~='table' or not hex(value.root,32) or type(value.expires)~='number' or value.expires%1~=0 or value.expires<=now or value.expires>now+86400000 then return nil end
 return value
end
local function directory(start,count)
 if redis.call('SCARD',KEYS[2])~=count then return false end
 local seen={}
 for i=1,count do
  local key=KEYS[start+i];local ns=string.sub(key,#base+#'reader:'+1)
  if not hex(ns,64) or key~=base..'reader:'..ns or seen[ns] or redis.call('SISMEMBER',KEYS[2],ns)~=1 then return false end
  seen[ns]=true
 end
 return true
end
local function held(start,count,now)
 local total,roots=0,{}
 for i=1,count do
  local fields=redis.call('HGETALL',KEYS[start+i])
  for n=1,#fields,2 do local value=hex(fields[n],32) and pin(fields[n+1],now);if value then total=total+1;roots[value.root]=true end end
 end
 local fields=redis.call('HGETALL',KEYS[1])
 for n=1,#fields,2 do if string.sub(fields[n],1,2)=='p:' then local value=pin(fields[n+1],now);if value then total=total+1;roots[value.root]=true end end end
 return total,roots
end
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if redis.call('HGET',KEYS[1],'format')~='16-model' then return redis.error_reply('not a staged reader catalog') end
`

const readerLeaseModelControl = readerLeaseModelHelpers + `
local op,ns,token,root,other=ARGV[2],ARGV[3],ARGV[4],ARGV[5],ARGV[6]
if not hex(ns,64) or not hex(token,32) or KEYS[3]~=base..'reader:'..ns then return redis.error_reply('invalid reader lease identity') end
local rawCurrent=redis.call('HGET',KEYS[3],token)
local current=pin(rawCurrent,now)
if op=='release' then redis.call('HDEL',KEYS[3],token);return 1 end
if op=='renew' or op=='valid' then
 if redis.call('SISMEMBER',KEYS[2],ns)~=1 or not current or current.root~=root or redis.call('HGET',KEYS[1],'w:'..root)~='committed' then return 0 end
 if op=='renew' then current.expires=now+120000;redis.call('HSET',KEYS[3],token,cjson.encode(current)) end
 return 1
end
if op=='acquire' and rawCurrent then
 if not current or redis.call('SISMEMBER',KEYS[2],ns)~=1 or redis.call('HGET',KEYS[1],'w:'..current.root)~='committed' then return redis.error_reply('existing reader lease is no longer valid') end
 return current.root
end
local count=tonumber(ARGV[7])
if #KEYS~=4+count or not directory(4,count) then return redis.error_reply('reader namespace changed before mutation') end
local total=held(4,count,now)
if total>=128 then return redis.error_reply('reader limit 128 reached') end
local partition=false
if op=='acquire' then root=redis.call('HGET',KEYS[1],'root')
elseif op=='retain' then
 if not current or current.root~=root or not hex(other,32) then return redis.error_reply('producer lease expired') end
 token=other;partition=true
elseif op=='partition' then
 local field=other;if KEYS[4]==KEYS[1] then field='p:'..other end
 local producer=pin(redis.call('HGET',KEYS[4],field),now)
 if not producer or not producer.partition or producer.root~=root then return redis.error_reply('partition capability expired') end
else return redis.error_reply('unknown reader operation') end
if redis.call('HGET',KEYS[1],'w:'..root)~='committed' then return redis.error_reply('root was reclaimed') end
if not redis.acl_check_cmd('HSET',KEYS[3],token,'') or not redis.acl_check_cmd('HDEL',KEYS[3],token) or not redis.acl_check_cmd('SADD',KEYS[2],ns) then return redis.error_reply('reader lease permission unavailable') end
local fields=redis.call('HGETALL',KEYS[3])
for i=1,#fields,2 do if not pin(fields[i+1],now) then redis.call('HDEL',KEYS[3],fields[i]) end end
redis.call('SADD',KEYS[2],ns)
redis.call('HSET',KEYS[3],token,cjson.encode({root=root,expires=now+120000,partition=partition}))
return root
`
const readerLeaseModelCollect = readerLeaseModelHelpers + `
local count=tonumber(ARGV[2]);local root=ARGV[3]
if #KEYS~=3+count or not directory(3,count) then return redis.error_reply('reader namespace changed before mutation') end
local _,roots=held(3,count,now)
if roots[root] or redis.call('HGET',KEYS[1],'root')==root then return 0 end
redis.call('HSET',KEYS[1],'w:'..root,'deleting')
redis.call('DEL',KEYS[3]);return 1
`

type readerLeaseModel struct {
	raw, reader, peer                      goredis.UniversalClient
	ctx                                    context.Context
	base, catalog, names, namespace, other string
}

func readerModelClient(t *testing.T, uri string) *goredis.Client {
	t.Helper()
	opts, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal("invalid private model principal configuration")
	}
	opts.Protocol, opts.DisableIdentity = 3, true
	if opts.TLSConfig != nil {
		opts.TLSConfig.RootCAs, err = tlstrust.Roots()
		if err != nil {
			t.Fatal(err)
		}
	}
	c := goredis.NewClient(opts)
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func readerModelNamespace(uri string) string {
	opts, _ := goredis.ParseURL(uri)
	sum := sha256.Sum256([]byte(opts.Username))
	return hex.EncodeToString(sum[:])
}
func newReaderLeaseModel(t *testing.T) *readerLeaseModel {
	t.Helper()
	uri, reader, peer, prefix := os.Getenv("REDIS_URI"), os.Getenv("REDIS_READER_MODEL_URI"), os.Getenv("REDIS_READER_MODEL_PEER_URI"), os.Getenv("REDIS_READER_MODEL_PREFIX")
	if uri == "" || reader == "" || peer == "" || !strings.HasPrefix(prefix, snapshotPrefix+"reader-model:") {
		t.Skip("requires the owned Software API reader lease model gate")
	}
	m := &readerLeaseModel{raw: readerModelClient(t, uri), reader: readerModelClient(t, reader), peer: readerModelClient(t, peer), ctx: context.Background(), base: prefix + snapshotID() + ":", namespace: readerModelNamespace(reader), other: readerModelNamespace(peer)}
	m.catalog, m.names = m.base+"catalog", m.base+"namespaces"
	if err := m.raw.HSet(m.ctx, m.catalog, "format", "16-model", "root", strings.Repeat("a", 32), "w:"+strings.Repeat("a", 32), "committed", "w:"+strings.Repeat("b", 32), "committed").Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.raw.Set(m.ctx, m.base+"rows:a", "old", 0).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var cursor uint64
		for {
			keys, next, err := m.raw.Scan(m.ctx, cursor, m.base+"*", 100).Result()
			if err != nil {
				t.Error(err)
				return
			}
			if len(keys) > 0 {
				if err := m.raw.Del(m.ctx, keys...).Err(); err != nil {
					t.Error(err)
				}
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
	return m
}
func (m *readerLeaseModel) namespaces(t *testing.T) []string {
	t.Helper()
	names, err := m.raw.SMembers(m.ctx, m.names).Result()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}
func (m *readerLeaseModel) control(t *testing.T, client goredis.UniversalClient, ns, op, token, root, other, group string) *goredis.Cmd {
	t.Helper()
	names := m.namespaces(t)
	keys := []any{m.catalog, m.names, m.base + "reader:" + ns, m.catalog}
	if group != "" {
		keys[3] = group
	}
	for _, name := range names {
		keys = append(keys, m.base+"reader:"+name)
	}
	args := []any{"EVAL", readerLeaseModelControl, len(keys)}
	args = append(args, keys...)
	args = append(args, m.base, op, ns, token, root, other, len(names))
	return once(m.ctx, client, args...)
}
func (m *readerLeaseModel) collect(t *testing.T, names []string) *goredis.Cmd {
	t.Helper()
	keys := []any{m.catalog, m.names, m.base + "rows:a"}
	for _, ns := range names {
		keys = append(keys, m.base+"reader:"+ns)
	}
	args := []any{"EVAL", readerLeaseModelCollect, len(keys)}
	args = append(args, keys...)
	args = append(args, m.base, len(names), strings.Repeat("a", 32))
	return once(m.ctx, m.raw, args...)
}
func modelDenied(t *testing.T, err error) {
	t.Helper()
	var reply goredis.Error
	if !errors.As(err, &reply) {
		t.Fatalf("expected a real Redis ACL denial: %v", err)
	}
	message := strings.TrimPrefix(reply.Error(), "ERR ")
	if !strings.HasPrefix(message, "NOPERM ") && !strings.HasPrefix(message, "ACL failure in script: No permissions") {
		t.Fatalf("unexpected Redis error instead of ACL denial: %v", err)
	}
}

func TestSnapshotReaderLeaseModelIsolationAndRetention(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	token := snapshotID()
	if value, err := m.control(t, m.reader, m.namespace, "acquire", token, "", "", "").Text(); err != nil || value != root {
		t.Fatal("mixed-key read/lease script failed", err)
	}
	// Direct and scripted mutation of definitions, data or another principal's
	// leases remain denied even though EVAL itself has declared RW key access.
	modelDenied(t, m.reader.HSet(m.ctx, m.catalog, "root", "forbidden").Err())
	modelDenied(t, m.reader.Do(m.ctx, "EVAL", "return redis.call('HSET',KEYS[1],'root','forbidden')", 1, m.catalog).Err())
	modelDenied(t, m.reader.Set(m.ctx, m.base+"rows:a", "forbidden", 0).Err())
	modelDenied(t, m.reader.HSet(m.ctx, m.base+"reader:"+m.other, token, "forbidden").Err())
	modelDenied(t, m.reader.SRem(m.ctx, m.names, m.other).Err())
	if err := m.raw.HSet(m.ctx, m.catalog, "root", strings.Repeat("b", 32)).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 0 {
		t.Fatal("collector ignored live reader", n, err)
	}
	if value, err := m.control(t, m.reader, m.namespace, "acquire", token, "", "", "").Text(); err != nil || value != root {
		t.Fatal("same-token acquisition switched generations", err)
	}
	if value, err := m.reader.Get(m.ctx, m.base+"rows:a").Result(); err != nil || value != "old" {
		t.Fatal("pinned data changed", err)
	}
	if n, err := m.control(t, m.reader, m.namespace, "renew", token, root, "", "").Int(); err != nil || n != 1 {
		t.Fatal("reader renewal failed", err)
	}
	if err := m.control(t, m.reader, m.namespace, "release", token, root, "", "").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 1 {
		t.Fatal("released reader blocked reclamation", n, err)
	}
	if m.raw.Exists(m.ctx, m.base+"rows:a").Val() != 0 {
		t.Fatal("released shadow data remained")
	}
}

func TestSnapshotReaderLeaseModelMembershipAndPartitions(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	producer, group, worker := snapshotID(), snapshotID(), snapshotID()
	if err := m.control(t, m.reader, m.namespace, "acquire", producer, "", "", "").Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.control(t, m.reader, m.namespace, "retain", producer, root, group, "").Err(); err != nil {
		t.Fatal(err)
	}
	stale := m.namespaces(t)
	if err := m.raw.HSet(m.ctx, m.catalog, "root", strings.Repeat("b", 32)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.control(t, m.peer, m.other, "partition", worker, root, group, m.base+"reader:"+m.namespace).Err(); err != nil {
		t.Fatal("cross-principal worker acquisition failed", err)
	}
	before, err := m.raw.Dump(m.ctx, m.catalog).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.collect(t, stale).Err(); err == nil || !strings.Contains(err.Error(), "namespace changed before mutation") {
		t.Fatal("stale namespace set did not refuse before mutation", err)
	}
	after, err := m.raw.Dump(m.ctx, m.catalog).Result()
	if err != nil || after != before {
		t.Fatal("membership refusal mutated catalog", err)
	}
	for _, token := range []string{producer, group} {
		if err := m.control(t, m.reader, m.namespace, "release", token, root, "", "").Err(); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 0 {
		t.Fatal("producer release invalidated acquired worker", n, err)
	}
	if err := m.control(t, m.peer, m.other, "release", worker, root, "", "").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 1 {
		t.Fatal("worker release did not allow reclamation", n, err)
	}
}

func TestSnapshotReaderLeaseModelQuotaAndExpiry(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	for i := 0; i < 128; i++ {
		if err := m.control(t, m.reader, m.namespace, "acquire", snapshotID(), "", "", "").Err(); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := m.control(t, m.peer, m.other, "acquire", snapshotID(), "", "", "").Err(); err == nil || !strings.Contains(err.Error(), "limit 128") {
		t.Fatal("global reader quota bypassed", err)
	}
	fields, err := m.raw.HKeys(m.ctx, m.base+"reader:"+m.namespace).Result()
	if err != nil || len(fields) != 128 {
		t.Fatal("quota control mutated leases", err)
	}
	for _, token := range fields {
		value, _ := json.Marshal(map[string]any{"root": root, "expires": 0})
		if err := m.raw.HSet(m.ctx, m.base+"reader:"+m.namespace, token, value).Err(); err != nil {
			t.Fatal(err)
		}
	}
	token := snapshotID()
	if err := m.control(t, m.reader, m.namespace, "acquire", token, "", "", "").Err(); err != nil {
		t.Fatal("expired leases blocked new reader", err)
	}
	if n, err := m.raw.HLen(m.ctx, m.base+"reader:"+m.namespace).Result(); err != nil || n != 1 {
		t.Fatal("expired own leases were not pruned", n, err)
	}
	if err := m.raw.HSet(m.ctx, m.catalog, "root", strings.Repeat("b", 32)).Err(); err != nil {
		t.Fatal(err)
	}
	expired, _ := json.Marshal(map[string]any{"root": root, "expires": time.Now().Add(-time.Hour).UnixMilli()})
	if err := m.raw.HSet(m.ctx, m.base+"reader:"+m.namespace, token, expired).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.control(t, m.reader, m.namespace, "valid", token, root, "", "").Int(); err != nil || n != 0 {
		t.Fatal("expired reader was accepted", n, err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 1 {
		t.Fatal("expired reader blocked reclamation", n, err)
	}
}

func TestSnapshotReaderLeaseModelLegacyProducerAndPreflight(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	group, worker := snapshotID(), snapshotID()
	value, _ := json.Marshal(map[string]any{"root": root, "expires": time.Now().Add(time.Minute).UnixMilli(), "partition": true})
	if err := m.raw.HSet(m.ctx, m.catalog, "p:"+group, value, "root", strings.Repeat("b", 32)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.control(t, m.peer, m.other, "partition", worker, root, group, "").Err(); err != nil {
		t.Fatal("reader could not consume writer-owned capability", err)
	}
	if err := m.raw.HDel(m.ctx, m.catalog, "p:"+group).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 0 {
		t.Fatal("writer release invalidated reader", n, err)
	}
	if err := m.control(t, m.peer, m.other, "release", worker, root, "", "").Err(); err != nil {
		t.Fatal(err)
	}
	// A new unauthorized namespace must fail HSET/HDEL preflight before SADD
	// can register it or any lease record can be changed.
	before, err := m.raw.Dump(m.ctx, m.names).Result()
	if err != nil {
		t.Fatal(err)
	}
	err = m.control(t, m.reader, strings.Repeat("c", 64), "acquire", snapshotID(), "", "", "").Err()
	if err == nil || !strings.Contains(err.Error(), "reader lease permission unavailable") {
		t.Fatal("mutation preflight was bypassed", err)
	}
	after, err := m.raw.Dump(m.ctx, m.names).Result()
	if err != nil || after != before {
		t.Fatal("failed permission preflight partially registered a namespace", err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 1 {
		t.Fatal("released writer/reader capability blocked cleanup", n, err)
	}
}

func TestSnapshotReaderLeaseModelRejectsStaleOrEscapedState(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	token := snapshotID()
	if err := m.control(t, m.reader, m.namespace, "acquire", token, "", "", "").Err(); err != nil {
		t.Fatal(err)
	}
	own := m.base + "reader:" + m.namespace
	before, err := m.raw.Dump(m.ctx, own).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"renew", "valid"} {
		if n, err := m.control(t, m.reader, m.namespace, op, token, strings.Repeat("b", 32), "", "").Int(); err != nil || n != 0 {
			t.Fatal("wrong-root lease was accepted", op, n, err)
		}
	}
	after, err := m.raw.Dump(m.ctx, own).Result()
	if err != nil || after != before {
		t.Fatal("rejected lease check mutated state", err)
	}
	// A malformed explicit key list is rejected before even a shadow mutation.
	escaped := strings.TrimSuffix(m.base, ":") + "-escaped:catalog"
	cmd := once(m.ctx, m.raw, "EVAL", readerLeaseModelControl, 4, escaped, m.names, own, m.catalog, m.base, "acquire", m.namespace, snapshotID(), "", "", 0)
	if err := cmd.Err(); err == nil || !strings.Contains(err.Error(), "escaped isolated prefix") {
		t.Fatal("model prefix fence failed", err)
	}
	if m.raw.Exists(m.ctx, escaped).Val() != 0 {
		t.Fatal("escaped key was created")
	}
	if err := m.raw.HSet(m.ctx, m.catalog, "root", strings.Repeat("b", 32), "w:"+root, "deleting").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.control(t, m.reader, m.namespace, "valid", token, root, "", "").Int(); err != nil || n != 0 {
		t.Fatal("reclaimed manifest was accepted", n, err)
	}
	if err := m.control(t, m.reader, m.namespace, "acquire", token, "", "", "").Err(); err == nil || !strings.Contains(err.Error(), "no longer valid") {
		t.Fatal("reclaimed lease silently moved to a new root", err)
	}
	after, err = m.raw.Dump(m.ctx, own).Result()
	if err != nil || after != before {
		t.Fatal("rejected reacquisition changed old lease", err)
	}
}

type readerModelLostReply struct{ calls atomic.Int64 }

func (*readerModelLostReply) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*readerModelLostReply) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *readerModelLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if err != nil || len(args) < 4 || !strings.EqualFold(fmt.Sprint(args[0]), "EVAL") {
			return err
		}
		count, _ := strconv.Atoi(fmt.Sprint(args[2]))
		if len(args) > 4+count && fmt.Sprint(args[4+count]) == "acquire" {
			if h.calls.Add(1) == 1 {
				return io.ErrUnexpectedEOF
			}
		}
		return err
	}
}
func TestSnapshotReaderLeaseModelLostReplyResolvesWithoutReplay(t *testing.T) {
	m := newReaderLeaseModel(t)
	root := strings.Repeat("a", 32)
	token := snapshotID()
	// This client is not used until its recording hook is installed.
	client := readerModelClient(t, os.Getenv("REDIS_READER_MODEL_URI"))
	observer := &readerModelLostReply{}
	client.AddHook(observer)
	if err := m.control(t, client, m.namespace, "acquire", token, "", "", "").Err(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("late reply failure was not injected", err)
	}
	if observer.calls.Load() != 1 {
		t.Fatal("uncertain acquire was resent", observer.calls.Load())
	}
	if err := m.raw.HSet(m.ctx, m.catalog, "root", strings.Repeat("b", 32)).Err(); err != nil {
		t.Fatal(err)
	}
	value, err := client.HGet(m.ctx, m.base+"reader:"+m.namespace, token).Bytes()
	var lease struct {
		Root    string `json:"root"`
		Expires int64  `json:"expires"`
	}
	if err != nil || json.Unmarshal(value, &lease) != nil || lease.Root != root || lease.Expires <= time.Now().UnixMilli() {
		t.Fatal("read-only outcome lookup lost the original root", err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 0 {
		t.Fatal("collector lost an acquire with an uncertain reply", n, err)
	}
	if observer.calls.Load() != 1 {
		t.Fatal("outcome recovery replayed acquire")
	}
	if err := m.control(t, client, m.namespace, "release", token, root, "", "").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := m.collect(t, m.namespaces(t)).Int(); err != nil || n != 1 {
		t.Fatal("resolved lease did not release", n, err)
	}
}
