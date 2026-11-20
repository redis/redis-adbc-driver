// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"testing"
)

func TestUnmanagedPartitionReadExactRefusalProofs(t *testing.T) {
	native := controlScriptNativeError("WRONGTYPE changed")
	for _, which := range []string{"direct", "wrapped_command", "joined_command", "wrapped_pipeline", "joined_pipeline", "unaccounted_pipeline"} {
		t.Run(which, func(t *testing.T) {
			r, h := projectionReadUnit(t)
			h.step = func(_ int, cmds []goredis.Cmder) error {
				setProjectionNative(cmds[0], nil, native)
				setProjectionNative(cmds[1], map[string]string{"n": "9007199254740993"}, nil)
				switch which {
				case "wrapped_command":
					cmds[0].SetErr(fmt.Errorf("unknown: %w", native))
				case "joined_command":
					cmds[0].SetErr(errors.Join(native, io.ErrUnexpectedEOF))
				case "wrapped_pipeline":
					return fmt.Errorf("unknown: %w", native)
				case "joined_pipeline":
					return errors.Join(native, io.ErrUnexpectedEOF)
				case "unaccounted_pipeline":
					setProjectionNative(cmds[0], nil, nil)
				}
				return native
			}
			rows, err := r.store.fetchUnmanagedPartitionRows(context.Background(), []string{"app:gone", "app:present"})
			if which == "direct" {
				if err != nil || len(rows) != 2 || rows[0] != nil || rows[1]["n"] != "9007199254740993" || rows[1][keyField] != "app:present" {
					t.Fatal(rows, err)
				}
			} else if err == nil || rows != nil {
				t.Fatal("composed/unaccounted refusal became successful omission", rows, err)
			}
			if h.calls != 1 {
				t.Fatal("uncertain read replayed", h.calls)
			}
		})
	}
}
