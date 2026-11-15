// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type metadataHintLiveObserver struct {
	mu       sync.Mutex
	replies  []string
	gets     int
	physical string
	drop     bool
}

func (h *metadataHintLiveObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *metadataHintLiveObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		err := next(ctx, c)
		args := c.Args()
		hint := len(args) > 1 && ((c.Name() == "evalsha" && args[1] == snapshotMetadataReadHintDigest) || (c.Name() == "eval" && args[1] == snapshotMetadataReadHintLua))
		h.mu.Lock()
		defer h.mu.Unlock()
		if c.Name() == "get" && len(args) == 2 && args[1] == h.physical {
			h.gets++
		}
		if hint && err == nil {
			cmd := c.(onceCmd)
			reply, ok := cmd.Val().([]any)
			if ok && len(reply) > 0 {
				h.replies = append(h.replies, fmt.Sprint(reply[0]))
			}
			if h.drop {
				h.drop = false
				c.SetErr(io.ErrUnexpectedEOF)
				return io.ErrUnexpectedEOF
			}
		}
		return err
	}
}
func (h *metadataHintLiveObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		err := next(ctx, cs)
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, c := range cs {
			args := c.Args()
			if c.Name() == "get" && len(args) == 2 && args[1] == h.physical {
				h.gets++
			}
		}
		return err
	}
}
func (h *metadataHintLiveObserver) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replies = nil
	h.gets = 0
}

func metadataHintLiveFixture(t *testing.T) (*sqlHarness, *goredis.Client, *store, context.Context, *snapshotPin, string, *metadataHintLiveObserver) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE metadata_hint(id BIGINT,label TEXT)")
	h.exec("INSERT INTO metadata_hint VALUES(1,'original')")
	o := &metadataHintLiveObserver{}
	transportCtx, impl := statisticsHookedTransport(t, h, o)
	prior := h.ctx
	h.ctx = transportCtx
	t.Cleanup(func() { h.ctx = prior })
	s := impl.store
	key := metaKey("public", "metadata_hint")
	first, p, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.readSnapshotMetadataString(first, key); err != nil {
		p.release()
		t.Fatal(err)
	}
	p.release()
	hint, ok := s.metadataReadHints.get(strings.TrimPrefix(key, metaPrefix))
	if !ok {
		t.Fatal("successful native descriptor read did not warm address hint")
	}
	o.physical = snapshotWorkspace(hint.value) + strings.TrimPrefix(key, metaPrefix)
	o.reset()
	ctx, p, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.release)
	return h, raw, s, ctx, p, key, o
}
func TestSnapshotMetadataHintLiveFreshReadAndKnownKeys(t *testing.T) {
	h, raw, s, ctx, _, key, o := metadataHintLiveFixture(t)
	hint, _ := s.metadataReadHints.get(strings.TrimPrefix(key, metaPrefix))
	physical := snapshotWorkspace(hint.value) + strings.TrimPrefix(key, metaPrefix)
	expected, err := raw.Get(h.ctx, physical).Result()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := s.readSnapshotMetadataString(ctx, key)
	if err != nil || actual != expected {
		t.Fatal("descriptor bytes changed", err)
	}
	if len(o.replies) != 1 || o.replies[0] != "ready" || o.gets != 0 {
		t.Fatal("cold dependency chain was not combined", o.replies, o.gets)
	}
	actual, err = s.readSnapshotMetadataString(ctx, key)
	if err != nil || actual != expected || o.gets != 1 || len(o.replies) != 1 {
		t.Fatal("same-pin native GET or ACL path changed", err, o.replies, o.gets)
	}
	rows, _ := h.query("SELECT label FROM metadata_hint WHERE __rowid=1")
	if len(rows) != 1 || rows[0] != "original" {
		t.Fatal(rows)
	}
}
func TestSnapshotMetadataHintLiveDDLAndWriterIsolation(t *testing.T) {
	h, _, s, ctx, _, key, o := metadataHintLiveFixture(t)
	if _, err := s.readSnapshotMetadataString(ctx, key); err != nil {
		t.Fatal(err)
	}
	o.reset()
	h.exec("UPDATE metadata_hint SET label='changed' WHERE __rowid=1")
	if len(o.replies) != 0 {
		t.Fatal("managed writer planning used read-only hint", o.replies)
	}
	rows, _ := h.query("SELECT label FROM metadata_hint WHERE __rowid=1")
	if len(rows) != 1 || rows[0] != "changed" {
		t.Fatal("stale descriptor/source after write", rows)
	}
	if len(o.replies) != 1 || o.replies[0] != "miss" {
		t.Fatal("changed owner did not fall back to fresh descriptor", o.replies)
	}
	o.reset()
	h.exec("ALTER TABLE metadata_hint ADD COLUMN more BIGINT")
	if len(o.replies) != 0 {
		t.Fatal("ALTER used read-only hint")
	}
	rows, schema := h.query("SELECT * FROM metadata_hint")
	if len(rows) != 1 || schema.NumFields() != 3 {
		t.Fatal("stale schema after ALTER", rows, schema)
	}
	h.exec("DROP TABLE metadata_hint")
	h.exec("CREATE TABLE metadata_hint(id BIGINT,label TEXT)")
	h.exec("INSERT INTO metadata_hint VALUES(2,'recreated')")
	rows, _ = h.query("SELECT label FROM metadata_hint WHERE __rowid=1")
	if len(rows) != 1 || rows[0] != "recreated" {
		t.Fatal("stale recreated table", rows)
	}
	// The earlier pin continues naming exactly its old immutable descriptor.
	original, err := s.readSnapshotMetadataString(ctx, key)
	if err != nil || !strings.Contains(original, "metadata_hint") {
		t.Fatal("old pinned metadata became unavailable", err)
	}
}
func TestSnapshotMetadataHintLiveRejectsCorruption(t *testing.T) {
	for _, kind := range []string{"root_identity", "root_type", "bucket_count", "bucket_directory", "bucket_refs", "object_refs", "missing_object", "wrongtype_object", "bucket_and_wrongtype_object"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, s, ctx, _, key, _ := metadataHintLiveFixture(t)
			scope := snapshotOf(ctx)
			suffix := strings.TrimPrefix(key, metaPrefix)
			slot := snapshotMetadataSlot(suffix)
			hint, _ := s.metadataReadHints.get(suffix)
			physical := snapshotWorkspace(hint.value) + suffix
			switch kind {
			case "root_identity":
				if err := raw.HSet(h.ctx, snapshotMetadataMap(scope.id), slot, "invalid-owner").Err(); err != nil {
					t.Fatal(err)
				}
			case "root_type":
				if err := raw.Del(h.ctx, snapshotMetadataMap(scope.id)).Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.Set(h.ctx, snapshotMetadataMap(scope.id), "wrong type", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "bucket_count":
				if err := raw.HSet(h.ctx, snapshotMetadataGraph, "n:"+hint.bucket+":"+slot, "999999").Err(); err != nil {
					t.Fatal(err)
				}
			case "bucket_directory":
				if err := raw.ZRem(h.ctx, snapshotMetadataDirectory(hint.bucket, slot), suffix).Err(); err != nil {
					t.Fatal(err)
				}
			case "bucket_refs":
				if err := raw.HSet(h.ctx, snapshotMetadataGraph, "b:"+hint.bucket+":"+slot, 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "object_refs":
				if err := raw.HSet(h.ctx, snapshotMetadataGraph, "v:"+physical, 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "missing_object":
				if err := raw.Del(h.ctx, physical).Err(); err != nil {
					t.Fatal(err)
				}
			case "bucket_and_wrongtype_object":
				if err := raw.HSet(h.ctx, snapshotMetadataGraph, "b:"+hint.bucket+":"+slot, 0).Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.Del(h.ctx, physical).Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.HSet(h.ctx, physical, "unexpected", "hash").Err(); err != nil {
					t.Fatal(err)
				}
			case "wrongtype_object":
				if err := raw.Del(h.ctx, physical).Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.HSet(h.ctx, physical, "unexpected", "hash").Err(); err != nil {
					t.Fatal(err)
				}
			}
			actual, err := s.readSnapshotMetadataString(ctx, key)
			if err == nil || actual != "" {
				t.Fatal("corrupt metadata accepted", kind, err)
			}
			if kind == "bucket_and_wrongtype_object" && !strings.Contains(err.Error(), "bucket is incomplete") {
				t.Fatal("later object error hid bucket-integrity failure", err)
			}
		})
	}
}
func TestSnapshotMetadataHintLiveExactBoundAndNativeFallback(t *testing.T) {
	for _, size := range []int{0, snapshotMetadataHintValueBytes, snapshotMetadataHintValueBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			h, raw, s, ctx, _, key, o := metadataHintLiveFixture(t)
			suffix := strings.TrimPrefix(key, metaPrefix)
			hint, _ := s.metadataReadHints.get(suffix)
			physical := snapshotWorkspace(hint.value) + suffix
			wanted := strings.Repeat("x", size)
			if size > 1 {
				wanted = wanted[:size-1] + string([]byte{0})
			}
			if err := raw.Set(h.ctx, physical, wanted, 0).Err(); err != nil {
				t.Fatal(err)
			}
			actual, err := s.readSnapshotMetadataString(ctx, key)
			if err != nil || actual != wanted {
				t.Fatal("descriptor bytes truncated or converted", size, len(actual), err)
			}
			expectedGets := 0
			if size > snapshotMetadataHintValueBytes {
				expectedGets = 1
			}
			if o.gets != expectedGets || len(o.replies) != 1 {
				t.Fatal("materialization bound/fallback changed", size, o.gets, o.replies)
			}
		})
	}
}
func TestSnapshotMetadataHintLiveLostReplyIsNotReplayed(t *testing.T) {
	_, _, s, ctx, _, key, o := metadataHintLiveFixture(t)
	if _, err := s.readSnapshotMetadataString(ctx, key); err != nil {
		t.Fatal(err)
	}
	scope := snapshotOf(ctx)
	fresh := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: scope.id, pin: scope.pin, store: s})
	o.reset()
	o.mu.Lock()
	o.drop = true
	o.mu.Unlock()
	actual, err := s.readSnapshotMetadataString(fresh, key)
	if err == nil || actual != "" || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatal("lost metadata reply hidden", actual, err)
	}
	if len(o.replies) != 1 || o.gets != 0 {
		t.Fatal("lost reply was replayed or fell back", o.replies, o.gets)
	}
}

func TestSnapshotMetadataHintLiveBoundsBeforeLuaMaterialization(t *testing.T) {
	for _, kind := range []string{"root_bytes", "bucket_proof_bytes", "object_proof_bytes"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, s, ctx, _, key, o := metadataHintLiveFixture(t)
			scope := snapshotOf(ctx)
			suffix := strings.TrimPrefix(key, metaPrefix)
			slot := snapshotMetadataSlot(suffix)
			hint, _ := s.metadataReadHints.get(suffix)
			physical := snapshotWorkspace(hint.value) + suffix
			expected, err := raw.Get(h.ctx, physical).Result()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "root_bytes":
				err = raw.HSet(h.ctx, snapshotMetadataMap(scope.id), "unrelated", strings.Repeat("x", snapshotMetadataHintValueBytes+1)).Err()
			case "bucket_proof_bytes":
				n, e := raw.HLen(h.ctx, snapshotMetadataBucket(hint.bucket, slot)).Result()
				if e != nil {
					t.Fatal(e)
				}
				err = raw.HSet(h.ctx, snapshotMetadataGraph, "n:"+hint.bucket+":"+slot, strings.Repeat("0", 1024)+fmt.Sprint(n)).Err()
			case "object_proof_bytes":
				n, e := raw.HGet(h.ctx, snapshotMetadataGraph, "v:"+physical).Result()
				if e != nil {
					t.Fatal(e)
				}
				err = raw.HSet(h.ctx, snapshotMetadataGraph, "v:"+physical, strings.Repeat("0", 1024)+n).Err()
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := s.readSnapshotMetadataString(ctx, key)
			if err != nil || actual != expected || o.gets != 1 || len(o.replies) != 1 || o.replies[0] != "decline" {
				t.Fatal("bounded helper failed to retain original native handling", kind, err, o.gets, o.replies)
			}
		})
	}
}

func TestSnapshotMetadataHintLiveExactBucketIntegerProof(t *testing.T) {
	for _, kind := range []string{"leading_plus_zeros", "maximum_reference", "reference_overflow", "negative_reference", "fractional_count", "scientific_count", "whitespace_count"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, s, ctx, _, key, o := metadataHintLiveFixture(t)
			suffix := strings.TrimPrefix(key, metaPrefix)
			slot := snapshotMetadataSlot(suffix)
			hint, _ := s.metadataReadHints.get(suffix)
			count, err := raw.HLen(h.ctx, snapshotMetadataBucket(hint.bucket, slot)).Result()
			if err != nil {
				t.Fatal(err)
			}
			n, refs := fmt.Sprint(count), "1"
			valid := false
			switch kind {
			case "leading_plus_zeros":
				n = "+000" + n
				refs = "+0001"
				valid = true
			case "maximum_reference":
				refs = "9223372036854775807"
				valid = true
			case "reference_overflow":
				refs = "9223372036854775808"
			case "negative_reference":
				refs = "-1"
			case "fractional_count":
				n = n + ".0"
			case "scientific_count":
				n = n + "e0"
			case "whitespace_count":
				n = " " + n
			}
			if err := raw.HSet(h.ctx, snapshotMetadataGraph, "n:"+hint.bucket+":"+slot, n, "b:"+hint.bucket+":"+slot, refs).Err(); err != nil {
				t.Fatal(err)
			}
			actual, err := s.readSnapshotMetadataString(ctx, key)
			if valid {
				if err != nil || actual == "" || len(o.replies) != 1 || o.replies[0] != "ready" {
					t.Fatal("valid int64 spelling rejected", kind, err, o.replies)
				}
			} else {
				if err == nil || actual != "" || !strings.Contains(err.Error(), "bucket is incomplete") || len(o.replies) != 1 || o.replies[0] != "bucket" {
					t.Fatal("invalid proof accepted or reached object", kind, err, o.replies)
				}
			}
		})
	}
}

// Software does not expose ACL SETUSER. The owner provisions these principals
// through the Software management API, on the same disposable strict-TLS DB.
func TestSnapshotMetadataHintLiveNativeACL(t *testing.T) {
	for _, denied := range []string{"get", "exists", "memory", "hstrlen", "strlen", "evalsha"} {
		variants := []string{"ordinary"}
		if denied == "get" {
			variants = append(variants, "bucket_corrupt", "object_corrupt")
		}
		if denied == "exists" {
			variants = append(variants, "bucket_corrupt")
		}
		for _, variant := range variants {
			t.Run(denied+"/"+variant, func(t *testing.T) {
				uri := os.Getenv("REDIS_METADATA_HINT_NO_" + strings.ToUpper(denied) + "_URI")
				if uri == "" {
					t.Skip("requires owned Software API principal denying " + denied)
				}
				h, raw, admin, _, pin, key, _ := metadataHintLiveFixture(t)
				opts, err := goredis.ParseURL(uri)
				if err != nil || opts.Addr != raw.Options().Addr || opts.DB != raw.Options().DB || opts.Username == "" || opts.TLSConfig == nil || raw.Options().TLSConfig == nil {
					t.Fatal("private operator endpoint differs from owned strict-TLS fixture")
				}
				opts.TLSConfig = raw.Options().TLSConfig.Clone()
				opts.Protocol = 3
				opts.ContextTimeoutEnabled = true
				client := goredis.NewClient(opts)
				t.Cleanup(func() { _ = client.Close() })
				client.AddHook(snapshotHook{})
				if err := client.Ping(h.ctx).Err(); err != nil {
					t.Fatal("operator authentication control failed")
				}
				suffix := strings.TrimPrefix(key, metaPrefix)
				slot := snapshotMetadataSlot(suffix)
				hint, _ := admin.metadataReadHints.get(suffix)
				physical := snapshotWorkspace(hint.value) + suffix
				expected, err := raw.Get(h.ctx, physical).Result()
				if err != nil {
					t.Fatal(err)
				}
				command := []any{}
				switch denied {
				case "get":
					command = []any{"GET", physical}
				case "exists":
					command = []any{"EXISTS", physical}
				case "memory":
					command = []any{"MEMORY", "USAGE", snapshotMetadataMap(pin.root), "SAMPLES", 0}
				case "hstrlen":
					command = []any{"HSTRLEN", snapshotMetadataGraph, "e:" + physical}
				case "strlen":
					command = []any{"STRLEN", physical}
				case "evalsha":
					command = []any{"EVALSHA", snapshotMetadataReadHintDigest, 0}
				}
				if err := client.Do(h.ctx, command...).Err(); err == nil || !strings.HasPrefix(err.Error(), "NOPERM ") {
					t.Fatal("operator command was not actually denied")
				}
				limited := &store{client: client, snapshots: true}
				limited.metadataReadHints.put(suffix, hint)
				limitedPin := &snapshotPin{ctx: h.ctx, store: limited, root: pin.root, token: pin.token, descriptorHint: true}
				ctx := context.WithValue(h.ctx, snapshotContextKey{}, &snapshotScope{id: pin.root, pin: limitedPin, store: limited})
				// The real admin-held pin protects this root; all limited-principal catalog
				// and lease proofs must still work before testing the descriptor primitive.
				if err := limited.validateSnapshotRead(ctx); err != nil {
					t.Fatal("operator catalog/lease positive control failed", err)
				}
				if variant == "bucket_corrupt" {
					if err := raw.HSet(h.ctx, snapshotMetadataGraph, "b:"+hint.bucket+":"+slot, 0).Err(); err != nil {
						t.Fatal(err)
					}
				}
				if variant == "object_corrupt" {
					if err := raw.HSet(h.ctx, snapshotMetadataGraph, "v:"+physical, 0).Err(); err != nil {
						t.Fatal(err)
					}
				}
				// Compare public classification against the actual original native
				// resolver, not merely a simulated RedisError in a helper unit.
				limited.metadataReadHints = snapshotMetadataReadHints{}
				cold := context.WithValue(h.ctx, snapshotContextKey{}, &snapshotScope{id: pin.root, pin: limitedPin, store: limited})
				_, coldErr := limited.getTableMetadata(cold, "public", "metadata_hint")
				limited.metadataReadHints.put(suffix, hint)
				warm := context.WithValue(h.ctx, snapshotContextKey{}, &snapshotScope{id: pin.root, pin: limitedPin, store: limited})
				_, warmErr := limited.getTableMetadata(warm, "public", "metadata_hint")
				if (coldErr == nil) != (warmErr == nil) || sqlState(coldErr) != sqlState(warmErr) {
					t.Fatal("cache warmness changed public error SQLSTATE", denied, variant, coldErr, warmErr)
				}
				if coldErr != nil && (!isStatus(coldErr, adbc.StatusIO) || !isStatus(warmErr, adbc.StatusIO)) {
					t.Fatal("cache warmness changed public native error status", denied, variant, coldErr, warmErr)
				}
				actual, err := limited.readSnapshotMetadataString(ctx, key)
				want := ""
				if denied == "get" || denied == "exists" {
					want = "NOPERM"
				}
				if variant == "bucket_corrupt" {
					want = "bucket is incomplete"
				}
				if variant == "object_corrupt" {
					want = "object is missing or corrupt"
				}
				if want != "" {
					if err == nil || actual != "" || !strings.Contains(err.Error(), want) {
						t.Fatal("native ACL/proof precedence changed", denied, variant, err)
					}
					if variant != "ordinary" && !isStatus(err, adbc.StatusIO) {
						t.Fatal("proof failure lost precedence to native permission", denied, variant, err)
					}
				} else {
					if err != nil || actual != expected {
						t.Fatal("pre-GET guard refusal did not preserve native read", denied, err)
					}
				}
			})
		}
	}
}
