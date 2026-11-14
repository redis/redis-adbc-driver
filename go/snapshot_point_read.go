// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// A single-row managed write often misses several newer segments before finding
// its old image. Read that one HASH and conditionally prove its absence in the
// same request. Do not probe older rows after finding a winner, inspect unused
// fields, or convert stored values/row IDs through Lua numbers. This script has
// no mutation commands and does not replace the caller's pin/ownership checks.
const snapshotPointReadLua = snapshotRowLayoutLua + `
if #KEYS~=2 or #ARGV<4 or #ARGV>260 then return redis.error_reply('invalid snapshot point read') end
local prefix,data=snapshot_row_key(KEYS[1])
local member,expected,owned,mode=ARGV[1],tonumber(ARGV[2]),ARGV[3],ARGV[4]
if not expected or expected<0 or expected>=9007199254740992 or expected%1~=0 or not prefix or KEYS[2]~='` + snapshotPrefix + `members:'..data or
 #member~=20 or not string.match(member,'^%d+$') or member=='00000000000000000000' or member>'09223372036854775807' or
 KEYS[1]~=prefix..string.gsub(member,'^0+','') or (owned~='0' and owned~='1') or
 (mode~='all' and mode~='fields') or (mode=='all' and #ARGV~=4) or (mode=='fields' and #ARGV<5) then
 return redis.error_reply('invalid snapshot point read layout')
end
-- Prefix native command failures inside the script. Only a top-level script
-- permission refusal may select the old command path; an inner data/directory
-- refusal or wrong-type error must remain terminal without another attempt.
local function checked(...)
 local reply=redis.pcall(...)
 if type(reply)=='table' and reply.err then error('snapshot point read native command failed: '..reply.err) end
 return reply
end
local row,present
if mode=='all' then row=checked('HGETALL',KEYS[1]);present=#row>0
else row=checked('HMGET',KEYS[1],unpack(ARGV,5));present=row[1]~=false end
if not present then
 if owned=='0' and checked('ZCARD',KEYS[2])~=expected then
  return redis.error_reply('published snapshot row directory changed')
 end
 if checked('ZSCORE',KEYS[2],member) then
  return redis.error_reply('snapshot directory references a missing row')
 end
 return {0,{}}
end
return {1,row}
`

var snapshotPointReadDigest = goredis.NewScript(snapshotPointReadLua).Hash()

// handled=false is only a local eligibility refusal or a definite top-level
// script ACL refusal. A lost read reply fails the statement; it is not replayed.
func (s *store) fetchSnapshotPoint(ctx context.Context, seg snapshotSegment, key string, fields []string) (row aggRow, handled bool, resultErr error) {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || len(fields) > 254 || seg.Rows < 0 || seg.Rows >= exactDoubleLimit {
		return nil, false, nil
	}
	member := snapshotDirectoryMember(key)
	if member == "" {
		return nil, false, nil
	}
	directory := snapshotMembers(snapshotDataID(seg.Prefix))
	owned := "0"
	if snapshotOwns(ctx, seg.Prefix) {
		owned = "1"
	}
	mode := "all"
	var names []string
	if fields != nil {
		mode = "fields"
		names = append([]string{rowIDField}, fields...)
		names = append(names, snapshotDeletedField)
	}
	args := []any{"EVAL", snapshotPointReadLua, 2, key, directory, member, strconv.FormatInt(seg.Rows, 10), owned, mode}
	bytes := len(key) + len(directory) + len(member) + len(owned) + len(mode) + 20
	for _, name := range names {
		args = append(args, name)
		bytes += len(name)
	}
	if bytes > snapshotWriteBatchBytes {
		return nil, false, nil
	}
	reply, err := s.pointReadScript.runScript(ctx, s.redis(ctx), snapshotPointReadDigest, args).Slice()
	if err != nil {
		// Inner native errors are explicitly prefixed above, never NOPERM here.
		// Preserve custom selector ACLs that allow native HASH/directory commands
		// but cannot declare their keys together in an EVAL envelope.
		if native, ok := err.(interface {
			error
			RedisError()
		}); ok && strings.HasPrefix(native.Error(), "NOPERM ") {
			return nil, false, nil
		}
		return nil, true, wrapRedis(transportContextError(ctx, err), "failed to read snapshot point image; read was not replayed")
	}
	if len(reply) != 2 {
		return nil, true, errorf(adbc.StatusIO, "invalid snapshot point read reply")
	}
	status, ok := reply[0].(int64)
	values, valuesOK := reply[1].([]any)
	if !ok || !valuesOK || status < 0 || status > 1 {
		return nil, true, errorf(adbc.StatusIO, "invalid snapshot point read reply")
	}
	if status == 0 {
		if len(values) != 0 {
			return nil, true, errorf(adbc.StatusIO, "invalid absent snapshot point reply")
		}
		return nil, true, nil
	}
	if fields == nil {
		if len(values) == 0 || len(values)%2 != 0 {
			return nil, true, errorf(adbc.StatusIO, "invalid snapshot point HASH reply")
		}
		row = make(aggRow, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, kOK := values[i].(string)
			value, vOK := values[i+1].(string)
			if !kOK || !vOK {
				return nil, true, errorf(adbc.StatusIO, "invalid snapshot point HASH value")
			}
			row[key] = value
		}
	} else {
		if len(values) != len(names) || values[0] == nil {
			return nil, true, errorf(adbc.StatusIO, "invalid snapshot point fields reply")
		}
		row = make(aggRow, len(names))
		for i, value := range values {
			if value != nil {
				text, ok := value.(string)
				if !ok {
					return nil, true, errorf(adbc.StatusIO, "invalid snapshot point field value %s", fmt.Sprint(i))
				}
				row[names[i]] = text
			}
		}
	}
	return row, true, nil
}
