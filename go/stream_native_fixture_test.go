// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"testing"
)

// No endpoint is contacted. This is an ordinary managed snapshot-directory
// transport fixture, independent of Flex optimizations or their test helpers.
type streamNativeReplies struct {
	commands int
	segments map[string]snapshotSegment
	rows     map[string]aggRow
}

func (h *streamNativeReplies) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *streamNativeReplies) reply(cmd goredis.Cmder) error {
	h.commands++
	switch cmd.Name() {
	case "fcall":
		cmd.(onceCmd).SetVal(int64(1))
	case "hmget":
		row := h.rows[fmt.Sprint(cmd.Args()[1])]
		values := make([]any, len(cmd.Args())-2)
		for i, f := range cmd.Args()[2:] {
			if v, ok := row[fmt.Sprint(f)]; ok {
				values[i] = v
			}
		}
		cmd.(*goredis.SliceCmd).SetVal(values)
	case "zcard":
		for _, seg := range h.segments {
			if fmt.Sprint(cmd.Args()[1]) == snapshotMembers(snapshotDataID(seg.Prefix)) {
				cmd.(*goredis.IntCmd).SetVal(seg.Rows)
				return nil
			}
		}
		return errors.New("unknown synthetic directory")
	case "hget":
		row := h.rows[fmt.Sprint(cmd.Args()[1])]
		v, ok := row[fmt.Sprint(cmd.Args()[2])]
		if !ok {
			return goredis.Nil
		}
		cmd.(*goredis.StringCmd).SetVal(v)
	default:
		return fmt.Errorf("unexpected synthetic stream command %s", cmd.Name())
	}
	return nil
}
func (h *streamNativeReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, c goredis.Cmder) error {
		err := h.reply(c)
		if err != nil {
			c.SetErr(err)
		}
		return err
	}
}
func (h *streamNativeReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cs []goredis.Cmder) error {
		var es []error
		for _, c := range cs {
			if err := h.reply(c); err != nil {
				c.SetErr(err)
				es = append(es, err)
			}
		}
		if len(es) == 1 {
			return es[0]
		}
		return errors.Join(es...)
	}
}
func streamNativeFixture(t *testing.T) (*snapshotDeltaScan, context.Context, *streamNativeReplies) {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = c.Close() })
	s := &store{client: c, snapshots: true}
	access, id := snapshotID(), snapshotID()
	n := int64(3)
	seg := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, access), Index: snapshotIndexPrefix(id) + "working", Rows: n}
	m := &tableMeta{Schema: defaultSchema, SnapshotAccessID: access, KeyPrefix: seg.Prefix, IndexName: seg.Index, SnapshotRows: &n, SnapshotOverlay: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s, pin: &snapshotPin{store: s}})
	h := &streamNativeReplies{segments: map[string]snapshotSegment{seg.Index: seg}, rows: map[string]aggRow{}}
	c.AddHook(h)
	return newSnapshotDeltaScan(s, m, nil), ctx, h
}
