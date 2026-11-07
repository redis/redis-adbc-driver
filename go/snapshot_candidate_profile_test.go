// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Zero-latency exact-membership replies isolate client merge allocation cost.
// Every candidate occurs in every immutable source, including the newest.
// This is not a Redis throughput/latency benchmark; real probes remain covered
// by Software profiles and native complete-result comparisons.
type snapshotCandidateProfileClient struct{ goredis.UniversalClient }
type snapshotCandidateProfilePipeline struct {
	goredis.Pipeliner
	commands []goredis.Cmder
}

func (*snapshotCandidateProfileClient) Pipeline() goredis.Pipeliner {
	return &snapshotCandidateProfilePipeline{}
}
func (p *snapshotCandidateProfilePipeline) Do(ctx context.Context, args ...any) *goredis.Cmd {
	if args[0] != "ZMSCORE" {
		panic("unexpected candidate profile command")
	}
	cmd := goredis.NewCmd(ctx, args...)
	scores := make([]any, len(args)-2)
	for i := range scores {
		scores[i] = float64(0)
	}
	cmd.SetVal(scores)
	p.commands = append(p.commands, cmd)
	return cmd
}
func (p *snapshotCandidateProfilePipeline) Exec(context.Context) ([]goredis.Cmder, error) {
	return p.commands, nil
}

func BenchmarkSnapshotCandidateMerge(b *testing.B) {
	for _, segments := range []int{1, 4, 8} {
		b.Run(fmt.Sprint(segments), func(b *testing.B) {
			d := snapshotCandidateScan{store: &store{client: &snapshotCandidateProfileClient{}}, meta: &tableMeta{KeyPrefix: "logical:"}}
			wantLogical, wantPhysical := make([]string, snapshotDeltaPage), make([]string, snapshotDeltaPage)
			for s := 0; s < segments; s++ {
				src := snapshotCandidateSource{segment: snapshotSegment{Prefix: fmt.Sprintf("segment%d:", s)}, done: true}
				for i := 1; i <= snapshotDeltaPage; i++ {
					src.ids = append(src.ids, fmt.Sprintf("%020d", i))
				}
				d.sources = append(d.sources, src)
			}
			for i := range wantLogical {
				wantLogical[i] = fmt.Sprint("logical:", i+1)
				wantPhysical[i] = fmt.Sprintf("segment%d:%d", segments-1, i+1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for s := range d.sources {
					d.sources[s].at = 0
				}
				logical, physical, err := d.nextKeys(context.Background())
				if err != nil || len(logical) != snapshotDeltaPage {
					b.Fatal("incomplete candidate merge", err)
				}
				for i := range logical {
					if logical[i] != wantLogical[i] || physical[i] != wantPhysical[i] {
						b.Fatal("candidate identity or newest winner differs")
					}
				}
			}
		})
	}
}
