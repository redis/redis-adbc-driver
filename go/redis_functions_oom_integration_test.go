// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// Requires an owned, disposable database that nothing else uses during the
// test: it is pushed over its memory limit and then restored. Redis Software
// (REDIS_FUNCTION_OOM_TEST_API, the owned fixture's management API) verifies
// the single-database fixture and its effective 4x QPF, then lowers the
// database's memory_size. A standalone Redis Open Source server
// (REDIS_FUNCTION_OOM_TEST_CONFIG=1) lowers maxmemory with CONFIG SET instead.
// Unflagged FCALL is a write that Redis denies under OOM; every no-writes
// entry point must still run with FCALL_RO, and none may write.
func TestRedisReadOnlyFunctionsOverMaxmemory(t *testing.T) {
	api := os.Getenv("REDIS_FUNCTION_OOM_TEST_API")
	config := os.Getenv("REDIS_FUNCTION_OOM_TEST_CONFIG") == "1"
	if api == "" && !config {
		t.Skip("set REDIS_FUNCTION_OOM_TEST_API (owned Redis Software fixture) or REDIS_FUNCTION_OOM_TEST_CONFIG=1 (owned standalone OSS) with REDIS_URI")
	}
	h := newSQLHarness(t) // the driver's Open installs this library version
	raw := h.rawClient()
	ctx := h.ctx
	// Functions must be installed before the limit: FUNCTION LOAD is denied under OOM.
	if err := ensureRedisFunctions(ctx, raw); err != nil {
		t.Fatal(err)
	}
	libraryName, _ := redisFunctionLibrary()
	assertInstalledFunctionFlags(t, ctx, raw, libraryName)

	prefix := "it:oom:" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16] + ":"
	absent := prefix + "{absent}"
	// Before the limit, the server itself proves which entry points are flagged.
	writer := newRedisFunction(unmanagedDeleteLua)
	if err := once(ctx, raw, "FCALL_RO", writer.Name(), 1, absent).Err(); err == nil || !strings.Contains(err.Error(), "write flag") {
		t.Fatal("FCALL_RO accepted an unflagged entry point", err)
	}

	var limit int64
	var setLimit func(int64)
	if api != "" {
		limit, setLimit = lowerSoftwareMemory(t, api)
	} else {
		limit, setLimit = lowerConfigMaxmemory(t, ctx, raw)
	}
	var filled []string
	clear := func() {
		// DEL is permitted over maxmemory.
		for _, key := range filled {
			if err := raw.Del(context.Background(), key).Err(); err != nil {
				t.Errorf("cleanup %s: %v", key, err)
			}
		}
		filled = nil
	}
	t.Cleanup(clear) // runs before the limit is restored
	// A refused SET only proves that used memory plus its own query buffer is
	// over the limit. Fill with ever smaller values until even 16-byte writes
	// are refused, so a small command finds the server itself over maxmemory.
	// Distinct hash tags reach every shard; a run of refusals per size covers
	// every shard of a multi-shard database. The byte budget bounds memory on
	// a server that has not applied the limit yet: fill reports false.
	writes := 0
	fill := func(budget int64) bool {
		var stored int64
		for _, size := range []int{1 << 20, 64 << 10, 1 << 10, 16} {
			value := strings.Repeat("x", size)
			for refusals := 0; refusals < 32; writes++ {
				if stored+int64(size) > budget {
					return false
				}
				key := fmt.Sprintf("%s{%d}", prefix, writes)
				err := raw.Set(ctx, key, value, 0).Err()
				switch {
				case err == nil:
					filled = append(filled, key)
					stored += int64(size)
					refusals = 0
				case strings.HasPrefix(err.Error(), "OOM "):
					refusals++
				default:
					t.Fatal(err)
				}
			}
		}
		return len(filled) > 0
	}
	// Redis Software applies a new memory_size to its shards some time after
	// the management API reports it, and occasionally not until it changes
	// again. Retry with a slightly lower size until the server enforces one.
	for attempt := 1; !fill(2*limit + 64<<20); attempt++ {
		clear()
		if attempt == 6 {
			t.Fatalf("the server did not enforce a %d-byte limit", limit)
		}
		time.Sleep(5 * time.Second)
		limit -= 1 << 20
		setLimit(limit)
	}
	stored := len(filled)
	// At the limit, used memory still drifts around it by buffer sizes. Lower
	// the limit to half the stored data, then proceed once small writes to the
	// control key's shard are refused on consecutive probes, so every check
	// below runs far over the limit.
	half := limit / 2
	for attempt := 1; ; attempt++ {
		setLimit(half)
		refused := 0
		for deadline := time.Now().Add(30 * time.Second); refused < 3 && time.Now().Before(deadline); {
			err := raw.Set(ctx, absent, "1", 0).Err()
			switch {
			case err != nil && strings.HasPrefix(err.Error(), "OOM "):
				refused++
				continue
			case err != nil:
				t.Fatal(err)
			}
			refused = 0
			if err := raw.Del(ctx, absent).Err(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)
		}
		if refused == 3 {
			break
		}
		if attempt == 4 {
			t.Fatalf("the server did not enforce the lowered %d-byte limit", half)
		}
		half -= 1 << 20
	}
	t.Logf("%d stored writes filled the %d-byte limit; now limited to %d bytes", stored, limit, half)

	// Control: an unflagged entry point is a write and is denied under OOM.
	if err := writer.Run(ctx, raw, []string{absent}).Err(); err == nil || !strings.HasPrefix(err.Error(), "OOM ") {
		t.Fatal("unflagged FCALL was not denied over maxmemory", err)
	}
	if err := raw.Set(ctx, absent, "1", 0).Err(); err == nil || !strings.HasPrefix(err.Error(), "OOM ") {
		t.Fatal("server is not over maxmemory", err)
	}

	// The connection probe, through the driver's own initialization path.
	if err := ensureRedisFunctions(ctx, raw); err != nil {
		t.Fatal("connection probe failed over maxmemory", err)
	}
	// The namespace read, through its production caller.
	keys, err := (&store{client: raw}).snapshotNamespaceKeys(ctx, absent)
	if err != nil || len(keys) != 1 || keys[0] != absent {
		t.Fatal("reader namespace read failed over maxmemory", keys, err)
	}
	// Every flagged body runs. Placeholder keys/arguments may make a body
	// reject its layout; that reply still proves Redis executed it.
	for _, body := range redisReadOnlyFunctionBodies {
		fn := newReadOnlyRedisFunction(body)
		if fn.Command() != "FCALL_RO" {
			t.Fatal("read-only entry point is not invoked with FCALL_RO", fn.Name())
		}
		err := once(ctx, raw, append(fn.Args(1), absent, "GET", absent, "0", "0")...).Err()
		// Redis's own refusals: OOM, a write from a no-writes script, an
		// unflagged function under FCALL_RO, or a missing entry point.
		if err != nil && (strings.HasPrefix(err.Error(), "OOM ") || strings.Contains(err.Error(), "not allowed from read-only scripts") ||
			strings.Contains(err.Error(), "write flag") || strings.HasPrefix(err.Error(), "ERR Function not found")) {
			t.Fatalf("read-only entry point %s failed over maxmemory: %v", fn.Name(), err)
		}
	}
	// A specific reply, not just "no OOM": the catalog write-capability probe
	// answers for this administrator.
	capable, err := once(ctx, raw, append(newReadOnlyRedisFunction(snapshotCatalogWriteProbeLua).Args(1), absent)...).Int()
	if err != nil || capable != 1 {
		t.Fatal("capability probe over maxmemory", capable, err)
	}
	if n, err := raw.Exists(ctx, absent).Result(); err != nil || n != 0 {
		t.Fatal("a read-only entry point created a key", n, err)
	}
}

// FUNCTION LIST reports what the server registered, independent of the
// library source the driver generated.
func assertInstalledFunctionFlags(t *testing.T, ctx context.Context, raw goredis.UniversalClient, library string) {
	t.Helper()
	reply, err := raw.Do(ctx, "FUNCTION", "LIST", "LIBRARYNAME", library).Slice()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, lib := range reply {
		entry := functionListEntry(t, lib)
		if entry["library_name"] != library {
			continue
		}
		functions, _ := entry["functions"].([]any)
		for _, f := range functions {
			fn := functionListEntry(t, f)
			name := fmt.Sprint(fn["name"])
			var flags []string
			switch v := fn["flags"].(type) {
			case []any:
				for _, flag := range v {
					flags = append(flags, fmt.Sprint(flag))
				}
			case map[any]struct{}:
				for flag := range v {
					flags = append(flags, fmt.Sprint(flag))
				}
			}
			want := []string(nil)
			if redisReadOnlyFunctionNames[name] {
				want = []string{"no-writes"}
			}
			if strings.Join(flags, ",") != strings.Join(want, ",") {
				t.Fatalf("server registered %s with flags %v, want %v", name, flags, want)
			}
			seen++
		}
	}
	if seen != len(redisReadOnlyFunctionNames)+len(redisFunctionBodies)-len(redisReadOnlyFunctionBodies) {
		t.Fatalf("FUNCTION LIST shows %d entry points of %s", seen, library)
	}
}

func functionListEntry(t *testing.T, v any) map[string]any {
	t.Helper()
	fields, ok := redisReplyFields(v)
	if !ok {
		t.Fatalf("unexpected FUNCTION LIST entry %T", v)
	}
	return fields
}

func lowerConfigMaxmemory(t *testing.T, ctx context.Context, raw goredis.UniversalClient) (int64, func(int64)) {
	t.Helper()
	if _, cluster := raw.(*goredis.ClusterClient); cluster {
		t.Fatal("REDIS_FUNCTION_OOM_TEST_CONFIG requires a standalone server")
	}
	config, err := raw.ConfigGet(ctx, "maxmemory*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if config["maxmemory-policy"] != "noeviction" {
		t.Fatal("the owned server must use maxmemory-policy noeviction, not", config["maxmemory-policy"])
	}
	original := config["maxmemory"]
	info, err := raw.Info(ctx, "memory").Result()
	if err != nil {
		t.Fatal(err)
	}
	used, err := strconv.ParseInt(infoField(info, "used_memory"), 10, 64)
	if err != nil {
		t.Fatal("used_memory", err)
	}
	limit := used + 16<<20
	t.Cleanup(func() {
		if err := raw.ConfigSet(context.Background(), "maxmemory", original).Err(); err != nil {
			t.Errorf("restore maxmemory %s: %v", original, err)
		}
	})
	set := func(bytes int64) {
		t.Helper()
		if err := raw.ConfigSet(ctx, "maxmemory", strconv.FormatInt(bytes, 10)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	set(limit)
	return limit, set
}

// lowerSoftwareMemory verifies the owned single-database fixture, its
// noeviction policy and effective 4x QPF (as software-qpf.sh does), then
// lowers memory_size and restores it afterwards.
func lowerSoftwareMemory(t *testing.T, api string) (int64, func(int64)) {
	t.Helper()
	origin, err := url.Parse(api)
	if err != nil || origin.Scheme != "https" || (origin.Hostname() != "localhost" && origin.Hostname() != "127.0.0.1") {
		t.Fatal("REDIS_FUNCTION_OOM_TEST_API must be the owned local fixture's https management origin")
	}
	user, password := os.Getenv("REDIS_FUNCTION_OOM_TEST_USER"), os.Getenv("REDIS_FUNCTION_OOM_TEST_PASSWORD")
	if user == "" {
		user, password = "admin@example.com", "adbc-software-test"
	}
	// The disposable local fixture's management API uses a self-signed certificate.
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	call := func(method, path string, body, out any) error {
		var payload io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			payload = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, strings.TrimRight(api, "/")+"/v1/"+path, payload)
		if err != nil {
			return err
		}
		req.SetBasicAuth(user, password)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode/100 != 2 {
			return fmt.Errorf("%s %s: HTTP %d: %s", method, path, res.StatusCode, data)
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	}
	type database struct {
		UID        int    `json:"uid"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		MemorySize int64  `json:"memory_size"`
		Shards     int64  `json:"shards_count"`
		Eviction   string `json:"eviction_policy"`
		Sched      string `json:"sched_policy"`
		Conns      int    `json:"conns"`
		QPF        struct {
			Active  bool `json:"active"`
			Scaling int  `json:"scaling_factor"`
		} `json:"query_performance_factor"`
		Search map[string]any `json:"search"`
	}
	var dbs []database
	if err := call("GET", "bdbs", nil, &dbs); err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 1 || dbs[0].Name != "adbc-test" || dbs[0].Eviction != "noeviction" {
		t.Fatal("requires the owned single-database adbc-test fixture with noeviction")
	}
	path := "bdbs/" + strconv.Itoa(dbs[0].UID)
	read := func() database {
		t.Helper()
		var db database
		if err := call("GET", path+"?fields=uid,name,status,memory_size,shards_count,eviction_policy,sched_policy,conns,query_performance_factor,search", nil, &db); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := read()
	if workers, _ := db.Search["search-workers"].(float64); db.Status != "active" || !db.QPF.Active || db.QPF.Scaling != 4 || workers != 6 || db.Sched != "mnp" || db.Conns != 32 {
		t.Fatalf("Redis Software fixture lacks verified effective 4x QPF (6 Search workers, mnp, 32 connections): %+v", db)
	}
	original := db.MemorySize
	limit := db.Shards * 32 << 20
	if limit >= original {
		t.Fatal("fixture memory_size is already at or below the test limit")
	}
	wait := func(size int64) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Minute); ; time.Sleep(time.Second) {
			if db := read(); db.Status == "active" && db.MemorySize == size {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("database did not become active with memory_size %d", size)
			}
		}
	}
	t.Cleanup(func() {
		if err := call("PUT", path, map[string]any{"memory_size": original}, nil); err != nil {
			t.Errorf("restore memory_size %d: %v", original, err)
			return
		}
		wait(original)
	})
	set := func(size int64) {
		t.Helper()
		if err := call("PUT", path, map[string]any{"memory_size": size}, nil); err != nil {
			t.Fatal(err)
		}
		wait(size)
	}
	set(limit)
	return limit, set
}
