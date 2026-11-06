// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Publication and reader registration both watch the view's metadata key. A
// reader therefore holds exactly the generation whose metadata it planned.
// Retired generations remain registered until every reader releases its lease.
// All coordination keys share the metadata slot; row/index cleanup is separate.
const matviewRetiredKey = metaPrefix + "matview:retired"

func matviewReadersKey(prefix string) string {
	return metaPrefix + "matview:readers:" + escapeKeyPart(prefix)
}

type matviewRead struct {
	lease *lease
	last  int64
}

type matviewReads struct {
	mu     sync.Mutex
	store  *store
	lease  *lease
	tables map[string]*tableMeta
	closed bool
}

func (s *store) pinMatview(ctx context.Context, m *tableMeta) (*tableMeta, error) {
	// A managed statement already pins its immutable catalog and every referenced
	// data generation. Registering the mutable-layout lease would create a second
	// ownership protocol outside that catalog's publication and collection fences.
	if s.snapshots {
		if scope := snapshotOf(ctx); m.MaterializedSQL != "" && scope != nil && scope.pin != nil {
			scope.pin.mu.Lock()
			scope.pin.materialized = true
			scope.pin.mu.Unlock()
		}
		return m, nil
	}
	t, _ := ctx.Value(transportContextKey{}).(*executionTransport)
	if m.MaterializedSQL == "" || t == nil || t.owner != s {
		return m, nil
	}
	r := t.matviews
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errConnClosed
	}
	key := metaKey(m.Schema, m.Name)
	if old := r.tables[m.prefix()]; old != nil {
		if !old.matviewRead.lease.valid() {
			return nil, matviewLeaseExpired()
		}
		m.matviewRead = old.matviewRead
		return m, nil
	}
	if r.lease == nil {
		token, err := randomUUID()
		if err != nil {
			return nil, err
		}
		since := time.Now()
		if err = s.client.Set(ctx, rekeyAliveKey(token.S), "1", rekeyTTL).Err(); err != nil {
			return nil, wrapRedis(err, "register materialized view reader")
		}
		r.lease = s.holdLease(context.WithoutCancel(ctx), token.S, since)
		r.tables = map[string]*tableMeta{}
	}
	for attempt := 0; attempt < 32; attempt++ {
		var pinned *tableMeta
		err := s.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			cur, err := s.getTableWith(ctx, tx, m.Schema, m.Name)
			if err != nil {
				return err
			}
			if cur.MaterializedSQL == "" {
				return stateErrorf("55000", "materialized view changed while registering its reader")
			}
			last, err := tx.Get(ctx, seqKey(cur.Schema, cur.Name)).Int64()
			if err != nil && !errors.Is(err, goredis.Nil) {
				return err
			}
			// Do not register after this process lost its lease. Watching the
			// alive key also prevents a registration racing expiry/recovery.
			if !r.lease.valid() {
				return matviewLeaseExpired()
			}
			alive, err := tx.Exists(ctx, rekeyAliveKey(r.lease.owner)).Result()
			if err != nil {
				return err
			}
			if alive == 0 {
				return matviewLeaseExpired()
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.SAdd(ctx, matviewReadersKey(cur.prefix()), r.lease.owner)
				return nil
			})
			if err == nil {
				cur.matviewRead = &matviewRead{lease: r.lease, last: last}
				pinned = cur
			}
			return err
		}, key, rekeyAliveKey(r.lease.owner))
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		if err != nil {
			return nil, wrapRedis(err, "pin materialized view generation")
		}
		r.tables[pinned.prefix()] = pinned
		return pinned, nil
	}
	return nil, errorf(adbc.StatusIO, "materialized view is being refreshed concurrently; try again")
}

func matviewLeaseExpired() error {
	return stateErrorf("55000", "materialized view reader lease expired; try again")
}

// close runs on the maintenance pool after cancellation, reader Release, stream
// completion, or connection close. Failed cleanup remains recoverable on Open.
func (r *matviewReads) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.lease == nil {
		return
	}
	r.lease.release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.store.client.Del(ctx, rekeyAliveKey(r.lease.owner)).Err()
	for _, m := range r.tables {
		_ = r.store.client.SRem(ctx, matviewReadersKey(m.prefix()), r.lease.owner).Err()
		_ = r.store.collectMatviewGeneration(ctx, m.prefix())
	}
}

func (s *store) rowSequence(ctx context.Context, m *tableMeta) (int64, error) {
	if m.matviewRead != nil {
		if !m.matviewRead.lease.valid() {
			return 0, matviewLeaseExpired()
		}
		return m.matviewRead.last, nil
	}
	return s.redis(ctx).Get(ctx, seqKey(m.Schema, m.Name)).Int64()
}

// No new reader can register a retired generation after publication. Existing
// owners are kept alive by the same renewable leases used for re-key recovery.
func (s *store) collectMatviewGeneration(ctx context.Context, prefix string) error {
	if s.snapshots {
		return nil // managed generations are reclaimed only by the reference graph
	}
	if snapshotDataID(prefix) != "" {
		return errorf(adbc.StatusInvalidState, "managed materialized view generations require snapshot reclamation")
	}
	raw, err := s.redis(ctx).HGet(ctx, matviewRetiredKey, prefix).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	var m tableMeta
	if err = json.Unmarshal([]byte(raw), &m); err != nil {
		return err
	}
	if m.MaterializedSQL == "" || m.Adopted || m.Foreign || m.AppIndex || m.KeyRows || m.prefix() != prefix {
		return errorf(adbc.StatusInvalidData, "invalid retired materialized view generation")
	}
	readers := matviewReadersKey(prefix)
	var cursor uint64
	for {
		owners, next, err := s.redis(ctx).SScan(ctx, readers, cursor, "*", 100).Result()
		if err != nil {
			return err
		}
		for _, owner := range owners {
			alive, err := s.redis(ctx).Exists(ctx, rekeyAliveKey(owner)).Result()
			if err != nil {
				return err
			}
			if alive != 0 {
				return nil
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if err = s.searchDo(ctx, m.index(), "FT.DROPINDEX", m.index(), "DD").Err(); err != nil && !isUnknownIndex(err) {
		return err
	}
	// Multiple collectors can reach here; release the generation exactly once.
	return matviewReleaseScript.Run(ctx, s.redis(ctx), []string{matviewRetiredKey, prefixesKey, indexesKey, releasedKey, readers}, prefix, m.index()).Err()
}

var matviewReleaseScript = goredis.NewScript(`
if redis.call('HDEL', KEYS[1], ARGV[1]) == 1 then
  redis.call('SREM', KEYS[2], ARGV[1])
  redis.call('SREM', KEYS[3], ARGV[2])
  redis.call('HINCRBY', KEYS[4], ARGV[1], 1)
  redis.call('DEL', KEYS[5])
end
return 1
`)

func (s *store) collectMatviews(ctx context.Context) error {
	if s.snapshots {
		return nil
	}
	// Use the same metadata-read permission as pending rename recovery;
	// ordinary connections must not require an additional HSCAN grant.
	entries, err := s.redis(ctx).HGetAll(ctx, matviewRetiredKey).Result()
	if err != nil {
		return err
	}
	for prefix := range entries {
		if err = s.collectMatviewGeneration(ctx, prefix); err != nil {
			return err
		}
	}
	return nil
}
