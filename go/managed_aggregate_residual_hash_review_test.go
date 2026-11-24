// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Synthetic command replies exercise the real scalar scan and callback wiring.
// No socket or database is opened; an unexpected dial/command fails the test.
type managedResidualReviewReplies struct{ hmget int }

func (*managedResidualReviewReplies) DialHook(goredis.DialHook) goredis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("unexpected network dial in scalar residual unit test")
	}
}
func (h *managedResidualReviewReplies) reply(cmd goredis.Cmder) error {
	switch cmd.Name() {
	case "ft.aggregate":
		cmd.(interface{ SetVal(any) }).SetVal([]any{[]any{int64(3),
			[]any{"__key", "review:1", rowIDField, "1"},
			[]any{"__key", "review:2", rowIDField, "2"},
			[]any{"__key", "review:3", rowIDField, "3"}}, int64(0)})
	case "hmget":
		h.hmget++
		args := cmd.Args()
		values := map[string]string{"review:1": "9007199254740992", "review:2": "9007199254740993"}
		row := make([]any, len(args)-2)
		for i, f := range args[2:] {
			switch fmt.Sprint(f) {
			case rowIDField:
				row[i] = fmt.Sprint(args[1])[len("review:"):]
			case "id":
				if value, ok := values[fmt.Sprint(args[1])]; ok {
					row[i] = value
				}
			default:
				return fmt.Errorf("unexpected native field %v", f)
			}
		}
		cmd.(*goredis.SliceCmd).SetVal(row)
	default:
		return fmt.Errorf("unexpected scalar residual command %s", cmd.Name())
	}
	return nil
}
func (h *managedResidualReviewReplies) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error { return h.reply(cmd) }
}
func (h *managedResidualReviewReplies) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestManagedAggregateResidualNativeHashCannotBypassPredicate(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = client.Close() })
	replies := &managedResidualReviewReplies{}
	client.AddHook(replies)
	meta := &tableMeta{Schema: defaultSchema, Name: "review", KeyPrefix: "review:", IndexName: "review-index", Columns: []columnMeta{
		{Name: "id", Type: typeInt64, Indexed: true}, {Name: "unused1", Type: typeString}, {Name: "unused2", Type: typeString},
	}}
	diagnostics := &executionDiagnostics{}
	e := &executor{store: &store{client: client}, sess: newSession(defaultSchema), cache: newExecCache(), diagnostics: diagnostics}
	req := scanRequest{meta: meta, need: map[string]bool{"id": true}, where: wherePlan{query: "*", residual: filteredComparison("=", "id", 9007199254740993)}}
	var consumed []int64
	used, err := e.scanScalarAggregateValues(context.Background(), req, nil, []*Func{{Name: "COUNT", Star: true}}, func(row []Value) error {
		consumed = append(consumed, row[0].I)
		return nil
	})
	if err != nil || !used {
		t.Fatalf("actual scalar aggregate path: used=%v error=%v", used, err)
	}
	if !reflect.DeepEqual(consumed, []int64{2}) {
		t.Fatalf("positional HASH bypassed exact residual or NULL rejection: consumed row IDs %v", consumed)
	}
	if replies.hmget != 3 {
		t.Fatalf("expected authoritative native values for all candidates, fetched %d", replies.hmget)
	}
	if diagnostics.processed.Load() != 3 || diagnostics.matched.Load() != 1 {
		t.Fatalf("predicate diagnostics processed=%d matched=%d", diagnostics.processed.Load(), diagnostics.matched.Load())
	}
}
