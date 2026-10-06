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

// Rows left under released key prefixes (redis-adbc sweep)
//
// DROP TABLE, TRUNCATE and a re-keying rename delete a table's rows with
// FT.DROPINDEX … DD, which deletes the rows its index knows about, and
// release its key prefix: adbc:{meta}:released counts the releases, and no
// table takes a released prefix again (claimNames). Rows can still be left
// under it. A statement still writing through the old metadata deletes what
// it wrote there once it finds out (checkKeys, discardWritten), but one
// whose process exits first never does, and a write of up to pipelineChunk
// rows that read the metadata within rekeyFence of the release doesn't
// check (checkWritten). No table or query reads such rows.
//
// SweepReleased finds them with one SCAN of the keyspace (on every primary
// of a cluster) and deletes them. It runs on demand, not when a connection
// opens, since it reads every key. A row's key is its table's prefix and a
// row id, and a driver table's prefix is <schema>:<table>: (or <table>~N),
// whose parts escapeKeyPart leaves without a ':'. So a key is a leftover
// row when it is a HASH, the part up to its last ':' is a released prefix,
// and the rest is a row id. SweepReleased leaves alone:
//
//   - Prefixes that a table has (reserved in prefixesKey): up to v0.0.7,
//     the driver gave released prefixes to new tables. Each pipeline of
//     deletions checks this again first.
//   - The prefixes of a re-keying rename in progress: a DROP TABLE during
//     one releases the table's prefix while the rename still reads it. A
//     later sweep, once the rename has settled, does them.
//   - The prefixes of adopted tables, including those dropped since
//     (droppedAdoptedKey), and the prefixes that overlap them: the HASHes
//     there are the application's. Dropping an adopted table releases its
//     prefix too, so that no new table takes it. v0.0.11 and v0.0.12 didn't
//     record such drops, so prefixes that don't have the form of a driver
//     table's are left alone as well.
//
// Temporary tables' prefixes are never counted as released; sweepTemp
// drops the tables of connections that went away. The released counts stay
// after a sweep: they keep new tables off the prefixes, and statements
// check them (checkKeys). A writer still running may leave keys after the
// sweep has passed them; another sweep deletes them.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

// SweepOptions are SweepReleased's options.
type SweepOptions struct {
	// Apply deletes the HASHes found; otherwise SweepReleased only counts
	// them.
	Apply bool
	// Progress, if set, is told what is happening.
	Progress func(string)
}

// Sweep is what SweepReleased found and deleted.
type Sweep struct {
	ServerVersion string `json:"server_version,omitempty"`
	Cluster       bool   `json:"cluster"`
	// Released counts the key prefixes that were ever released
	// (adbc:{meta}:released).
	Released int `json:"released"`
	// Prefixes are the released prefixes that HASHes were found under.
	Prefixes []SweptPrefix `json:"prefixes,omitempty"`
	// Skipped are the released prefixes left alone, and why.
	Skipped []SkippedPrefix `json:"skipped,omitempty"`
	Found   int64           `json:"found"`
	Deleted int64           `json:"deleted"`
	Applied bool            `json:"applied"`
}

// SweptPrefix counts the HASHes found under a released prefix, and those
// deleted.
type SweptPrefix struct {
	Prefix  string `json:"prefix"`
	Found   int64  `json:"found"`
	Deleted int64  `json:"deleted"`
}

// SkippedPrefix is a released prefix that SweepReleased left alone.
type SkippedPrefix struct {
	Prefix string `json:"prefix"`
	Reason string `json:"reason"`
}

// SweepReleased finds the HASHes left under the key prefixes of dropped and
// truncated tables (and of tables a re-keying rename moved), and with
// o.Apply deletes them. The options are the driver's database options.
func SweepReleased(ctx context.Context, options map[string]string, o SweepOptions) (*Sweep, error) {
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	progress := o.Progress
	if progress == nil {
		progress = func(string) {}
	}
	res := &Sweep{Cluster: c.cluster(), Applied: o.Apply}
	if info, err := c.client.Info(ctx, "server").Result(); err == nil {
		res.ServerVersion = infoField(info, "redis_version")
	}
	cands, err := c.sweepCandidates(ctx, res)
	if err != nil {
		return nil, err
	}
	s := &sweeper{c: c, res: res, cands: cands, apply: o.Apply, seen: map[string]bool{}}
	if len(cands) > 0 {
		err = s.run(ctx, progress)
	}
	for _, sp := range s.found {
		if s.cands[sp.Prefix] == sp {
			res.Prefixes = append(res.Prefixes, *sp)
			res.Found += sp.Found
			res.Deleted += sp.Deleted
		}
	}
	sort.Slice(res.Prefixes, func(i, j int) bool { return res.Prefixes[i].Prefix < res.Prefixes[j].Prefix })
	sort.Slice(res.Skipped, func(i, j int) bool { return res.Skipped[i].Prefix < res.Skipped[j].Prefix })
	return res, err
}

// Why SweepReleased leaves a released prefix alone.
const (
	sweepTaken    = "a table has it"
	sweepRetaken  = "a table took it while sweep ran (versions up to v0.0.7 give released prefixes to new tables)"
	sweepMoving   = "a rename (adbc.redis.rename_rekey) is moving rows to or from it; run sweep again once it has finished"
	sweepAdopted  = "an adopted table had it: the HASHes are the application's"
	sweepDenied   = "this user may not delete its keys (NOPERM)"
	sweepNotTable = "it isn't of the form of a driver table's prefix (<schema>:<table>:), so an adopted table had it: the HASHes are the application's"
)

// sweepCandidates reads the released prefixes, and returns those to sweep.
// The others go to res.Skipped.
func (c *hashConn) sweepCandidates(ctx context.Context, res *Sweep) (map[string]*SweptPrefix, error) {
	pipe := c.client.Pipeline()
	released := pipe.HGetAll(ctx, releasedKey)
	reserved := pipe.SMembers(ctx, prefixesKey)
	dropped := pipe.SMembers(ctx, droppedAdoptedKey)
	jobs := pipe.HGetAll(ctx, rekeyKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, wrapRedis(err, "failed to read the released key prefixes")
	}
	res.Released = len(released.Val())
	moving := map[string]bool{}
	for old, raw := range jobs.Val() {
		moving[old] = true
		var j rekeyJob
		if json.Unmarshal([]byte(raw), &j) == nil {
			moving[j.KeyPrefix] = true
		}
	}
	// Two prefixes of a driver table's form overlap only if they are the
	// same, so only the others (adopted tables') are compared for overlap.
	taken, adopted := map[string]bool{}, map[string]bool{}
	var otherTaken, otherAdopted []string
	for _, p := range reserved.Val() {
		taken[p] = true
		if !driverPrefix(p) {
			otherTaken = append(otherTaken, p)
		}
	}
	for _, p := range dropped.Val() {
		adopted[p] = true
		if !driverPrefix(p) {
			otherAdopted = append(otherAdopted, p)
		}
	}
	cands := map[string]*SweptPrefix{}
	for p := range released.Val() {
		reason := ""
		switch {
		case taken[p]:
			reason = sweepTaken
		case moving[p]:
			reason = sweepMoving
		case adopted[p]:
			reason = sweepAdopted
		case !driverPrefix(p):
			reason = sweepNotTable
		case adoptedOverlap(otherTaken, p) != "":
			reason = fmt.Sprintf("keys under it can be under %q, which a table has", adoptedOverlap(otherTaken, p))
		case adoptedOverlap(otherAdopted, p) != "":
			reason = fmt.Sprintf("keys under it can be under %q, which an adopted table had: the HASHes are the application's", adoptedOverlap(otherAdopted, p))
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, SkippedPrefix{Prefix: p, Reason: reason})
			continue
		}
		cands[p] = &SweptPrefix{Prefix: p}
	}
	return cands, nil
}

// driverPrefix reports whether a key prefix has the form of a driver
// table's: <schema>:<table>: (or <table>~N), each part as escapeKeyPart
// writes it.
func driverPrefix(p string) bool {
	parts := strings.Split(p, ":")
	if len(parts) != 3 || parts[2] != "" {
		return false
	}
	for _, part := range parts[:2] {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			ch := part[i]
			if ch == '%' {
				if i+2 >= len(part) || !isHex(part[i+1]) || !isHex(part[i+2]) {
					return false
				}
				i += 2
				continue
			}
			if escapeKeyPart(string(ch)) != string(ch) {
				return false
			}
		}
	}
	return true
}

func isHex(ch byte) bool {
	return ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'F'
}

// isRowID reports whether s is a row id as row keys hold it: a decimal
// integer without leading zeros.
func isRowID(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sweeper scans for the HASHes under the candidate prefixes and deletes
// them.
type sweeper struct {
	c     *hashConn
	res   *Sweep
	cands map[string]*SweptPrefix
	apply bool
	// found are the candidates that HASHes were found under, in order.
	found []*SweptPrefix
	// seen are the keys found (SCAN may return a key more than once).
	seen  map[string]bool
	batch []string
}

func (s *sweeper) run(ctx context.Context, progress func(string)) error {
	// One SCAN for every candidate: MATCH their longest common prefix.
	var common string
	first := true
	for p := range s.cands {
		if first {
			common, first = p, false
			continue
		}
		n := 0
		for n < len(common) && n < len(p) && common[n] == p[n] {
			n++
		}
		common = common[:n]
	}
	ks, err := s.c.scanKeys(ctx, common, "hash")
	if err != nil {
		return err
	}
	progress(fmt.Sprintf("Looking for HASHes under %s (SCAN MATCH %s* TYPE hash, on %s)",
		plural(int64(len(s.cands)), "released key prefix", "released key prefixes"), globEscape(common),
		plural(int64(len(ks.nodes)), "node")))
	for {
		keys, done, err := ks.next(ctx)
		if err != nil {
			return err
		}
		for _, k := range keys {
			p := collectionPrefix(k)
			sp := s.cands[p]
			if sp == nil || !isRowID(k[len(p):]) || s.seen[k] {
				continue
			}
			s.seen[k] = true
			if sp.Found == 0 {
				s.found = append(s.found, sp)
			}
			sp.Found++
			if s.apply {
				s.batch = append(s.batch, k)
				if len(s.batch) >= pipelineChunk {
					if err := s.flush(ctx); err != nil {
						return err
					}
				}
			}
		}
		if done {
			return s.flush(ctx)
		}
	}
}

// flush deletes the batch of keys, but first checks again that no table
// has their prefixes. A prefix whose keys the ACL user may not delete is
// left alone.
func (s *sweeper) flush(ctx context.Context) error {
	if len(s.batch) == 0 {
		return nil
	}
	batch := s.batch
	s.batch = nil
	skip := func(p, reason string) {
		if s.cands[p] != nil {
			delete(s.cands, p)
			s.res.Skipped = append(s.res.Skipped, SkippedPrefix{Prefix: p, Reason: reason})
		}
	}
	taken := map[string]*goredis.BoolCmd{}
	pipe := s.c.client.Pipeline()
	for _, k := range batch {
		if p := collectionPrefix(k); taken[p] == nil {
			taken[p] = pipe.SIsMember(ctx, prefixesKey, p)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return wrapRedis(err, "failed to check the key prefixes")
	}
	for p, cmd := range taken {
		if cmd.Val() {
			skip(p, sweepRetaken)
		}
	}
	pipe = s.c.client.Pipeline()
	dels := make([]*goredis.IntCmd, len(batch))
	for i, k := range batch {
		if s.cands[collectionPrefix(k)] != nil {
			dels[i] = pipe.Del(ctx, k)
		}
	}
	if pipe.Len() == 0 {
		return nil
	}
	// Each command's error is checked below.
	_, _ = pipe.Exec(ctx)
	for i, cmd := range dels {
		if cmd == nil {
			continue
		}
		p := collectionPrefix(batch[i])
		switch err := cmd.Err(); {
		case err == nil:
			if sp := s.cands[p]; sp != nil && cmd.Val() == 1 {
				sp.Deleted++
			}
		case strings.HasPrefix(err.Error(), "NOPERM"):
			skip(p, sweepDenied)
		default:
			return wrapRedis(err, "failed to delete keys")
		}
	}
	return nil
}
