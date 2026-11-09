// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// This is private statement state, not detached operation ownership. Existing
// workspace expiry/reference reclamation still applies; no resume API is exposed.
// Complete capture is sealed before output consumes any journal entry.
const snapshotImportCaptureProof = `
local function import_capture_ready(c)
 local function count(n) return type(n)=='number' and n>=0 and n<=9007199254740991 and n%1==0 end
 return type(c)=='table' and c.state=='ready' and type(c.data)=='string' and #c.data==32 and
  not string.find(c.data,'[^0-9a-f]') and count(c.total) and count(c.checked) and count(c.remaining) and
  c.checked==c.total and c.remaining<=c.total and type(c.sequence)=='string'
end
local function import_capture_reservation(c,graph,workspace)
 if not import_capture_ready(c) then return false end
 local private='` + snapshotPrefix + `workspace:'..workspace..':'
 if string.sub(c.sequence,1,#(private..'seq:'))~=private..'seq:' or
  tonumber(redis.call('HGET',graph,'v:'..c.sequence) or '0')<=0 or
  redis.call('HGET',graph,'e:'..c.sequence)~='1' or
  redis.call('TYPE',c.sequence).ok~='string' or
  redis.call('GET',c.sequence)~=string.format('%.0f',c.total) or
  redis.call('ZSCORE',private..'__keys',c.sequence)~='0' then return false end
 return true
end
`

const snapshotImportCaptureLua = snapshotImportCaptureProof + `
local catalog,graph,rows,inputs,output,sequence,metakeys=unpack(KEYS)
local op,workspace,token,data=unpack(ARGV)
local private='` + snapshotPrefix + `workspace:'..workspace..':'
if catalog~='` + snapshotCatalogKey + `' or graph~='` + snapshotMetadataGraph + `' or
 rows~=private..'import:rows:'..workspace or inputs~=private..'import:keys:'..workspace or
 string.sub(sequence,1,#(private..'seq:'))~=private..'seq:' or metakeys~=private..'__keys' or
 #data~=32 or string.find(data,'[^0-9a-f]') or output~='` + snapshotPrefix + `members:'..data then
 return redis.error_reply('invalid import capture identity')
end
local raw=redis.call('HGET',catalog,'w:'..workspace)
if not raw then return redis.error_reply('snapshot write generation was reclaimed') end
local w=cjson.decode(raw)
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if w.state~='staging' or w.token~=token or w.expires<=now then return redis.error_reply('snapshot writer lease lost') end
if w.import_pending then return redis.error_reply('incomplete import batch cannot resume') end
if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
local owned=redis.call('HGET',graph,'d:'..data)
if not owned then return redis.error_reply('import output is unavailable') end
local d=cjson.decode(owned)
if d.owner~=workspace or d.state~='live' then return redis.error_reply('import output is not private') end
for _,key in ipairs({rows,inputs,sequence}) do
 if tonumber(redis.call('HGET',graph,'v:'..key) or '0')<=0 or
  tonumber(redis.call('HGET',graph,'e:'..key) or '-1')~=redis.call('EXISTS',key) then
  return redis.error_reply('import capture metadata is unavailable')
 end
end
local rowtype,inputtype=redis.call('TYPE',rows).ok,redis.call('TYPE',inputs).ok
if (rowtype~='none' and rowtype~='hash') or (inputtype~='none' and inputtype~='zset') then
 return redis.error_reply('invalid import capture journal type')
end
local capture=w.import_capture
if capture and (type(capture)~='table' or capture.data~=data or capture.sequence~=sequence) then return redis.error_reply('import capture identity changed') end
if op=='begin' then
 if capture then return 1 end
 if redis.call('ZCARD',output)~=0 or redis.call('HLEN',rows)~=0 or redis.call('ZCARD',inputs)~=0 then
  return redis.error_reply('import capture must begin before input or output')
 end
 if redis.call('EXISTS',sequence)~=0 then return redis.error_reply('import row IDs must be unallocated') end
 w.import_capture={state='capturing',data=data,sequence=sequence}
elseif op=='complete' then
 if not capture then return redis.error_reply('import capture was not started') end
 if capture.state=='ready' then
  if not import_capture_reservation(capture,graph,workspace) then return redis.error_reply('invalid import capture checkpoint or row ID reservation') end
  return 1
 end
 local total=redis.call('HLEN',rows)
 if capture.state=='capturing' then
  capture.state='checking';capture.total=total;capture.remaining=total;capture.checked=0
  -- Persist closure before validation: a failed or interrupted check must
  -- never reopen input capture. Ordinary abort/expiry still reclaims it.
  redis.call('HSET',catalog,'w:'..workspace,cjson.encode(w))
 elseif capture.state~='checking' or capture.total~=total or capture.remaining~=total then
  return redis.error_reply('import capture changed during completion')
 end
 if total~=redis.call('ZCARD',inputs) or total>9007199254740991 then return redis.error_reply('import capture directory is incomplete') end
 if type(capture.checked)~='number' or capture.checked<0 or capture.checked>total or capture.checked%1~=0 then
  return redis.error_reply('invalid import capture checkpoint')
 end
 local count,bytes=0,0
 for i=1,128 do
  local offset=capture.checked+count
  if offset==total then break end
  -- Fetch one key at a time so a page of wide binary keys cannot allocate
  -- 128 maximum-sized keys in one reply. Published input stays frozen.
  local page=redis.call('ZRANGE',inputs,offset,offset)
  if #page~=1 or #page[1]>67108864 then return redis.error_reply('invalid import capture member') end
  local key=page[1]
  if count>0 and bytes+#key>1048576 then break end
  if redis.call('ZSCORE',inputs,key)~='0' or redis.call('HEXISTS',rows,key)~=1 then
   return redis.error_reply('import capture membership is incomplete')
  end
  local size=redis.call('HSTRLEN',rows,key)
  if size<1 or size>67108864 then return redis.error_reply('invalid import capture payload size') end
  count=count+1;bytes=bytes+#key
 end
 capture.checked=capture.checked+count
 if capture.checked==total then
  if redis.call('EXISTS',sequence)~=0 or redis.call('ZCARD',output)~=0 then return redis.error_reply('import row IDs must be unallocated') end
  local reserved=string.format('%.0f',total)
  if not redis.acl_check_cmd('SET',sequence,reserved) or
     not redis.acl_check_cmd('HSET',graph,'e:'..sequence,1) or
     not redis.acl_check_cmd('ZADD',metakeys,0,sequence) or
     not redis.acl_check_cmd('HSET',catalog,'w:'..workspace,cjson.encode(w)) then
   return redis.error_reply('NOPERM import capture requires row ID reservation')
  end
  -- Reserve the entire immutable ordinal range before admitting output.
  -- The remaining count binds subsequent batches to its unconsumed suffix.
  redis.call('SET',sequence,reserved)
  redis.call('HSET',graph,'e:'..sequence,1)
  redis.call('ZADD',metakeys,0,sequence)
  capture.state='ready'
 end
else return redis.error_reply('invalid import capture operation') end
redis.call('HSET',catalog,'w:'..workspace,cjson.encode(w))
if w.import_capture.state=='ready' or op=='begin' then return 1 end
return 0
`

func (p *snapshotImportSpool) captureControl(ctx context.Context, op string) (bool, error) {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || scope.pin == nil {
		return false, errorf(adbc.StatusInvalidState, "import capture requires a private workspace")
	}
	rows, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(p.rows, metaPrefix), true, "hset")
	if err != nil {
		return false, err
	}
	keys, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(p.keys, metaPrefix), true, "zadd")
	if err != nil {
		return false, err
	}
	sequence, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(seqKey(p.meta.Schema, p.meta.Name), metaPrefix), true, "set")
	if err != nil {
		return false, err
	}
	data := snapshotDataID(p.meta.prefix())
	n, err := once(ctx, p.store.client, "EVAL", snapshotImportCaptureLua, 7,
		snapshotCatalogKey, snapshotMetadataGraph, rows, keys, snapshotMembers(data), sequence, snapshotMetadataKeys(scope.id),
		op, scope.id, scope.pin.token, data).Int()
	if err != nil {
		return false, wrapRedis(err, "import capture transition failed; input was not replayed")
	}
	if n != 0 && n != 1 {
		return false, errorf(adbc.StatusIO, "invalid import capture transition outcome")
	}
	return n == 1, nil
}

func (p *snapshotImportSpool) begin(ctx context.Context) error {
	if p.begun {
		return nil
	}
	if _, err := p.captureControl(ctx, "begin"); err != nil {
		return err
	}
	p.begun = true
	return nil
}

func (p *snapshotImportSpool) complete(ctx context.Context) error {
	if err := p.begin(ctx); err != nil {
		return err
	}
	for {
		done, err := p.captureControl(ctx, "complete")
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

type snapshotImportCaptureState struct {
	State, Data, Sequence     string
	Total, Remaining, Checked int64
}

func (p *snapshotImportSpool) captureOutcome(ctx context.Context) (*snapshotImportCaptureState, error) {
	scope := snapshotOf(ctx)
	if scope == nil || scope.pin == nil {
		return nil, errorf(adbc.StatusInvalidState, "import capture requires a private workspace")
	}
	raw, err := p.store.client.HGet(ctx, snapshotCatalogKey, "w:"+scope.id).Bytes()
	if err != nil {
		return nil, wrapRedis(err, "import capture outcome unavailable")
	}
	var work struct {
		State   string                      `json:"state"`
		Token   string                      `json:"token"`
		Pending string                      `json:"import_pending"`
		Capture *snapshotImportCaptureState `json:"import_capture"`
	}
	if json.Unmarshal(raw, &work) != nil || work.State != "staging" || work.Token != scope.pin.token || work.Pending != "" || work.Capture == nil ||
		work.Capture.State != "ready" || work.Capture.Data != snapshotDataID(p.meta.prefix()) || work.Capture.Total < 0 ||
		work.Capture.Checked != work.Capture.Total || work.Capture.Remaining < 0 || work.Capture.Remaining > work.Capture.Total {
		return nil, errorf(adbc.StatusInvalidState, "import capture is not complete")
	}
	return work.Capture, nil
}

func (p *snapshotImportSpool) ready(ctx context.Context) (int64, error) {
	capture, err := p.captureOutcome(ctx)
	if err != nil {
		return 0, err
	}
	return capture.Remaining, nil
}
