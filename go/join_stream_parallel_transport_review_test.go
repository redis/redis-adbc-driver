// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type parallelHashReviewReplies struct {
	*parallelNativeReplies
	fault string
}

func (h *parallelHashReviewReplies) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		hash := false
		for _, cmd := range cmds {
			if err := h.replyParallel(cmd); err != nil {
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
				return errors.Join(goredis.Nil, errors.New("synthetic parallel HASH reply loss"))
			case "wrapped_nil":
				return fmt.Errorf("synthetic parallel pipeline uncertainty: %w", goredis.Nil)
			}
		}
		return nil
	}
}

// Both workers use the actual exact-range scan and HASH helper. All individual
// replies are valid; the pipeline-wide uncertainty alone must fail closed and
// release the complete worker grant. No cohort implementation is required.
func TestParallelStarReviewHashPipelineUncertaintyFailsClosed(t *testing.T) {
	for _, fault := range []string{"direct_nil", "composed_nil_transport", "wrapped_nil"} {
		t.Run(fault, func(t *testing.T) {
			e, ctx, req, base := parallelNativeStarFixture(t)
			hook := &parallelHashReviewReplies{parallelNativeReplies: base, fault: fault}
			client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
			t.Cleanup(func() { _ = client.Close() })
			client.AddHook(hook)
			e.store.client = client
			count := 0
			handled, err := e.scanStarJoinPages(ctx, req, nil, func(rows []map[string]Value) error { count += len(rows); return nil })
			if !handled || e.workerGrant.Load() != 2 || e.workerBudget.option(OptionIntQueryWorkersActive) != "0" || e.workerBudget.option(OptionIntQueryWorkersQueued) != "0" {
				t.Fatalf("actual parallel path/grant cleanup failed: handled=%v err=%v count=%d", handled, err, count)
			}
			if fault == "direct_nil" {
				if err != nil || count != 8194 {
					t.Fatalf("ordinary direct-Nil tolerance changed: count=%d err=%v", count, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "synthetic parallel") || count != 0 {
				t.Fatalf("uncertain populated HASH pipeline became successful output: count=%d err=%v", count, err)
			}
		})
	}
}
