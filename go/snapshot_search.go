// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotSearchOnce struct{ goredis.Cmder }

func (snapshotSearchOnce) NoRetry() bool { return true }

func snapshotSearchMutation(ctx context.Context, cmd goredis.Cmder) bool {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write {
		return false
	}
	switch strings.ToLower(fmt.Sprint(cmd.Args()[0])) {
	case "ft.create", "ft.dropindex", "ft.alter":
		return true
	}
	return false
}

func snapshotSearchGate(ctx context.Context, cmd goredis.Cmder) *goredis.Cmd {
	fenced := fenceSnapshotCommand(ctx, cmd).(snapshotFencedCommand)
	args := append([]any(nil), fenced.args...)
	args[1] = snapshotRowLayoutLua + snapshotMutationAdmission + "return 1"
	check := goredis.NewCmd(ctx, args...)
	check.SetFirstKeyPos(3)
	return check
}

// Software's distributed index registry is updated only by a top-level Search
// command. Invoking FT.CREATE inside Lua creates a local shard index invisible
// to distributed queries. Bracket global schema operations with server-side
// admission/validation, keep their replies non-replayable, and publish only
// validated private indexes. Row mutations remain atomic inside their Lua fence.
// A late global CREATE cannot publish and its unreferenced index is reclaimed.
func processSnapshotSearch(ctx context.Context, cmd goredis.Cmder, next goredis.ProcessHook) error {
	gate := snapshotSearchGate(ctx, cmd)
	if err := next(ctx, snapshotSearchOnce{gate}); err != nil {
		cmd.SetErr(err)
		return err
	}
	cmd.SetFirstKeyPos(1)
	if err := next(ctx, snapshotSearchOnce{cmd}); err != nil {
		return err
	}
	gate = snapshotSearchGate(ctx, cmd)
	if err := next(ctx, snapshotSearchOnce{gate}); err != nil {
		cmd.SetErr(err)
		return err
	}
	return nil
}

func processSnapshotSearchPipeline(ctx context.Context, cmds []goredis.Cmder, next goredis.ProcessPipelineHook) error {
	var search []goredis.Cmder
	for _, cmd := range cmds {
		if snapshotSearchMutation(ctx, cmd) {
			search = append(search, cmd)
		}
	}
	checks := func() []goredis.Cmder {
		out := make([]goredis.Cmder, len(search))
		for i, cmd := range search {
			out[i] = snapshotSearchOnce{snapshotSearchGate(ctx, cmd)}
		}
		if len(cmds) > 0 && strings.EqualFold(cmds[0].Name(), "multi") {
			out = append([]goredis.Cmder{goredis.NewStatusCmd(ctx, "multi")}, out...)
			out = append(out, goredis.NewSliceCmd(ctx, "exec"))
		}
		return out
	}
	fail := func(err error) error {
		for _, cmd := range cmds {
			cmd.SetErr(err)
		}
		return err
	}
	if err := next(ctx, checks()); err != nil {
		return fail(err)
	}
	for i, cmd := range cmds {
		if snapshotSearchMutation(ctx, cmd) {
			cmd.SetFirstKeyPos(1)
			cmds[i] = snapshotSearchOnce{cmd}
		} else {
			cmds[i] = fenceSnapshotCommand(ctx, cmd)
		}
	}
	if err := next(ctx, cmds); err != nil {
		return err
	}
	if err := next(ctx, checks()); err != nil {
		return fail(err)
	}
	return nil
}

// Private index names are never reused. If a lease expires while a global
// CREATE is in flight, an index can arrive after ordinary data reclamation.
// It can never become visible through a committed manifest. Maintenance removes
// at most one such orphan per pass, excluding every still-registered data ID.
func (s *store) reapSnapshotOrphanIndex(ctx context.Context) error {
	indexes, err := s.searchDo(ctx, "", "FT._LIST").StringSlice()
	if err != nil {
		return err
	}
	for _, name := range indexes {
		id := snapshotDataID(name)
		if id == "" {
			continue
		}
		exists, err := s.client.HExists(ctx, snapshotMetadataGraph, "d:"+id).Result()
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := s.searchDo(ctx, name, "FT.DROPINDEX", name).Err(); err != nil && !isUnknownIndex(err) {
			return err
		}
		return nil
	}
	return nil
}
