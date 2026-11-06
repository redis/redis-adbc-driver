// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotReaderDirectory = snapshotPrefix + "readers"
const snapshotReaderPrefix = snapshotPrefix + "reader:"
const snapshotReaderDomains = snapshotPrefix + "reader-domains"

func snapshotReaderNamespace(user string) string {
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:])
}

// Reader HASHes are untrusted: their principals can write arbitrary fields.
// Only bounded, unexpired pins of committed roots participate in retention.
const snapshotReaderLua = `
local function reader_hex(v,n) return type(v)=='string' and #v==n and not string.find(v,'[^0-9a-f]') end
local function reader_pin(raw,now)
 if not raw or #raw>1024 then return nil end
 local ok,p=pcall(cjson.decode,raw)
 if not ok or type(p)~='table' or not reader_hex(p.root,32) or type(p.expires)~='number' or p.expires%1~=0 or p.expires<=now or p.expires>now+86400000 then return nil end
 return p
end
local function reader_committed(catalog,root)
 local raw=redis.call('HGET',catalog,'w:'..root)
 if not raw then return false end
 local ok,w=pcall(cjson.decode,raw)
 return ok and type(w)=='table' and w.state=='committed'
end
local function reader_directory(start,count)
 if type(count)~='number' or count<0 or count>128 or redis.call('SCARD',KEYS[start])~=count then return false end
 local seen={}
 for i=1,count do
  local key=KEYS[start+i]
  local ns=string.sub(key,#'` + snapshotReaderPrefix + `'+1)
  if not reader_hex(ns,64) or key~='` + snapshotReaderPrefix + `'..ns or seen[ns] or redis.call('SISMEMBER',KEYS[start],ns)~=1 then return false end
  if redis.call('HLEN',key)>128 or (redis.call('MEMORY','USAGE',key,'SAMPLES',0) or 0)>262144 then error('reader lease hash exceeds resource limit; administrator reconciliation required') end
  seen[ns]=true
 end
 return KEYS[start]=='` + snapshotReaderDirectory + `'
end
local function reader_held(start,count,now)
 local total,roots=0,{}
 for i=1,count do
  local fields=redis.call('HGETALL',KEYS[start+i])
  for n=1,#fields,2 do
   local p=reader_hex(fields[n],32) and reader_pin(fields[n+1],now)
   if p and reader_committed(KEYS[1],p.root) then total=total+1;roots[p.root]=true end
  end
 end
 return total,roots
end
`

const snapshotReaderControl = snapshotReaderLua + `
local catalog,names,own,producer=unpack(KEYS)
local op,ns,token,id,root,ttl,count=unpack(ARGV)
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
ttl=tonumber(ttl);count=tonumber(count)
if catalog~='` + snapshotCatalogKey + `' or redis.call('HGET',catalog,'format')~='16' then return redis.error_reply('missing or unsupported snapshot catalog') end
if names~='` + snapshotReaderDirectory + `' or not reader_hex(ns,64) or own~='` + snapshotReaderPrefix + `'..ns or not reader_hex(token,32) then return redis.error_reply('invalid reader lease identity') end
if redis.call('HSTRLEN',own,token)>1024 then return redis.error_reply('reader lease value exceeds resource limit') end
local raw=redis.call('HGET',own,token)
local current=reader_pin(raw,now)
if op=='release' or op=='release_partitions' then
 if op=='release' or current and current.partition then redis.call('HDEL',own,token) end
 return 1
end
if op=='valid' or op=='renew' then
 if redis.call('SISMEMBER',names,ns)~=1 or not current or current.root~=root or not reader_committed(catalog,root) then return 0 end
 if op=='renew' then current.expires=now+120000;redis.call('HSET',own,token,cjson.encode(current)) end
 return 1
end
if op=='acquire' and raw then
 if not current or redis.call('SISMEMBER',names,ns)~=1 or not reader_committed(catalog,current.root) then return redis.error_reply('existing reader lease is no longer valid') end
 return current.root
end
if #KEYS~=5+count or not reader_directory(5,count) then return redis.error_reply('reader namespace changed before mutation') end
local total=reader_held(5,count,now)
local writers=redis.call('HGETALL',catalog)
for n=1,#writers,2 do if string.sub(writers[n],1,2)=='p:' and reader_pin(writers[n+1],now) then total=total+1 end end
if redis.call('SISMEMBER',names,ns)==0 then
 if count>=128 then return redis.error_reply('snapshot reader namespace limit (128) reached') end
 if redis.call('HLEN',own)~=0 then return redis.error_reply('unregistered reader lease data; administrator reconciliation required') end
end
if total>=128 then return redis.error_reply('snapshot reader limit (128) reached') end
local partition=false
if op=='acquire' then root=redis.call('HGET',catalog,'root');ttl=120000
elseif op=='retain_partitions' then
 if not current or current.root~=root or not reader_hex(id,32) or ttl<1000 or ttl>86400000 then return redis.error_reply('partition producer snapshot expired') end
 token=id;partition=true
elseif op=='acquire_partition' then
 local field=id
 if producer==catalog then field='p:'..id
 elseif not reader_hex(string.sub(producer,#'` + snapshotReaderPrefix + `'+1),64) or string.sub(producer,1,#'` + snapshotReaderPrefix + `')~='` + snapshotReaderPrefix + `' then return redis.error_reply('invalid partition namespace') end
 local group=reader_pin(redis.call('HGET',producer,field),now)
 if not group or not group.partition or group.root~=root then return redis.error_reply('partition snapshot expired or released') end
 ttl=120000
else return redis.error_reply('unsupported restricted reader operation') end
if not reader_committed(catalog,root) then return redis.error_reply('snapshot root unavailable') end
if not redis.acl_check_cmd('HSET',own,token,'') or not redis.acl_check_cmd('HDEL',own,token) or not redis.acl_check_cmd('SADD',names,ns) then return redis.error_reply('NOPERM reader lease permission unavailable') end
-- All refusals precede mutation. Register before publishing the pin, atomically
-- with the collector's exact directory snapshot validation.
local fields=redis.call('HGETALL',own)
for i=1,#fields,2 do if not reader_pin(fields[i+1],now) then redis.call('HDEL',own,fields[i]) end end
redis.call('SADD',names,ns)
redis.call('HSET',own,token,cjson.encode({root=root,expires=now+ttl,partition=partition}))
if partition then return tostring(now+ttl) end
return root
`

func (s *store) snapshotReaderKeys(ctx context.Context) ([]any, error) {
	return s.snapshotNamespaceKeys(ctx, snapshotReaderDirectory)
}

func (s *store) snapshotNamespaceKeys(ctx context.Context, directory string) ([]any, error) {

	// Atomic read-only size checks precede materializing an untrusted directory.
	// A separate SCARD followed by SMEMBERS could race arbitrary raw SADD calls.
	names, err := once(ctx, s.client, "EVAL", `if redis.call('SCARD',KEYS[1])>128 or (redis.call('MEMORY','USAGE',KEYS[1],'SAMPLES',0) or 0)>262144 then return redis.error_reply('snapshot reader namespace resource limit exceeded; administrator reconciliation required') end return redis.call('SMEMBERS',KEYS[1])`, 1, directory).StringSlice()
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	keys := []any{directory}
	for _, ns := range names {
		if len(ns) != 64 || strings.Trim(ns, "0123456789abcdef") != "" {
			return nil, errorf(adbc.StatusIO, "invalid snapshot reader namespace")
		}
		keys = append(keys, snapshotReaderPrefix+ns)
	}
	return keys, nil
}

// Only the administrator's explicit RBAC configuration registers authorization
// domains. Restricted roles cannot alter this set. Users in one Software role
// share its backend rights and therefore share the same lease trust boundary.
func (s *store) registerRBACReaderDomains(ctx context.Context, manager *rbacManager) error {
	roles := make([]string, 0, len(manager.cfg.Roles))
	for role := range manager.cfg.Roles {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	var domains []any
	for _, role := range roles {
		acl, _, err := manager.readRole(ctx, role)
		if err != nil {
			return err
		}
		domains = append(domains, snapshotReaderNamespace(acl.Domain))
	}
	if len(domains) > 128 {
		return errorf(adbc.StatusInvalidArgument, "RBAC reader domain limit (128) exceeded")
	}
	args := []any{"EVAL", `local n=redis.call('SCARD',KEYS[1]);for _,v in ipairs(ARGV) do if redis.call('SISMEMBER',KEYS[1],v)==0 then n=n+1 end end;if n>128 then return redis.error_reply('RBAC reader domain limit (128) exceeded; administrator reconciliation required') end;return redis.call('SADD',KEYS[1],unpack(ARGV))`, 1, snapshotReaderDomains}
	args = append(args, domains...)
	return wrapRedis(once(ctx, s.client, args...).Err(), "cannot register RBAC reader authorization domains")
}

func (s *store) discoverReaderDomain(ctx context.Context) (string, error) {
	domains, err := s.snapshotNamespaceKeys(ctx, snapshotReaderDomains)
	if err != nil {
		return "", err
	}
	keys := []any{snapshotReaderDirectory}
	keys = append(keys, domains[1:]...)
	args := []any{"EVAL", `local own='';for i=2,#KEYS do if redis.acl_check_cmd('HSET',KEYS[i],'probe','') and redis.acl_check_cmd('HDEL',KEYS[i],'probe') and redis.acl_check_cmd('SADD',KEYS[1],'probe') then if own~='' then return redis.error_reply('multiple reader authorization domains; SQL RBAC requires a single Redis role') end;own=KEYS[i] end end;return own`, len(keys)}
	args = append(args, keys...)
	own, err := once(ctx, s.client, args...).Text()
	if err != nil {
		return "", wrapRedis(err, "cannot inspect snapshot reader authorization domain")
	}
	if own == "" {
		return "", errorf(adbc.StatusUnauthorized, "restricted snapshot reader has no registered role/ACL authorization domain; configure its administrator RBAC binding")
	}
	return strings.TrimPrefix(own, snapshotReaderPrefix), nil
}

// Only a direct native reply from the fixed script proves no mutation ran.
// Transport/wrapped/joined errors can contain the same words after execution.
func snapshotNativeRefusal(err error, message string) bool {
	native, ok := err.(interface {
		error
		RedisError()
	})
	return ok && (native.Error() == message || native.Error() == "ERR "+message)
}

func (s *store) readerControl(ctx context.Context, producer, op, token, id, root string, extra ...string) *goredis.Cmd {
	ttl := "120000"
	if len(extra) > 0 && extra[0] != "" {
		ttl = extra[0]
	}
	if producer == "" {
		producer = snapshotCatalogKey
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err := ctx.Err(); err != nil {
			cmd := goredis.NewCmd(ctx)
			cmd.SetErr(err)
			return cmd
		}
		var directory []any
		var err error
		if op == "valid" || op == "renew" || op == "release" || op == "release_partitions" {
			directory = []any{snapshotReaderDirectory}
		} else {
			directory, err = s.snapshotReaderKeys(ctx)
		}
		if err != nil {
			c := goredis.NewCmd(ctx)
			c.SetErr(err)
			return c
		}
		keys := []any{snapshotCatalogKey, snapshotReaderDirectory, snapshotReaderPrefix + s.readerNamespace, producer}
		keys = append(keys, directory...)
		args := []any{"EVAL", snapshotReaderControl, len(keys)}
		args = append(args, keys...)
		args = append(args, op, s.readerNamespace, token, id, root, ttl, len(directory)-1)
		cmd := once(ctx, s.client, args...)
		if cmd.Err() == nil || !snapshotNativeRefusal(cmd.Err(), "reader namespace changed before mutation") {
			return cmd
		}
	}
	c := goredis.NewCmd(ctx)
	c.SetErr(errorf(adbc.StatusIO, "snapshot reader namespace changed repeatedly; retry the statement"))
	return c
}

// An unknown acquire reply is resolved by reading its original token, never by
// repeating acquisition against a newer root. Native refusals and cancellation
// cannot become successful admissions through this recovery path.
func (s *store) resolveReaderAcquire(ctx context.Context, token, expected string, cause error) (string, bool) {
	var reply goredis.Error
	if ctx.Err() != nil || errors.As(cause, &reply) {
		return "", false
	}
	key := snapshotReaderPrefix + s.readerNamespace
	n, err := s.client.HStrLen(ctx, key, token).Result()
	if err != nil || n < 1 || n > 1024 {
		return "", false
	}
	raw, err := s.client.HGet(ctx, key, token).Bytes()
	if err != nil {
		return "", false
	}
	var pin struct {
		Root string `json:"root"`
	}
	if json.Unmarshal(raw, &pin) != nil || !snapshotMetadataID(pin.Root) || expected != "" && pin.Root != expected {
		return "", false
	}
	valid, err := s.readerControl(ctx, "", "valid", token, "", pin.Root).Int()
	return pin.Root, err == nil && valid == 1
}
