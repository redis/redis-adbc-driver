// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// Re-keying renamed tables (adbc.redis.rename_rekey).
//
// RENAME TO normally rewrites metadata only, so a table keeps the key prefix
// and index name it was created with: one that dbt builds as
// <name>__dbt_tmp and renames into place keeps that name in its keys. With
// rename_rekey set, RENAME TO also moves the rows to the new name's prefix
// and index (<schema>:<new>: and idx:<schema>:<new>, or ~N if a table has
// had those before: prefixes are never reused, see claimNames). It reads
// and writes every row, in four steps:
//
//  1. Begin. One transaction reserves the new prefix and index name, sets
//     rekey_to in the table's metadata, which refuses writes and other
//     ALTERs, and records the job in adbc:{meta}:rekey, a HASH keyed by the
//     old prefix. The job names an owner, whose lease
//     adbc:{meta}:rekey:alive:<owner> has a TTL that the renaming connection
//     keeps renewing (see lease).
//  2. Copy. The new index is created empty, then every row the old index
//     knows about is copied with DUMP and RESTORE … REPLACE, a cursor page
//     at a time, pipelined. A copy keeps every field (__rowid and dropped
//     columns' fields too), and each command touches one key, so this works
//     on a cluster, where a row's old and new keys are in different hash
//     slots. The old rows stay: until step 3, readers see the whole table
//     under its old name.
//  3. Switch. One transaction moves the metadata to the new name, prefix
//     and index (as a plain RENAME TO does) and marks the job switched.
//  4. Finish. FT.DROPINDEX … DD on the old index deletes the old rows, and
//     one transaction releases the old prefix and index name and removes
//     the job.
//
// Other connections read a table's metadata at the start of each statement
// (nothing is cached between statements), so a statement that overlaps a
// re-key may still use the old names after they were released. Every key
// prefix has a generation, the number of times it was released
// (adbc:{meta}:released, bumped by DROP TABLE, TRUNCATE and step 4), and a
// table's metadata records the generation of its prefix (prefix_gen), so
// that a statement can tell its table's rows moved away (checkKeys). A
// released prefix is never taken again (claimNames), so prefix_gen is 0 for
// tables created since; earlier versions reused prefixes.
//
//   - Writers that read the metadata after step 1 are refused. One that
//     read it just before may still be writing, so the copy waits until
//     rekeyFence after step 1: a statement that wrote within rekeyFence of
//     reading the metadata has been copied, and a slower one checks
//     afterwards, and fails if the prefix is being moved or was released,
//     since its changes may be lost or have gone to another table
//     (checkWritten). Once the prefix is released, it also deletes what it
//     wrote there (discardWritten).
//   - Readers use the old index and rows until the switch and the new ones
//     after it. A statement that read the metadata during the move, or ran
//     for rekeyFence or more, checks afterwards, and fails if the prefix was
//     released or the move has switched (checkReads): what it read may be
//     gone or belong to another table. Others ended before step 4.
//
// If the renaming connection goes away, its lease expires. Then the next
// connection to open, the next statement refused because of rekey_to, or
// the next re-keying rename takes the job over (one transaction makes it
// the owner, and marks the job recovering). Before the switch it rolls the
// rename back: it drops the new index with DD, which deletes the copies,
// releases the new names and clears rekey_to, so the table is unchanged
// under its old name. After the switch it finishes step 4.
//
// A takeover never outlives the call that made it: a connection that opens
// settles every abandoned re-key before Open returns, and a statement before
// it goes on. Otherwise a connection that closed right after opening (as
// dbt's metadata connections do) would leave the job with a lease of its own
// that nobody renews, and every other connection would wait for that to
// expire too. Connections that find a job being settled by another one wait
// for it (up to rekeyTTL), and a takeover that fails deletes its lease key
// at once, so the next connection can try again (see settleAbandoned).
//
// ALTER COLUMN … TYPE rewrites a table's rows to new keys under its own name
// with the same steps and checks (a job marked rewrite, with From = To; see
// alter_type.go): it converts the rows instead of copying them with DUMP,
// and its switch makes the metadata the altered table's.
//
// Closing a connection while a re-key runs on another goroutine stops it
// as a cancel does, before the client closes (see stopRekeys): the move
// stops at its next command, and the rename is rolled back, or finished if
// it has switched. A re-key still running when Close stops waiting gives
// its lease up, so that the next connection takes it over at once.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const (
	rekeyKey     = metaPrefix + "rekey"
	rekeyNextKey = metaPrefix + "rekey:next"
	// releasedKey counts the releases of each key prefix (its generation).
	// Temporary tables are not counted: only their own connection uses them.
	releasedKey = metaPrefix + "released"

	// rekeyFence is how long the copy waits after writes are refused (see
	// above). Statements that take longer pay one round trip for the check
	// afterwards.
	rekeyFence = 100 * time.Millisecond
)

// rekeyTTL is how long a re-key outlives its owner's last lease renewal.
var rekeyTTL = 30 * time.Second

func rekeyAliveKey(owner string) string { return metaPrefix + "rekey:alive:" + owner }

// rekeyJob is a re-key in progress, stored in rekeyKey under OldPrefix.
type rekeyJob struct {
	Schema    string `json:"schema"`
	From      string `json:"from"`
	To        string `json:"to"`
	OldPrefix string `json:"old_prefix"`
	OldIndex  string `json:"old_index"`
	KeyPrefix string `json:"key_prefix"`
	IndexName string `json:"index_name"`
	// The generations of the two prefixes (tableMeta.PrefixGen).
	OldPrefixGen int64  `json:"old_prefix_gen,omitempty"`
	KeyPrefixGen int64  `json:"key_prefix_gen,omitempty"`
	Owner        string `json:"owner"`
	// Switched is set once the table's metadata names the new prefix and
	// index (step 3).
	Switched bool `json:"switched,omitempty"`
	// Recovering is set when a connection has taken the job over from one
	// that went away, to roll it back or finish it.
	Recovering bool `json:"recovering,omitempty"`
	// Rewrite is set for ALTER COLUMN … TYPE, which moves the rows to new
	// keys of the same table (From = To), converted (see alter_type.go).
	Rewrite bool `json:"rewrite,omitempty"`

	// lease is held by the connection running the job.
	lease *lease
}

// errLostRekey means that this connection may no longer own a re-key: it
// could not renew the lease in time, so another one may have taken over.
var errLostRekey = errorf(adbc.StatusIO, "the rename stopped: its lease could not be renewed in time, so another connection may take it over")

// mayAct reports whether the job's connection may take a destructive step
// (drop an index, write rows).
func (j *rekeyJob) mayAct() error {
	if !j.lease.valid() {
		return errLostRekey
	}
	return nil
}

// testHookRekeyStep, when set by a test, runs at two steps of a re-key that
// rekeyTable runs: "copy" before each page of rows is copied, and
// "switched" once the metadata names the new keys. ctx is the move's, which
// Close cancels.
var testHookRekeyStep func(ctx context.Context, step string)

// rekeyTable renames a table and moves its rows and index to the new name's.
func (e *executor) rekeyTable(ctx context.Context, meta *tableMeta, to string) error {
	s := e.store
	run, err := s.trackRekey(ctx)
	if err != nil {
		return err
	}
	defer s.untrackRekey(run)
	// Closing the connection stops the rename as cancelling it does.
	sctx := run.ctx
	// Leftovers of abandoned re-keys (one may hold this table's prefix or
	// the new names).
	_ = s.recoverDead(sctx)
	job, start, err := s.beginRekey(sctx, meta.Schema, meta.Name, to)
	if err != nil {
		if run.closing() {
			return errRekeyClosed
		}
		return err
	}
	s.holdRekey(run, job.lease)
	defer job.lease.release()
	fence := time.Now().Add(rekeyFence)
	// Cancelling the statement stops the move, but not the rollback.
	mctx, cancel := context.WithCancel(job.lease.ctx)
	defer cancel()
	defer context.AfterFunc(sctx, cancel)()
	err = s.moveRows(mctx, job, start, fence)
	var next *tableMeta
	if err == nil {
		next, err = s.switchRekey(mctx, job, start)
	}
	if err == nil && testHookRekeyStep != nil {
		testHookRekeyStep(mctx, "switched")
	}
	bg := context.WithoutCancel(ctx)
	if err != nil {
		switch {
		case run.closing():
			err = errRekeyClosed
		case job.lease.ctx.Err() != nil:
			err = errLostRekey
		case ctx.Err() != nil:
			err = stateErrorf(stateQueryCanceled, "the rename was cancelled")
		}
		// Roll back (or, if the switch did happen, finish). If that fails
		// too, another connection does it once the lease has expired.
		switched, serr := s.settleRekey(job.lease.ctx, job)
		if serr != nil {
			s.client.Del(bg, rekeyAliveKey(job.Owner))
		}
		if !switched {
			return err
		}
	} else if err := s.finishRekey(job.lease.ctx, job); err != nil {
		// The table is renamed; another connection removes the old rows.
		s.client.Del(bg, rekeyAliveKey(job.Owner))
	}
	s.trackTemp(meta.Schema, meta.Name, false)
	s.trackTemp(meta.Schema, to, true)
	if next != nil && len(next.PendingCleanup) > 0 {
		s.startCleanup(meta.Schema, to)
	}
	return nil
}

// beginRekey is step 1. It returns the job, holding its lease, and the
// table's metadata as it was before.
func (s *store) beginRekey(ctx context.Context, schema, from, to string) (*rekeyJob, *tableMeta, error) {
	return s.beginMove(ctx, schema, from, to, nil)
}

// beginMove is step 1 of a re-key, or with rewrite set, of the rewrite of
// ALTER COLUMN … TYPE (see alter_type.go): the table keeps its name (to is
// from), and rewrite checks, in the transaction, that the metadata as it is
// then is the table the change was planned for.
func (s *store) beginMove(ctx context.Context, schema, from, to string, rewrite func(cur *tableMeta) error) (*rekeyJob, *tableMeta, error) {
	what := "the rename"
	next := s.client.Incr
	if rewrite != nil {
		// INCRBY, which every user of the driver may run (see "ACL users"
		// in the README).
		what = "rewriting the table"
		next = func(ctx context.Context, key string) *goredis.IntCmd { return s.redis(ctx).IncrBy(ctx, key, 1) }
	}
	n, err := next(ctx, rekeyNextKey).Result()
	if err != nil {
		return nil, nil, wrapRedis(err, "failed to start "+what)
	}
	owner := strconv.FormatInt(n, 10)
	oldKey, newKey := metaKey(schema, from), metaKey(schema, to)
	for attempt := 0; attempt < 20; attempt++ {
		var job *rekeyJob
		var start tableMeta
		var names tableNames
		// moving is the table's metadata if another re-key is moving its
		// rows, busy its prefix if an earlier re-key of the prefix is left.
		var moving *tableMeta
		var busy string
		sent := time.Now()
		err := s.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			cur, err := s.getTableWith(ctx, tx, schema, from)
			if err != nil {
				return err
			}
			if cur.RekeyTo != "" {
				moving = cur
				return errRekeyBusy
			}
			if rewrite != nil {
				if err := rewrite(cur); err != nil {
					return err
				}
			} else {
				if err := refuseAdopted(cur, "RENAME TO with "+OptionStringRenameRekey+" (which moves the rows to new keys)"); err != nil {
					return err
				}
				if n, err := tx.Exists(ctx, newKey).Result(); err != nil {
					return err
				} else if n > 0 {
					return stateErrorf(stateDuplicateTable, "table %q.%q already exists", displaySchema(schema), to)
				}
			}
			// Pin the physical names, as a plain rename does.
			cur.KeyPrefix, cur.IndexName = cur.prefix(), cur.index()
			if found, err := tx.HExists(ctx, rekeyKey, cur.KeyPrefix).Result(); err != nil {
				return err
			} else if found {
				// A table that was dropped while its rows were being moved
				// (DROP TABLE isn't refused), and then created again by an
				// earlier version of the driver, which reused prefixes, has
				// the same prefix (claimNames never hands one out again).
				busy = cur.KeyPrefix
				return errRekeyBusy
			}
			// Names no table has had, as for a new table (claimNames), so
			// the generation of the new prefix is 0.
			names, err = s.freeNames(ctx, tx, schema, to)
			if err != nil {
				return err
			}
			job = &rekeyJob{Schema: schema, From: from, To: to, OldPrefix: cur.KeyPrefix, OldIndex: cur.IndexName,
				KeyPrefix: names.prefix, IndexName: names.index, OldPrefixGen: cur.PrefixGen, Owner: owner,
				Rewrite: rewrite != nil}
			start = *cur
			cur.RekeyTo, cur.Rewriting = names.prefix, rewrite != nil
			rawMeta, err := marshalMeta(cur)
			if err != nil {
				return err
			}
			rawJob, err := json.Marshal(job)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				reserveNames(ctx, p, names)
				p.HSet(ctx, rekeyKey, job.OldPrefix, rawJob)
				p.Set(ctx, rekeyAliveKey(owner), "1", rekeyTTL)
				p.Set(ctx, oldKey, rawMeta, 0)
				return nil
			})
			return err
		}, oldKey, newKey, prefixesKey, indexesKey, rekeyKey, releasedKey, adoptedKey)
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		if errors.Is(err, errRekeyBusy) {
			// Go on once a re-key abandoned by its connection is settled.
			if moving != nil {
				err = s.checkWritable(ctx, moving)
			} else {
				err = s.earlierRekeyErr(ctx, schema, from, busy)
			}
			if err == nil {
				continue
			}
			return nil, nil, err
		}
		if err != nil {
			return nil, nil, wrapRedis(err, "failed to start "+what)
		}
		s.noteTempNames(names)
		job.lease = s.holdLease(context.WithoutCancel(ctx), owner, sent)
		return job, &start, nil
	}
	return nil, nil, errorf(adbc.StatusIO, "table %q.%q is being changed concurrently; try again", displaySchema(schema), from)
}

// lease is a connection's hold on a re-key. It is renewed every rekeyTTL/4,
// and the connection takes destructive steps only while it is valid: the
// last successful renewal was sent less than rekeyTTL/2 ago, so the lease
// key has not expired and no other connection can have taken the job over,
// with rekeyTTL/2 to spare for commands still in flight.
type lease struct {
	// ctx is cancelled once the lease is no longer valid.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// client renews the lease key, rekeyAliveKey(owner). It is the client
	// the connection had when the lease was taken: one that other timeouts
	// replace stays open until the connection closes.
	client  goredis.UniversalClient
	owner   string
	mu      sync.Mutex
	renewed time.Time
}

// holdLease renews a re-key's lease, last set at since, until release.
func (s *store) holdLease(ctx context.Context, owner string, since time.Time) *lease {
	// Lease renewal and rollback outlive execution cancellation. Keep both on
	// the connection's maintenance pool, even when their context retains the
	// execution's values through WithoutCancel.
	ctx = context.WithValue(ctx, transportContextKey{}, (*executionTransport)(nil))
	l := &lease{client: s.client, owner: owner, renewed: since, done: make(chan struct{})}
	l.ctx, l.cancel = context.WithCancel(ctx)
	go func() {
		defer close(l.done)
		tick := time.NewTicker(rekeyTTL / 4)
		defer tick.Stop()
		for {
			select {
			case <-l.ctx.Done():
				return
			case <-tick.C:
				if !l.renew() {
					l.cancel()
					return
				}
			}
		}
	}()
	return l
}

// renew extends the lease and reports whether it is still valid.
func (l *lease) renew() bool {
	sent := time.Now()
	// XX: an expired lease must stay expired.
	ok, err := l.client.SetXX(l.ctx, rekeyAliveKey(l.owner), "1", rekeyTTL).Result()
	if err == nil && !ok {
		return false
	}
	if err == nil {
		l.mu.Lock()
		l.renewed = sent
		l.mu.Unlock()
	}
	return l.valid()
}

func (l *lease) valid() bool {
	if l == nil || l.ctx.Err() != nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return elapsed(l.renewed) < rekeyTTL/2
}

func (l *lease) release() { l.cancel(); <-l.done }

// ---- re-keys running when the connection closes ----

// rekeyRun is a re-key that this connection runs (rekeyTable) or settles
// for a connection that went away (recoverRekey), as Close sees it.
type rekeyRun struct {
	// ctx is derived from the caller's, and Close cancels it with
	// errClosing. A rename's move stops then, but not its rollback; a
	// takeover doesn't use it.
	ctx  context.Context
	stop context.CancelCauseFunc
	done chan struct{}
	// lease is the job's, once it has one (guarded by store.mu).
	lease *lease
}

var (
	// errClosing is the cause with which Close cancels a re-key's context.
	errClosing = errors.New("the connection is closing")
	// errRekeyClosed is the error of a rename that Close stopped.
	errRekeyClosed = errorf(adbc.StatusInvalidState, "the connection was closed while the table was being renamed (%s); the rename was stopped",
		OptionStringRenameRekey)
)

// closing reports whether Close stopped the re-key.
func (r *rekeyRun) closing() bool { return context.Cause(r.ctx) == errClosing }

// trackRekey registers a re-key that is starting, so that Close can stop it.
// It fails once Close has begun.
func (s *store) trackRekey(ctx context.Context) (*rekeyRun, error) {
	r := &rekeyRun{done: make(chan struct{})}
	r.ctx, r.stop = context.WithCancelCause(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		r.stop(nil)
		return nil, errConnClosed
	}
	if s.rekeys == nil {
		s.rekeys = map[*rekeyRun]struct{}{}
	}
	s.rekeys[r] = struct{}{}
	return r, nil
}

// holdRekey records the lease a re-key holds.
func (s *store) holdRekey(r *rekeyRun, l *lease) {
	s.mu.Lock()
	r.lease = l
	s.mu.Unlock()
}

// untrackRekey ends a re-key, once its lease is released.
func (s *store) untrackRekey(r *rekeyRun) {
	s.mu.Lock()
	delete(s.rekeys, r)
	s.mu.Unlock()
	r.stop(nil)
	close(r.done)
}

func (s *store) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// setClosed marks the connection closed, so that no re-key starts on it any
// more, and reports whether it was open.
func (s *store) setClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	was := s.closed
	s.closed = true
	return !was
}

// stopRekeys runs when the connection closes (after setClosed), before its
// client does. It stops the re-keys that statements run on other
// goroutines: a rename's move stops at its next command and is rolled back
// (or finished, if it has switched) as when the statement is cancelled; a
// takeover finishes its rollback. It waits for them up to rekeyTTL/2 (their
// leases are renewed meanwhile). A re-key that is still running then stops
// renewing its lease, and its lease key is deleted, so that the next
// connection takes it over at once rather than after rekeyTTL; it takes no
// further destructive step (mayAct). One stuck before it holds its lease
// (beginRekey, takeOver) can't be given up: its lease expires.
func (s *store) stopRekeys(ctx context.Context) {
	s.mu.Lock()
	runs := make([]*rekeyRun, 0, len(s.rekeys))
	for r := range s.rekeys {
		runs = append(runs, r)
	}
	s.mu.Unlock()
	if len(runs) == 0 {
		return
	}
	for _, r := range runs {
		r.stop(errClosing)
	}
	timeout := time.NewTimer(rekeyTTL / 2)
	defer timeout.Stop()
wait:
	for _, r := range runs {
		select {
		case <-r.done:
		case <-timeout.C:
			break wait
		}
	}
	for _, r := range runs {
		select {
		case <-r.done:
			continue
		default:
		}
		s.mu.Lock()
		l := r.lease
		s.mu.Unlock()
		if l != nil {
			// Not release: a renewal under way may wait for its reply.
			l.cancel()
			s.client.Del(context.WithoutCancel(ctx), rekeyAliveKey(l.owner))
		}
	}
}

// moveRows is step 2: it creates the new index and, once the writes that
// may have missed rekey_to are done (at fence), copies every row.
func (s *store) moveRows(ctx context.Context, job *rekeyJob, start *tableMeta, fence time.Time) error {
	target := *start
	target.KeyPrefix, target.IndexName = job.KeyPrefix, job.IndexName
	if err := job.mayAct(); err != nil {
		return err
	}
	// The index name was free in the registry, so an index with that name is
	// left over from an interrupted CREATE/DROP and can be discarded
	// (freeNames skips names whose index isn't one, see foreignIndex).
	_ = s.searchDo(ctx, job.IndexName, "FT.DROPINDEX", job.IndexName, "DD").Err()
	if err := s.searchDo(ctx, job.IndexName, indexCreateArgs(&target)...).Err(); err != nil {
		return wrapRedis(err, "failed to create the new search index")
	}
	select {
	case <-ctx.Done():
		return wrapRedis(ctx.Err(), "rename stopped")
	case <-time.After(time.Until(fence)):
	}
	node, err := s.search(ctx, job.OldIndex)
	if err != nil {
		return err
	}
	reply, err := node.Do(ctx, "FT.AGGREGATE", job.OldIndex, "*", "LOAD", 1, "@__key",
		"TIMEOUT", 0, "WITHCURSOR", "COUNT", cursorCount, "DIALECT", 2).Result()
	if err != nil {
		return wrapRedis(err, "rename: FT.AGGREGATE failed")
	}
	for {
		rows, cursor, err := parseCursorReply(reply)
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(rows))
		for _, r := range rows {
			if k, ok := r["__key"]; ok && strings.HasPrefix(k, job.OldPrefix) {
				keys = append(keys, k)
			}
		}
		if testHookRekeyStep != nil {
			testHookRekeyStep(ctx, "copy")
		}
		if err := s.copyRows(ctx, job, keys); err != nil {
			if cursor != 0 {
				_ = node.Do(ctx, "FT.CURSOR", "DEL", job.OldIndex, cursor).Err()
			}
			return err
		}
		if cursor == 0 {
			return nil
		}
		if reply, err = node.Do(ctx, "FT.CURSOR", "READ", job.OldIndex, cursor, "COUNT", cursorCount).Result(); err != nil {
			return wrapRedis(err, "rename: FT.CURSOR READ failed")
		}
	}
}

// copyRows copies row HASHes to the new prefix with pipelined DUMP and
// RESTORE … REPLACE (so a copy left over from an earlier attempt is
// overwritten).
func (s *store) copyRows(ctx context.Context, job *rekeyJob, keys []string) error {
	for start := 0; start < len(keys); start += pipelineChunk {
		chunk := keys[start:min(start+pipelineChunk, len(keys))]
		pipe := s.redis(ctx).Pipeline()
		dumps := make([]*goredis.StringCmd, len(chunk))
		for i, k := range chunk {
			dumps[i] = pipe.Dump(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return wrapRedis(err, "rename: DUMP failed")
		}
		pipe = s.redis(ctx).Pipeline()
		for i, k := range chunk {
			payload, err := dumps[i].Result()
			if errors.Is(err, goredis.Nil) {
				continue // deleted after the index listed it
			}
			if err != nil {
				return wrapRedis(err, "rename: DUMP failed")
			}
			pipe.RestoreReplace(ctx, job.KeyPrefix+strings.TrimPrefix(k, job.OldPrefix), 0, payload)
		}
		if pipe.Len() == 0 {
			continue
		}
		if err := job.mayAct(); err != nil {
			return err
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return wrapRedis(err, "rename: RESTORE failed")
		}
	}
	return nil
}

// switchRekey is step 3. It returns the table's new metadata.
func (s *store) switchRekey(ctx context.Context, job *rekeyJob, start *tableMeta) (*tableMeta, error) {
	schema := job.Schema
	oldKey, newKey := metaKey(schema, job.From), metaKey(schema, job.To)
	oldSeq, newSeq := seqKey(schema, job.From), seqKey(schema, job.To)
	// The attributes the new index has.
	indexed := map[string]bool{}
	for _, c := range start.Columns {
		if c.Indexed {
			indexed[c.field()] = true
		}
	}
	for attempt := 0; attempt < 20; attempt++ {
		var next tableMeta
		err := s.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			if _, err := ownRekey(ctx, tx, job); err != nil {
				return err
			}
			cur, err := s.getTableWith(ctx, tx, schema, job.From)
			if err != nil {
				return err
			}
			if cur.RekeyTo != job.KeyPrefix {
				return errorf(adbc.StatusIO, "table %q.%q changed during the rename; try again", displaySchema(schema), job.From)
			}
			if n, err := tx.Exists(ctx, newKey).Result(); err != nil {
				return err
			} else if n > 0 {
				return stateErrorf(stateDuplicateTable, "table %q.%q already exists", displaySchema(schema), job.To)
			}
			if n, err := tx.Exists(ctx, viewKey(schema, job.To)).Result(); err != nil {
				return err
			} else if n > 0 {
				return stateErrorf(stateDuplicateTable, "%q.%q already exists as a view", displaySchema(schema), job.To)
			}
			// An ADD COLUMN that read the metadata before step 1 may have
			// added an attribute to the old index since.
			for _, c := range cur.Columns {
				if c.Indexed && !indexed[c.field()] {
					args := append([]any{"FT.ALTER", job.IndexName, "SCHEMA", "ADD"}, indexAttrArgs(c)...)
					if err := s.searchDo(ctx, job.IndexName, args...).Err(); err != nil {
						return wrapRedis(err, "failed to add a column to the new search index")
					}
					indexed[c.field()] = true
				}
			}
			seq, err := tx.Get(ctx, oldSeq).Result()
			if err != nil && err != goredis.Nil {
				return err
			}
			pending, err := tx.SIsMember(ctx, cleanupKey, cleanupMember(schema, job.From)).Result()
			if err != nil {
				return err
			}
			next = *cur
			next.Name, next.KeyPrefix, next.IndexName, next.RekeyTo = job.To, job.KeyPrefix, job.IndexName, ""
			next.PrefixGen = job.KeyPrefixGen
			// The copies may hold the fields of columns dropped before or
			// during the move (a cleanup may have finished on the old rows
			// only), so they are cleaned up again.
			next.PendingCleanup = slices.Clone(cur.PendingCleanup)
			for _, f := range append(slices.Clone(start.PendingCleanup), cur.RetiredFields...) {
				dropped := slices.Contains(start.PendingCleanup, f) || !slices.Contains(start.RetiredFields, f)
				if dropped && !slices.Contains(next.PendingCleanup, f) {
					next.PendingCleanup = append(next.PendingCleanup, f)
				}
			}
			raw, err := marshalMeta(&next)
			if err != nil {
				return err
			}
			switched := *job
			switched.Switched = true
			rawJob, err := json.Marshal(&switched)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				p.Set(ctx, newKey, raw, 0)
				p.Del(ctx, oldKey, oldSeq)
				if seq != "" {
					p.Set(ctx, newSeq, seq, 0)
				}
				p.SRem(ctx, tablesKey(schema), job.From)
				p.SAdd(ctx, tablesKey(schema), job.To)
				if pending {
					p.SRem(ctx, cleanupKey, cleanupMember(schema, job.From))
				}
				if len(next.PendingCleanup) > 0 {
					p.SAdd(ctx, cleanupKey, cleanupMember(schema, job.To))
				}
				p.HSet(ctx, rekeyKey, job.OldPrefix, rawJob)
				return nil
			})
			return err
		}, oldKey, newKey, oldSeq, rekeyKey, viewKey(schema, job.To))
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		if err != nil {
			return nil, wrapRedis(err, "failed to rename table")
		}
		job.Switched = true
		return &next, nil
	}
	return nil, errorf(adbc.StatusIO, "table %q.%q is being changed concurrently; try again", displaySchema(schema), job.From)
}

// finishRekey is step 4.
func (s *store) finishRekey(ctx context.Context, job *rekeyJob) error {
	if err := job.mayAct(); err != nil {
		return err
	}
	// DD deletes every document the old index knows about: the old rows.
	if err := s.searchDo(ctx, job.OldIndex, "FT.DROPINDEX", job.OldIndex, "DD").Err(); err != nil &&
		!isUnknownIndex(err) {
		return wrapRedis(err, "failed to drop the old search index")
	}
	return s.endRekey(ctx, job)
}

// rollbackRekey undoes steps 1 and 2.
func (s *store) rollbackRekey(ctx context.Context, job *rekeyJob) error {
	if err := job.mayAct(); err != nil {
		return err
	}
	// DD deletes the copies.
	if err := s.searchDo(ctx, job.IndexName, "FT.DROPINDEX", job.IndexName, "DD").Err(); err != nil &&
		!isUnknownIndex(err) {
		return wrapRedis(err, "failed to drop the new search index")
	}
	return s.endRekey(ctx, job)
}

// settleRekey finishes a job that has switched and rolls back one that
// hasn't, as recorded (an error from the switch transaction doesn't say
// whether it ran). It reports whether the job had switched.
func (s *store) settleRekey(ctx context.Context, job *rekeyJob) (bool, error) {
	raw, err := s.redis(ctx).HGet(ctx, rekeyKey, job.OldPrefix).Result()
	if err != nil {
		return false, wrapRedis(err, "failed to read the rename's state")
	}
	var cur rekeyJob
	if err := json.Unmarshal([]byte(raw), &cur); err != nil || cur.Owner != job.Owner {
		return false, errLostRekey
	}
	cur.lease = job.lease
	if cur.Switched {
		return true, s.finishRekey(ctx, &cur)
	}
	return false, s.rollbackRekey(ctx, &cur)
}

// endRekey removes a job, and releases the prefix and index name it no
// longer needs: the old ones once it has switched, the new ones otherwise
// (then it also clears rekey_to).
func (s *store) endRekey(ctx context.Context, job *rekeyJob) error {
	prefix, index := job.KeyPrefix, job.IndexName
	if job.Switched {
		prefix, index = job.OldPrefix, job.OldIndex
	}
	oldKey := metaKey(job.Schema, job.From)
	for attempt := 0; attempt < 20; attempt++ {
		err := s.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			if _, err := ownRekey(ctx, tx, job); err != nil {
				return err
			}
			var raw []byte
			if !job.Switched {
				cur, err := s.getTableWith(ctx, tx, job.Schema, job.From)
				var ae adbc.Error
				switch {
				case err == nil && cur.RekeyTo == job.KeyPrefix:
					cur.RekeyTo, cur.Rewriting = "", false
					if raw, err = marshalMeta(cur); err != nil {
						return err
					}
				case err != nil && !(asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound):
					return err
				}
			}
			_, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				if raw != nil {
					p.Set(ctx, oldKey, raw, 0)
				}
				p.SRem(ctx, prefixesKey, prefix)
				p.SRem(ctx, indexesKey, index)
				if !isTempSchema(job.Schema) {
					p.HIncrBy(ctx, releasedKey, prefix, 1)
				}
				p.HDel(ctx, rekeyKey, job.OldPrefix)
				p.Del(ctx, rekeyAliveKey(job.Owner))
				return nil
			})
			return err
		}, rekeyKey, oldKey)
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		return wrapRedis(err, "failed to finish the rename")
	}
	return errorf(adbc.StatusIO, "table %q.%q is being changed concurrently; try again", displaySchema(job.Schema), job.From)
}

// ownRekey reads a job inside a transaction and checks that it still has
// the given owner.
func ownRekey(ctx context.Context, tx *goredis.Tx, job *rekeyJob) (*rekeyJob, error) {
	raw, err := tx.HGet(ctx, rekeyKey, job.OldPrefix).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, errLostRekey
	}
	if err != nil {
		return nil, err
	}
	var cur rekeyJob
	if err := json.Unmarshal([]byte(raw), &cur); err != nil || cur.Owner != job.Owner {
		return nil, errLostRekey
	}
	return &cur, nil
}

// ---- recovery ----

var (
	// errRekeyBusy stops beginRekey's transaction when a re-key is in the
	// way; beginRekey then settles it or says why it can't.
	errRekeyBusy = errors.New("a rename is in the way")
	// errRekeyOwnerAlive means that a job's owner renewed its lease after
	// all.
	errRekeyOwnerAlive = errors.New("the rename's owner is alive")
)

// rekeyOutcome is what settleAbandoned found.
type rekeyOutcome int

const (
	// rekeyGone: there is no re-key of the prefix (any more).
	rekeyGone rekeyOutcome = iota
	// rekeySettled: its connection had gone away, and it has now been rolled
	// back (or finished, if it had switched).
	rekeySettled
	// rekeyRunning: the connection doing it is renewing its lease.
	rekeyRunning
	// rekeyRecovering: another connection is still settling it.
	rekeyRecovering
	// rekeyStuck: its connection has gone away, and settling it failed.
	rekeyStuck
)

// recoverDead settles every re-key whose connection has gone away (its
// lease has expired) before it returns: it takes the job over and rolls it
// back or finishes it, or waits while another connection does. Re-keys whose
// connection renews its lease are left alone. Only a failure to read the
// jobs is an error: statements that run into a job that couldn't be settled
// say so, and try again.
func (s *store) recoverDead(ctx context.Context) error {
	jobs, err := s.redis(ctx).HGetAll(ctx, rekeyKey).Result()
	if err != nil {
		return wrapRedis(err, "failed to read pending renames")
	}
	for prefix, raw := range jobs {
		var job rekeyJob
		if json.Unmarshal([]byte(raw), &job) != nil {
			continue
		}
		if !job.Recovering {
			if alive, err := s.ownerAlive(ctx, &job); err != nil || alive {
				continue
			}
		}
		_, _, _ = s.settleAbandoned(ctx, prefix, true)
	}
	return nil
}

// recoverStale settles the re-key of a prefix if its connection has gone
// away, without waiting for another connection that does.
func (s *store) recoverStale(ctx context.Context, prefix string) {
	_, _, _ = s.settleAbandoned(ctx, prefix, false)
}

// settleAbandoned rolls back (or finishes) the re-key of a prefix if its
// connection has gone away. If another connection is doing that, it waits
// for it, up to rekeyTTL, when wait is set. It returns what it found, the
// job as last read, and why settling it failed (rekeyStuck).
func (s *store) settleAbandoned(ctx context.Context, prefix string, wait bool) (rekeyOutcome, *rekeyJob, error) {
	var seen *rekeyJob
	var waitUntil time.Time
	for attempt := 0; attempt < 100; attempt++ {
		job, err := s.readRekey(ctx, prefix)
		if err != nil {
			return rekeyStuck, seen, err
		}
		if job == nil {
			if seen == nil {
				return rekeyGone, nil, nil
			}
			return rekeySettled, seen, nil
		}
		seen = job
		alive, err := s.ownerAlive(ctx, job)
		if err != nil {
			return rekeyStuck, job, err
		}
		if alive && !job.Recovering {
			return rekeyRunning, job, nil
		}
		if alive {
			if waitUntil.IsZero() {
				waitUntil = time.Now().Add(rekeyTTL)
			}
			if !wait || time.Now().After(waitUntil) {
				return rekeyRecovering, job, nil
			}
			select {
			case <-ctx.Done():
				return rekeyRecovering, job, nil
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		switch err := s.recoverRekey(ctx, job); {
		case err == nil:
			return rekeySettled, job, nil
		case errors.Is(err, errLostRekey), errors.Is(err, goredis.TxFailedErr), errors.Is(err, errRekeyOwnerAlive):
			// Another connection took it over first, or it changed: look
			// again.
		default:
			return rekeyStuck, job, err
		}
	}
	return rekeyRecovering, seen, nil
}

// readRekey returns the re-key of a prefix, or nil if there is none.
func (s *store) readRekey(ctx context.Context, prefix string) (*rekeyJob, error) {
	raw, err := s.redis(ctx).HGet(ctx, rekeyKey, prefix).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapRedis(err, "failed to read pending renames")
	}
	var job rekeyJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, errorf(adbc.StatusInternal, "corrupt record of a rename of key prefix %q: %v", prefix, err)
	}
	return &job, nil
}

// ownerAlive reports whether the lease of a job's owner has not expired.
func (s *store) ownerAlive(ctx context.Context, job *rekeyJob) (bool, error) {
	n, err := s.redis(ctx).Exists(ctx, rekeyAliveKey(job.Owner)).Result()
	if err != nil {
		return false, wrapRedis(err, "failed to read pending renames")
	}
	return n > 0, nil
}

// recoverRekey takes a job over from an owner whose lease has expired, then
// rolls it back or finishes it. If that fails, it gives the job up at once,
// by deleting its own lease key, rather than keeping it from every other
// connection until that expires.
func (s *store) recoverRekey(ctx context.Context, job *rekeyJob) error {
	run, err := s.trackRekey(ctx)
	if err != nil {
		return err
	}
	defer s.untrackRekey(run)
	taken, err := s.takeOver(ctx, job)
	if err != nil {
		return err
	}
	s.holdRekey(run, taken.lease)
	defer taken.lease.release()
	if taken.Switched {
		err = s.finishRekey(taken.lease.ctx, taken)
	} else {
		err = s.rollbackRekey(taken.lease.ctx, taken)
	}
	if err != nil {
		s.client.Del(context.WithoutCancel(ctx), rekeyAliveKey(taken.Owner))
	}
	return err
}

// takeOver makes this connection the owner of a job whose owner's lease has
// expired, and marks it recovering. The caller releases the lease.
func (s *store) takeOver(ctx context.Context, job *rekeyJob) (*rekeyJob, error) {
	// INCRBY rather than INCR: a user without rename_rekey's commands
	// recovers the rewrites of ALTER COLUMN … TYPE (see beginMove).
	n, err := s.redis(ctx).IncrBy(ctx, rekeyNextKey, 1).Result()
	if err != nil {
		return nil, wrapRedis(err, "failed to recover a rename")
	}
	owner := strconv.FormatInt(n, 10)
	var taken *rekeyJob
	sent := time.Now()
	err = s.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
		cur, err := ownRekey(ctx, tx, job)
		if err != nil {
			return err
		}
		if n, err := tx.Exists(ctx, rekeyAliveKey(job.Owner)).Result(); err != nil {
			return err
		} else if n > 0 {
			return errRekeyOwnerAlive
		}
		cur.Owner, cur.Recovering = owner, true
		raw, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
			p.HSet(ctx, rekeyKey, job.OldPrefix, raw)
			p.Set(ctx, rekeyAliveKey(owner), "1", rekeyTTL)
			return nil
		})
		taken = cur
		return err
	}, rekeyKey, rekeyAliveKey(job.Owner))
	if err != nil {
		return nil, err // including another connection taking it first
	}
	taken.lease = s.holdLease(ctx, owner, sent)
	return taken, nil
}

// ---- checks for concurrent statements ----

// movingErr refuses to change a table whose rows are being moved by a
// connection that renews its lease.
func movingErr(meta *tableMeta) error {
	if meta.RekeyTo == "" {
		return nil
	}
	if meta.Rewriting {
		return errorf(adbc.StatusIO, "table %q.%q is being altered: ALTER COLUMN … TYPE is rewriting its rows to new keys; try again when that has finished. "+
			"If the connection altering it has gone away, the next connection to open, or the next statement that changes the table, "+
			"rolls the change back once its lease (%s) has expired",
			displaySchema(meta.Schema), meta.Name, rekeyTTL)
	}
	return errorf(adbc.StatusIO, "table %q.%q is being renamed and its rows moved to new keys (%s); try again when that has finished. "+
		"If the connection renaming it has gone away, the next connection to open, or the next statement that changes the table, "+
		"rolls the rename back once its lease (%s) has expired",
		displaySchema(meta.Schema), meta.Name, OptionStringRenameRekey, rekeyTTL)
}

// checkWritable refuses a statement about to change a table whose rows are
// being moved. If the connection moving them went away, the move is rolled
// back (or finished) first, or the statement waits while another connection
// does; then a statement whose table is back as it was goes on.
func (s *store) checkWritable(ctx context.Context, meta *tableMeta) error {
	if meta.Foreign {
		return virtualReadOnly(meta.Schema)
	}
	if meta.RekeyTo == "" {
		return nil
	}
	outcome, job, err := s.settleAbandoned(ctx, meta.prefix(), true)
	sch, t := displaySchema(meta.Schema), meta.Name
	if meta.Rewriting {
		return s.checkRewritable(ctx, meta, outcome, job, err)
	}
	switch outcome {
	case rekeyRunning:
		return movingErr(meta)
	case rekeyRecovering:
		return errorf(adbc.StatusIO, "table %q.%q was being renamed (%s) by a connection that has gone away, "+
			"and another connection is still rolling the rename back; try again in a moment", sch, t, OptionStringRenameRekey)
	case rekeyStuck:
		return errorf(adbc.StatusIO, "table %q.%q is being renamed (%s), but the connection renaming it has gone away, "+
			"and rolling the rename back failed: %v. The next connection to open, or the next statement that changes "+
			"the table, tries again", sch, t, OptionStringRenameRekey, errMessage(err))
	case rekeySettled:
		if job.Switched {
			return errorf(adbc.StatusIO, "table %q.%q was being renamed to %q (%s) by a connection that has gone away; "+
				"that rename has now been completed", sch, t, job.To, OptionStringRenameRekey)
		}
	}
	// The move is over (rolled back now, or ended since the metadata was
	// read). Unless the table changed otherwise, the statement goes on.
	if s.unmoved(ctx, meta) {
		return nil
	}
	if outcome == rekeySettled {
		return errorf(adbc.StatusIO, "table %q.%q was being renamed (%s) by a connection that has gone away; "+
			"the rename has now been rolled back, so try again", sch, t, OptionStringRenameRekey)
	}
	return errorf(adbc.StatusIO, "table %q.%q was being renamed (%s) when this statement started, and that has "+
		"ended since; try again", sch, t, OptionStringRenameRekey)
}

// unmoved reports whether a table that was being moved when meta was read
// is back as it was, the move rolled back (or ended without changing it);
// then meta counts as read now, without rekey_to.
func (s *store) unmoved(ctx context.Context, meta *tableMeta) bool {
	fresh, err := s.getTable(ctx, meta.Schema, meta.Name)
	if err != nil || !sameTable(meta, fresh) {
		return false
	}
	meta.RekeyTo, meta.Rewriting, meta.readAt = "", false, fresh.readAt
	return true
}

// checkRewritable is checkWritable for a table that ALTER COLUMN … TYPE was
// rewriting when meta was read, once settleAbandoned has found the outcome.
func (s *store) checkRewritable(ctx context.Context, meta *tableMeta, outcome rekeyOutcome, job *rekeyJob, err error) error {
	sch, t := displaySchema(meta.Schema), meta.Name
	switch outcome {
	case rekeyRunning:
		return movingErr(meta)
	case rekeyRecovering:
		return errorf(adbc.StatusIO, "table %q.%q was being altered (ALTER COLUMN … TYPE) by a connection that has gone away, "+
			"and another connection is still rolling the change back; try again in a moment", sch, t)
	case rekeyStuck:
		return errorf(adbc.StatusIO, "table %q.%q is being altered (ALTER COLUMN … TYPE), but the connection altering it has gone away, "+
			"and rolling the change back failed: %v. The next connection to open, or the next statement that changes "+
			"the table, tries again", sch, t, errMessage(err))
	case rekeySettled:
		if job.Switched {
			return errorf(adbc.StatusIO, "table %q.%q was being altered (ALTER COLUMN … TYPE) by a connection that has gone away; "+
				"that change has now been completed, so try again", sch, t)
		}
	}
	if s.unmoved(ctx, meta) {
		return nil
	}
	if outcome == rekeySettled {
		return errorf(adbc.StatusIO, "table %q.%q was being altered (ALTER COLUMN … TYPE) by a connection that has gone away; "+
			"the change has now been rolled back, so try again", sch, t)
	}
	return errorf(adbc.StatusIO, "table %q.%q was being altered (ALTER COLUMN … TYPE) when this statement started, and that has "+
		"ended since; try again", sch, t)
}

// sameTable reports whether fresh metadata is the moving table's, with
// rekey_to cleared and nothing else changed.
func sameTable(moving, fresh *tableMeta) bool {
	if fresh.RekeyTo != "" {
		return false
	}
	was := *moving
	was.RekeyTo, was.Rewriting = "", false
	a, aerr := json.Marshal(&was)
	b, berr := json.Marshal(fresh)
	return aerr == nil && berr == nil && string(a) == string(b)
}

// errMessage is an error's text without the driver's prefix.
func errMessage(err error) string {
	var ae adbc.Error
	if asAdbc(err, &ae) {
		return strings.TrimPrefix(ae.Msg, "[redis] ")
	}
	return err.Error()
}

// earlierRekeyErr says why a table can't be re-keyed while an earlier
// re-key of its key prefix is left, or returns nil once that is settled.
func (s *store) earlierRekeyErr(ctx context.Context, schema, table, prefix string) error {
	outcome, job, err := s.settleAbandoned(ctx, prefix, true)
	if job != nil {
		schema, table = job.Schema, job.From
	}
	sch := displaySchema(schema)
	switch outcome {
	case rekeyRunning:
		return errorf(adbc.StatusIO, "an earlier rename of table %q.%q is still moving its rows (%s); try again when it has finished. "+
			"If the connection doing it has gone away, the next connection to open, or the next such rename, "+
			"rolls it back once its lease (%s) has expired", sch, table, OptionStringRenameRekey, rekeyTTL)
	case rekeyRecovering:
		return errorf(adbc.StatusIO, "an earlier rename of table %q.%q (%s) was left unfinished by a connection that has gone away, "+
			"and another connection is still rolling it back; try again in a moment", sch, table, OptionStringRenameRekey)
	case rekeyStuck:
		return errorf(adbc.StatusIO, "an earlier rename of table %q.%q (%s) was left unfinished by a connection that has gone away, "+
			"and rolling it back failed: %v. The next connection to open, or the next such rename, tries again",
			sch, table, OptionStringRenameRekey, errMessage(err))
	}
	return nil
}

// checkWritten runs after a statement has written rows of a table. If the
// writes ended rekeyFence or more after the metadata was read (or last
// checked), a re-key may have started meanwhile and copied the rows before
// the writes, so it checks for one.
func (s *store) checkWritten(ctx context.Context, meta *tableMeta) error {
	if !meta.readAt.IsZero() && elapsed(meta.readAt) < rekeyFence {
		return nil
	}
	return s.checkKeys(ctx, meta, true)
}

// checkReads runs after a statement, for the tables whose metadata it read.
// One that read a table's metadata while its rows were being moved, or
// rekeyFence or more before it ended, checks that the rows were still the
// table's when it read them.
func (s *store) checkReads(ctx context.Context, metas []*tableMeta) error {
	for _, m := range metas {
		if m.RekeyTo == "" && elapsed(m.readAt) < rekeyFence {
			continue
		}
		if err := s.checkKeys(ctx, m, false); err != nil {
			return err
		}
	}
	return nil
}

// checkKeys fails if a table's rows may have moved away since its metadata
// was read: its prefix was released (the table was re-keyed, truncated or
// dropped, even if an earlier version of the driver gave the prefix to
// another table since), or is being moved by a re-key (which only matters
// to readers once it has switched). Otherwise the metadata counts as read
// again now.
func (s *store) checkKeys(ctx context.Context, meta *tableMeta, writing bool) error {
	if meta.Foreign {
		if writing {
			return virtualReadOnly(meta.Schema)
		}
		return nil
	}
	sent := time.Now()
	pipe := s.redis(ctx).Pipeline()
	job := pipe.HGet(ctx, rekeyKey, meta.prefix())
	gen := pipe.HGet(ctx, releasedKey, meta.prefix())
	kept := pipe.SIsMember(ctx, prefixesKey, meta.prefix())
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
		return wrapRedis(err, "failed to check the table's keys")
	}
	n, _ := gen.Int64()
	moved := n != meta.PrefixGen || !kept.Val()
	if raw, err := job.Result(); err == nil {
		var j rekeyJob
		if json.Unmarshal([]byte(raw), &j) == nil && j.OldPrefixGen == meta.PrefixGen {
			moved = moved || writing || j.Switched
			// The move may have been abandoned.
			s.recoverStale(ctx, meta.prefix())
		}
	}
	if !moved {
		meta.readAt = sent
		return nil
	}
	if writing {
		// No table has the prefix any more, and none will take it again
		// (claimNames), so what this statement wrote there is left over.
		if !kept.Val() {
			s.discardWritten(ctx, meta)
		}
		return writeMovedErr(meta)
	}
	return errorf(adbc.StatusIO, "table %q.%q was renamed with its rows moved to new keys, rewritten by ALTER COLUMN … TYPE, truncated or dropped, while this statement was reading it; try again",
		displaySchema(meta.Schema), meta.Name)
}

// writeMovedErr is the error of a statement whose table's rows moved away
// (a re-keying rename or ALTER COLUMN … TYPE), or that was truncated or
// dropped, while it was writing to it.
func writeMovedErr(meta *tableMeta) error {
	return errorf(adbc.StatusIO, "table %q.%q was renamed with its rows moved to new keys, rewritten by ALTER COLUMN … TYPE, truncated or dropped, while this statement was writing to it; some of its changes may be lost or have gone to another table",
		displaySchema(meta.Schema), meta.Name)
}

// elapsed is the time since t on both clocks: the monotonic clock may stop
// while the machine sleeps.
func elapsed(t time.Time) time.Duration {
	return max(time.Since(t), time.Now().Round(0).Sub(t.Round(0)))
}
