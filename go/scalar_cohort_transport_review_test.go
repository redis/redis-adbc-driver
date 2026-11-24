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

type cohortTransportReviewReplies struct {
	*scalarCohortReplies
	fault string
}

func (h *cohortTransportReviewReplies) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		hash := false
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
			hash = hash || cmd.Name() == "hmget"
		}
		if hash {
			switch h.fault {
			case "direct_nil":
				return goredis.Nil
			case "composed_nil_transport":
				return errors.Join(goredis.Nil, errors.New("synthetic HMGET reply loss"))
			case "wrapped_nil":
				return fmt.Errorf("synthetic uncertain pipeline: %w", goredis.Nil)
			}
		}
		return nil
	}
}

// Exercise the parsed CASE/cohort/raw-source path with valid individual reply
// values. A pipeline-wide uncertainty must not publish a successful lane just
// because errors.Is can also find redis.Nil among its causes.
func TestScalarCohortReviewAmbiguousHashPipelineFailsClosed(t *testing.T) {
	for _, fault := range []string{"direct_nil", "composed_nil_transport", "wrapped_nil"} {
		t.Run(fault, func(t *testing.T) {
			e, ctx, base, _ := cohortFixture(t, 1)
			replies := &cohortTransportReviewReplies{scalarCohortReplies: base, fault: fault}
			client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
			t.Cleanup(func() { _ = client.Close() })
			client.AddHook(replies)
			e.store.client = client
			p := cohortPlan(t, e, ctx, cohortParsedCase)
			c := oneCohort(t, e)
			rows, err := e.runSelect(ctx, p, nil)
			if fault == "direct_nil" {
				if err != nil || len(rows) != 1 || rows[0][0].Text() != "10.0000000000000000" || len(e.cache.sub) != 2 {
					t.Fatalf("ordinary direct-Nil tolerance changed: %v %v", rows, err)
				}
			} else {
				if err == nil || len(e.cache.sub) != 0 || c.sourceErr == nil {
					t.Fatalf("uncertain populated HASH replies became cached success: rows=%v err=%v cached=%d sourceErr=%v", rows, err, len(e.cache.sub), c.sourceErr)
				}
			}
			if !c.started || c.scans != 1 || base.fetches == 0 {
				t.Fatal("regression did not exercise actual cohort HASH source")
			}
		})
	}
}
