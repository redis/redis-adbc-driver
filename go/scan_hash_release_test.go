// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestHashReplyPageReleasesConsumedRows(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		for _, outcome := range []string{"complete", "error", "cancel"} {
			t.Run(map[bool]string{false: "ordinary", true: "snapshot"}[snapshot]+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				it, projection := hashProjectionFixture()
				it.meta.KeyPrefix = "logical:"
				it.env, it.projection = &evalEnv{ctx: ctx}, projection
				row := []any{"1", "9007199254740993", "a\x00b", nil, nil, "1234567890123456"}
				if snapshot {
					row = append(row, nil)
				}
				original := append([]any(nil), row...)
				values := [][]any{row, row, row}
				keys := []string{"logical:1", "logical:1", "logical:1"}
				var decoded [][]Value
				wantErr := errors.New("consumer stopped")
				projection.consumeHash = func(raw []any) error {
					for i := 0; i <= len(decoded); i++ {
						if values[i] != nil {
							t.Fatal("consumed reply row is still retained by the page", i)
						}
					}
					out := make([]Value, projection.width)
					if err := projection.decodeHashInto(raw, out); err != nil {
						return err
					}
					decoded = append(decoded, out)
					if outcome == "error" {
						return wantErr
					}
					if outcome == "cancel" {
						cancel()
					}
					return nil
				}
				var err error
				if snapshot {
					_, _, err = it.consumeSnapshotHashValues(ctx, keys, values)
				} else {
					_, _, err = it.consumeHashValues(keys, values)
				}
				if outcome == "complete" && (err != nil || len(decoded) != 3) ||
					outcome == "error" && !errors.Is(err, wantErr) ||
					outcome == "cancel" && !isStatus(err, adbc.StatusCancelled) {
					t.Fatal("unexpected consumer result", len(decoded), err)
				}
				for _, retained := range values {
					if retained != nil {
						t.Fatal("page retained a row on completion/error/cancellation")
					}
				}
				if !reflect.DeepEqual(row, original) {
					t.Fatal("the command's original reply slots were modified")
				}
				if decoded[0][0].S != "a\x00b" || decoded[0][1].I != 9007199254740993 {
					t.Fatal("decoded values changed after releasing the raw page")
				}
			})
		}
	}
}
