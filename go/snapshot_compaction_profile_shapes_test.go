// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotCompactionProfilePipelineShapes(t *testing.T) {
	ctx := context.Background()
	p := &compactionPhaseProfile{phase: "copy", phases: map[string]*compactionProfilePhase{
		"copy": {RoundTrips: map[string]*compactionProfileRoundTrips{}},
	}}
	hook := p.ProcessPipelineHook(func(context.Context, []goredis.Cmder) error { return nil })
	const secret = "private-fixture-key-and-value"
	for _, commands := range [][]goredis.Cmder{
		{goredis.NewCmd(ctx, "hget", secret, secret), goredis.NewCmd(ctx, "zcard", secret), goredis.NewCmd(ctx, "hlen", secret)},
		{goredis.NewCmd(ctx, "hmget", secret, secret), goredis.NewCmd(ctx, "exists", secret)},
		{goredis.NewCmd(ctx, "hgetall", secret), goredis.NewCmd(ctx, "hgetall", secret)},
	} {
		if err := hook(ctx, commands); err != nil {
			t.Fatal(err)
		}
	}
	stats := p.phases["copy"]
	for _, shape := range []string{"hget,zcard,hlen", "hmget,exists", "hgetall*2"} {
		if stats.PipelineShapes[shape] != 1 {
			t.Fatalf("missing distinct proof/image pipeline %q: %v", shape, stats.PipelineShapes)
		}
	}
	var calls, commands int
	for _, count := range stats.RoundTrips {
		calls += count.Calls
		commands += count.Commands
	}
	encoded, err := json.Marshal(stats)
	if err != nil || strings.Contains(string(encoded), secret) || calls != 3 || commands != 7 {
		t.Fatalf("profile privacy or original accounting changed: calls=%d commands=%d err=%v", calls, commands, err)
	}
	p.phase = ""
	if err := hook(ctx, []goredis.Cmder{goredis.NewCmd(ctx, "hgetall", secret)}); err != nil {
		t.Fatal(err)
	}
	if stats.PipelineShapes["hgetall"] != 0 {
		t.Fatal("observer calls outside a phase must remain excluded")
	}
}
