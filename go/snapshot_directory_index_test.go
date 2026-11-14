// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// This deterministic transport checks the actual population helper's discovery
// and notification sequence. Integration tests separately exercise Lua ownership,
// lease, identity and permission fences against the native server.
type snapshotDirectoryPopulationHook struct {
	members                                   []string
	initial, final                            int64
	pipelines, pages, counts, notifications   int
	pipelineErr, notifyErr, countErr, pageErr error
}

func (*snapshotDirectoryPopulationHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *snapshotDirectoryPopulationHook) command(ctx context.Context, cmd goredis.Cmder) error {
	if err := ctx.Err(); err != nil {
		cmd.SetErr(err)
		return err
	}
	switch cmd.Name() {
	case "zcard":
		h.counts++
		if h.countErr != nil {
			cmd.SetErr(h.countErr)
			return h.countErr
		}
		value := h.initial
		if h.counts > 1 {
			value = h.final
		}
		cmd.(*goredis.IntCmd).SetVal(value)
	case "zrangebylex":
		h.pages++
		if h.pageErr != nil {
			cmd.SetErr(h.pageErr)
			return h.pageErr
		}
		start := (h.pages - 1) * 256
		var page []string
		if start < len(h.members) {
			page = h.members[start:min(start+256, len(h.members))]
		}
		cmd.(*goredis.StringSliceCmd).SetVal(page)
	case "eval", "evalsha":
		h.notifications++
		if h.notifyErr != nil {
			cmd.SetErr(h.notifyErr)
			return h.notifyErr
		}
		cmd.(interface{ SetVal(any) }).SetVal(int64(cmd.Args()[2].(int) - 3))
	default:
		return fmt.Errorf("unexpected population command %s", cmd.Name())
	}
	return nil
}
func (h *snapshotDirectoryPopulationHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error { return h.command(ctx, cmd) }
}
func (h *snapshotDirectoryPopulationHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		h.pipelines++
		if len(cmds) != 2 || cmds[0].Name() != "zcard" || cmds[1].Name() != "zrangebylex" {
			return errors.New("unbounded or unexpected discovery pipeline")
		}
		for _, cmd := range cmds {
			if err := h.command(ctx, cmd); err != nil {
				return err
			}
		}
		return h.pipelineErr
	}
}
func snapshotPopulationUnitFixture(t *testing.T, rows int) (*store, context.Context, *tableMeta, *snapshotDirectoryPopulationHook) {
	t.Helper()
	h := &snapshotDirectoryPopulationHook{initial: int64(rows), final: int64(rows)}
	for i := 1; i <= rows; i++ {
		h.members = append(h.members, formatSnapshotIndexMember(int64(i)))
	}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	client.AddHook(h)
	t.Cleanup(func() { _ = client.Close() })
	s := &store{client: client, snapshots: true}
	data := snapshotID()
	scope := &snapshotScope{id: snapshotID(), pin: &snapshotPin{token: snapshotID()}, store: s, write: true, owned: map[string]bool{data: true}}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, scope)
	m := &tableMeta{Schema: "public", Name: "t", KeyPrefix: snapshotRows(data, "public", snapshotID())}
	return s, ctx, m, h
}
func TestSnapshotDirectoryPopulationDiscoveryBudget(t *testing.T) {
	for _, rows := range []int{0, 1, 255, 256, 257, 512, 513} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) {
			s, ctx, m, h := snapshotPopulationUnitFixture(t, rows)
			if err := s.populateSnapshotIndex(ctx, m); err != nil {
				t.Fatal(err)
			}
			wantPages := max(1, (rows+255)/256)
			if h.pipelines != 1 || h.pages != wantPages || h.counts != 2 || h.notifications != (rows+255)/256 {
				t.Fatalf("unexpected discovery work: pipelines=%d pages=%d counts=%d notifications=%d", h.pipelines, h.pages, h.counts, h.notifications)
			}
		})
	}
}
func TestSnapshotDirectoryPopulationRetainsFinalCountAndFailureFences(t *testing.T) {
	for _, failure := range []string{"count_error", "page_error", "excess_entries", "short_directory"} {
		t.Run(failure, func(t *testing.T) {
			s, ctx, m, h := snapshotPopulationUnitFixture(t, 1)
			switch failure {
			case "count_error":
				h.countErr = goredis.Nil
			case "page_error":
				h.pageErr = errors.New("failed directory page")
			case "excess_entries":
				h.initial = 0
			case "short_directory":
				h.initial = 2
				h.final = 2
			}
			if err := s.populateSnapshotIndex(ctx, m); err == nil {
				t.Fatal("corrupt/incomplete discovery accepted")
			}
			if failure != "short_directory" && h.notifications != 0 {
				t.Fatal("notified after invalid discovery")
			}
			if h.pages > 2 {
				t.Fatal("unbounded directory traversal", h.pages)
			}
		})
	}
	t.Run("changed_final_directory", func(t *testing.T) {
		s, ctx, m, h := snapshotPopulationUnitFixture(t, 1)
		h.final = 2
		if err := s.populateSnapshotIndex(ctx, m); err == nil {
			t.Fatal("changed directory accepted")
		}
		if h.counts != 2 || h.notifications != 1 {
			t.Fatal("missing final proof", h.counts, h.notifications)
		}
	})
	t.Run("pipeline_error_with_populated_replies", func(t *testing.T) {
		s, ctx, m, h := snapshotPopulationUnitFixture(t, 1)
		h.pipelineErr = errors.New("lost discovery pipeline completion")
		if err := s.populateSnapshotIndex(ctx, m); err == nil {
			t.Fatal("pipeline error ignored")
		}
		if h.notifications != 0 {
			t.Fatal("mutated after failed discovery")
		}
	})
	t.Run("lost_notification_reply", func(t *testing.T) {
		s, ctx, m, h := snapshotPopulationUnitFixture(t, 257)
		h.notifyErr = errors.New("lost notification reply")
		if err := s.populateSnapshotIndex(ctx, m); err == nil {
			t.Fatal("unknown notification accepted")
		}
		if h.notifications != 1 || h.pages != 1 || h.counts != 1 {
			t.Fatal("replayed uncertain mutation", h.notifications, h.pages, h.counts)
		}
	})
}
