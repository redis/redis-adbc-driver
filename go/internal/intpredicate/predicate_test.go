// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0
package intpredicate

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"testing"
)

func TestParity(t *testing.T) {
	t.Logf("backend=%s", Backend())
	if expected := os.Getenv("INTEGER_SIMD_EXPECT"); expected != "" && Backend() != expected {
		t.Fatalf("expected %s hardware dispatch, got %s", expected, Backend())
	}
	edges := []int64{math.MinInt64, math.MinInt64 + 1, -9007199254740993, -1, 0, 1, 9007199254740993, math.MaxInt64 - 1, math.MaxInt64}
	rng := rand.New(rand.NewPCG(296, 1))
	for _, n := range []int{0, 1, 2, 3, 63, 64, 65, 127, 128, 129, 1024, 65535, 65536, 65537} {
		values := make([]int64, n)
		for i := range values {
			if i < len(edges) {
				values[i] = edges[i]
			} else {
				values[i] = int64(rng.Uint64())
			}
		}
		for _, density := range []int{0, 1, 50, 100} {
			valid := make([]uint64, (n+63)/64)
			for i := range values {
				if rng.IntN(100) >= density {
					valid[i/64] |= uint64(1) << uint(i%64)
				}
			}
			for _, rhs := range edges {
				for op := Equal; op <= GreaterEqual; op++ {
					got, err := Match(context.Background(), values, valid, rhs, op, false)
					want, werr := Match(context.Background(), values, valid, rhs, op, true)
					if err != nil || werr != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("n=%d null=%d rhs=%d op=%d mismatch", n, density, rhs, op)
					}
					// Independent boolean oracle, including tail padding and row order.
					for i, v := range values {
						expected := []bool{v == rhs, v != rhs, v < rhs, v <= rhs, v > rhs, v >= rhs}[op] && valid[i/64]&(uint64(1)<<uint(i%64)) != 0
						if (got[i/64]&(uint64(1)<<uint(i%64)) != 0) != expected {
							t.Fatalf("oracle row %d", i)
						}
					}
					if n%64 != 0 && got[len(got)-1]>>uint(n%64) != 0 {
						t.Fatal("padding bits set")
					}
				}
			}
		}
		got, _ := Match(context.Background(), values, nil, 0, GreaterEqual, false)
		want, _ := Match(context.Background(), values, nil, 0, GreaterEqual, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("all valid parity")
		}
	}
}

func TestContracts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := Match(ctx, []int64{1}, nil, 0, Equal, false); err == nil || out != nil {
		t.Fatal("cancelled result")
	}
	for _, scalar := range []bool{false, true} {
		if _, err := Match(context.Background(), []int64{1}, []uint64{}, 0, Equal, scalar); err == nil {
			t.Fatal("bad bitmap accepted")
		}
		if _, err := Match(context.Background(), nil, nil, 0, Op(99), scalar); err == nil {
			t.Fatal("bad operation accepted")
		}
		values := []int64{1, 2, 3}
		valid := []uint64{7}
		out, _ := Match(context.Background(), values, valid, 0, Greater, scalar)
		out[0] = 0
		again, _ := Match(context.Background(), values, valid, 0, Greater, scalar)
		if again[0] != 7 || valid[0] != 7 || values[0] != 1 {
			t.Fatal("output ownership/input mutated")
		}
	}
}

var sink []uint64

func BenchmarkMatch(b *testing.B) {
	for _, n := range []int{64, 1024, 65536} {
		for _, nulls := range []int{0, 50, 100} {
			for _, selectivity := range []int{0, 1, 50, 100} {
				v := make([]int64, n)
				valid := make([]uint64, (n+63)/64)
				for i := range v {
					v[i] = int64(i % 100)
					if i%100 >= nulls {
						valid[i/64] |= uint64(1) << uint(i%64)
					}
				}
				for _, scalar := range []bool{true, false} {
					backend := Backend()
					if scalar {
						backend = "scalar"
					}
					b.Run(fmt.Sprintf("%s/n=%d/null=%d/select=%d", backend, n, nulls, selectivity), func(b *testing.B) {
						b.ReportAllocs()
						b.SetBytes(int64(n * 8))
						for b.Loop() {
							var err error
							sink, err = Match(context.Background(), v, valid, int64(selectivity), Less, scalar)
							if err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

// Cancel between blocks to ensure no partial mask escapes after native work.
type blockCancelContext struct {
	context.Context
	checks int
}

func (c *blockCancelContext) Err() error {
	c.checks++
	if c.checks >= 3 {
		return context.Canceled
	}
	return nil
}
func TestCancellationBetweenBlocks(t *testing.T) {
	for _, scalar := range []bool{false, true} {
		ctx := &blockCancelContext{Context: context.Background()}
		out, err := Match(ctx, make([]int64, 65537), nil, 0, Equal, scalar)
		if err != context.Canceled || out != nil {
			t.Fatal("partial result escaped cancellation")
		}
	}
}
