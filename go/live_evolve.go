// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
)

// LiveHashEvolutionOptions selects a complete new SQL view of an existing live
// collection. Columns names the desired public columns after Renames; omitted
// source fields remain untouched. Existing fields cannot change type/encoding.
// All application writers, SQL writers and registration administrators must stay
// stopped until evolution (including resume/cancel) completes.
type LiveHashEvolutionOptions struct {
	HashAdoptOptions
	Columns            []string
	ExpectedIndex      string
	ExclusiveQuiescent bool
}

// EvolveLiveHashes exposes/removes compatible source fields and changes the
// driver-owned index policy. It builds a fresh index without changing HASHes.
// A durable transition blocks new SQL while retaining both index identities.
// ResumeLiveHashes only observes an existing candidate; it never retries CREATE.
func EvolveLiveHashes(ctx context.Context, options map[string]string, o LiveHashEvolutionOptions) (*LiveHashRegistration, error) {
	if o.Table == "" || o.ExpectedIndex == "" || !o.ExclusiveQuiescent || o.Prefix == "" || len(o.Columns) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "live evolution requires table, exact prefix/index, columns and exclusive quiescence")
	}
	if o.Schema != "" && o.Schema != liveSchema || o.Mode != "" || o.Index != "" || o.OwnIndex || o.TrustStrings || o.operation != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "live evolution accepts only live registration shaping options")
	}
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	return evolveLiveHashes(ctx, c, o)
}

func evolveLiveHashes(ctx context.Context, c *hashConn, o LiveHashEvolutionOptions) (*LiveHashRegistration, error) {
	ctx = context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	if c.db.flex {
		return nil, flexUnsupported("live HASH tables")
	}
	before, r, err := c.st.readLiveRecord(ctx, o.Table)
	if err != nil {
		return nil, err
	}
	if r.State != "ready" || r.Pending != nil || r.Meta.prefix() != o.Prefix || r.Meta.index() != o.ExpectedIndex {
		return nil, stateErrorf("40001", "live evolution target identity changed or transition is unfinished")
	}
	if err := c.st.validateLiveIndex(ctx, r.Meta); err != nil {
		return nil, err
	}
	// Force retained source fields through their existing declared decoder. This
	// validates every observed value instead of silently widening an inferred type.
	types, encodings, renames := map[string]string{}, map[string]string{}, map[string]string{}
	for _, col := range r.Meta.Columns {
		if col.Name == keyField {
			continue
		}
		name := col.Name
		if renamed, ok := o.Renames[col.field()]; ok {
			name = renamed
		}
		if !slices.Contains(o.Columns, name) {
			continue
		}
		renames[col.field()] = name
		encoding := col.Encoding
		if encoding == "" {
			encoding = "native"
		}
		types[name], encodings[name] = col.Type.SQLName(), encoding
	}
	for k, v := range o.Types {
		types[k] = v
	}
	for k, v := range o.Encodings {
		encodings[k] = v
	}
	for k, v := range o.Renames {
		renames[k] = v
	}
	shape := o.HashAdoptOptions
	shape.Types, shape.Encodings, shape.Renames = types, encodings, renames
	// IndexColumns is checked after selecting public columns below.
	shape.IndexColumns = nil
	a := &adopter{c: c, o: shape, schema: liveSchema, table: o.Table, index: liveIndexPrefix + snapshotID(), progress: func(string) {}, res: &HashAdoption{Report: &HashReport{Prefix: o.Prefix}}}
	if err := a.readLiveEvolution(ctx, o.Columns); err != nil {
		return nil, err
	}
	for _, col := range a.cols {
		if slices.Contains(o.Columns, col.name) && col.guess != nil && col.guess.misfits > 0 {
			return nil, errorf(adbc.StatusInvalidData, "source values do not match live column %q encoding/type", col.name)
		}
	}
	meta, err := a.plan()
	if err != nil {
		return nil, err
	}
	meta, err = planLiveEvolution(r.Meta, meta, o.Columns, o.IndexColumns)
	if err != nil {
		return nil, err
	}
	result := &LiveHashRegistration{Table: liveSchema + "." + o.Table, Prefix: o.Prefix, Index: meta.index()}
	for _, col := range meta.Columns {
		result.Columns = append(result.Columns, HashColumn{Name: col.Name, Field: col.field(), Type: col.Type.SQLName(), Encoding: col.Encoding})
	}
	if !o.Apply {
		result.Index = ""
		return result, nil
	}
	pending := &liveRecord{State: "evolving", Meta: r.Meta, Pending: meta}
	raw, err := c.st.swapLiveEvolution(ctx, before, pending)
	if err != nil {
		return result, err
	}
	if err := c.st.searchDo(ctx, meta.index(), liveIndexArgs(meta)...).Err(); err != nil {
		// Only a direct native ACL refusal proves FT.CREATE could not have run.
		// Preserve this durable fact so cancellation may remove an absent candidate.
		if _, native := err.(interface {
			error
			RedisError()
		}); native && strings.HasPrefix(err.Error(), "NOPERM ") {
			pending.CreateRefused = true
			if _, markErr := c.st.swapLiveEvolution(ctx, raw, pending); markErr != nil {
				return result, markErr
			}
		}
		return result, wrapRedis(err, "live evolution index creation failed; identity retained; resume or cancel without replaying CREATE")
	}
	if err := c.st.resumeLiveEvolution(ctx, raw, pending); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

func planLiveEvolution(old, inferred *tableMeta, names, indexNames []string) (*tableMeta, error) {
	if len(names) == 0 || len(names) > 1024 {
		return nil, errorf(adbc.StatusInvalidArgument, "live evolution requires 1..1024 public columns")
	}
	meta := *inferred
	meta.Columns = nil
	meta.Live, meta.LiveVersion = true, snapshotID()
	seen := map[string]bool{}
	for _, name := range names {
		if !utf8.ValidString(name) || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "__") || seen[name] {
			return nil, errorf(adbc.StatusInvalidArgument, "invalid or duplicate live column %q", name)
		}
		seen[name] = true
		i, ok := inferred.resolve(name)
		if !ok {
			return nil, errorf(adbc.StatusInvalidArgument, "no compatible source column %q", name)
		}
		col := inferred.Columns[i]
		if !utf8.ValidString(col.field()) || strings.ContainsRune(col.field(), 0) || strings.HasPrefix(col.field(), "__") {
			return nil, errorf(adbc.StatusInvalidArgument, "reserved live source field")
		}
		for _, prior := range old.Columns {
			if prior.field() == col.field() && (!reflect.DeepEqual(prior.Type, col.Type) || prior.Encoding != col.Encoding || prior.TextFormat != col.TextFormat) {
				return nil, errorf(adbc.StatusNotImplemented, "live evolution cannot change source type or encoding")
			}
		}
		col.Indexed = col.Encoding != "text" && col.Type.Alias != "uuid" && indexable(col.Type) && simpleName(col.field()) && (indexNames == nil || slices.Contains(indexNames, name))
		col.NaNs = col.Type.Kind.isFloat()
		if col.Type.Kind == KindString {
			col.TagsChecked, col.TagValues = true, tagsTruncated
		}
		meta.Columns = append(meta.Columns, col)
	}
	for _, name := range indexNames {
		if !seen[name] {
			return nil, errorf(adbc.StatusInvalidArgument, "index column %q is not selected", name)
		}
	}
	indexed := 0
	for _, col := range meta.Columns {
		if col.Indexed {
			indexed++
		}
	}
	if indexed > maxIndexed {
		return nil, errorf(adbc.StatusInvalidArgument, "too many indexed live columns")
	}
	return &meta, nil
}

func validLiveEvolutionPair(a, b *tableMeta) bool {
	return a != nil && b != nil && a.Live && b.Live && a.Schema == liveSchema && b.Schema == a.Schema && a.Name == b.Name && a.prefix() == b.prefix() && a.index() != b.index() &&
		strings.HasPrefix(b.index(), liveIndexPrefix) && len(strings.TrimPrefix(b.index(), liveIndexPrefix)) == 32 && b.Adopted && b.KeyRows && !b.Unmanaged && !b.AppIndex && !b.Foreign && b.SnapshotRows == nil && !b.SnapshotOverlay && len(b.SnapshotSegments) == 0
}

func (s *store) swapLiveEvolution(ctx context.Context, before string, r *liveRecord) (string, error) {
	after, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if len(after) > 1<<20 {
		return "", errorf(adbc.StatusInvalidArgument, "live evolution descriptor exceeds 1 MiB")
	}
	n, err := once(ctx, s.client, "FCALL", redisFunctionName(`if redis.call('HGET',KEYS[1],ARGV[1]) ~= ARGV[2] then return 0 end; redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]); return 1`), 1, liveRegistry, r.Meta.Name, before, string(after)).Int()
	if err != nil {
		return "", wrapRedis(err, "live evolution transition outcome uncertain; inspect/resume retained identity")
	}
	if n != 1 {
		return "", stateErrorf("40001", "live registration changed during evolution")
	}
	return string(after), nil
}

func (s *store) resumeLiveEvolution(ctx context.Context, raw string, r *liveRecord) error {
	if r.State == "evolving" {
		if r.CreateRefused {
			return errorf(adbc.StatusInvalidState, "live evolution CREATE was refused; cancel evolution before starting a new attempt")
		}
		// Readiness never sends CREATE again, even if its original reply was lost.
		for {
			reply, err := s.searchDo(ctx, r.Pending.index(), "FT.INFO", r.Pending.index()).Result()
			if err != nil {
				return wrapRedis(err, "live evolution candidate unavailable; identity retained, CREATE was not replayed")
			}
			info := indexInfoOf(reply)
			if err := validateLiveIndexDefinition(r.Pending, info); err != nil {
				return err
			}
			if info.failures != 0 {
				return errorf(adbc.StatusInvalidData, "live evolution candidate has indexing failures; cancel and fix source encodings")
			}
			if !info.indexing && info.percent >= 1 {
				break
			}
			if err := liveEvolutionWait(ctx); err != nil {
				return err
			}
		}
		// Retain the old identity durably until its index is positively retired.
		r = &liveRecord{State: "retiring", Meta: r.Pending, Pending: r.Meta}
		var err error
		raw, err = s.swapLiveEvolution(ctx, raw, r)
		if err != nil {
			return err
		}
	}
	if r.State != "retiring" && r.State != "aborting" {
		return errorf(adbc.StatusInvalidState, "invalid live evolution state")
	}
	if err := s.validateLiveIndex(ctx, r.Meta); err != nil {
		return err
	}
	reply, err := s.searchDo(ctx, r.Pending.index(), "FT.INFO", r.Pending.index()).Result()
	if err != nil {
		// For retiring/aborting an absent index is the desired idempotent outcome.
		// Abort enters this phase only after observing the candidate or a proven ACL refusal.
		if !isUnknownIndex(err) || executionPoolUncertain(err) {
			return wrapRedis(err, "live evolution cleanup index unavailable")
		}
	} else {
		if err := validateLiveIndexDefinition(r.Pending, indexInfoOf(reply)); err != nil {
			return err
		}
		if err := s.searchDo(ctx, r.Pending.index(), "FT.DROPINDEX", r.Pending.index()).Err(); err != nil {
			return wrapRedis(err, "live evolution cleanup outcome uncertain; resume retained identity")
		}
	}
	_, err = s.swapLiveEvolution(ctx, raw, &liveRecord{State: "ready", Meta: r.Meta})
	return err
}

// CancelLiveHashEvolution restores the old definition before the retirement
// phase. The exact original index is required. Source writers must remain paused.
// An unobserved uncertain CREATE cannot be canceled: its identity stays reserved.
func CancelLiveHashEvolution(ctx context.Context, options map[string]string, table, expectedIndex string) error {
	c, err := dialHashes(ctx, options)
	if err != nil {
		return err
	}
	defer c.close(ctx)
	raw, r, err := c.st.readLiveRecord(ctx, table)
	if err != nil {
		return err
	}
	if r.Meta.index() != expectedIndex || (r.State != "evolving" && r.State != "aborting") {
		return errorf(adbc.StatusInvalidState, "live evolution cannot be canceled after definition switch or with a different index")
	}
	if r.State == "evolving" {
		reply, err := c.st.searchDo(ctx, r.Pending.index(), "FT.INFO", r.Pending.index()).Result()
		if err != nil {
			if !r.CreateRefused || !isUnknownIndex(err) || executionPoolUncertain(err) {
				return wrapRedis(err, "unobserved live candidate identity retained; cancellation cannot replay or forget CREATE")
			}
		} else if err := validateLiveIndexDefinition(r.Pending, indexInfoOf(reply)); err != nil {
			return err
		}
		r.State = "aborting"
		raw, err = c.st.swapLiveEvolution(ctx, raw, r)
		if err != nil {
			return err
		}
	}
	return c.st.resumeLiveEvolution(ctx, raw, r)
}

func liveEvolutionWait(ctx context.Context) error {
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Evolution validates complete atomic HASH images, accounting for aggregate
// pipeline failures before accepting schema facts. Only selected source fields
// contribute to inference; unrelated application fields cannot veto evolution.
func (a *adopter) readLiveEvolution(ctx context.Context, names []string) error {
	gopts := guessOptions{types: a.o.Types, renames: a.o.Renames, encodings: a.o.Encodings}
	gs, err := newGuesserFor(gopts)
	if err != nil {
		return err
	}
	scan, err := a.c.scanKeys(ctx, a.o.Prefix, "hash")
	if err != nil {
		return err
	}
	reader := &unmanagedProjectionRead{store: a.c.st}
	seen := map[string]bool{}
	for {
		keys, done, err := scan.next(ctx)
		if err != nil {
			return err
		}
		selected := keys[:0:0]
		for _, key := range keys {
			if !seen[key] {
				seen[key] = true
				if isRowKey(a.o.Prefix, key) {
					selected = append(selected, key)
				}
			}
		}
		rows, err := reader.native(ctx, selected)
		if err != nil {
			return err
		}
		for _, raw := range rows {
			if raw == nil {
				continue
			}
			row := hashRow{key: raw[keyField]}
			fields := make([]string, 0, len(raw))
			for field := range raw {
				if field != keyField {
					fields = append(fields, field)
				}
			}
			slices.Sort(fields)
			for _, field := range fields {
				name := field
				if renamed, ok := a.o.Renames[field]; ok {
					name = renamed
				}
				if slices.Contains(names, name) {
					row.fields = append(row.fields, field)
					row.values = append(row.values, raw[field])
				}
			}
			gs.add(row)
		}
		if done {
			break
		}
	}
	a.cols, _, err = gs.columns(gopts)
	return err
}
