// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"errors"
	"fmt"
	"testing"
)

func TestSnapshotAggregateLimitRefusal(t *testing.T) {
	for _, prefix := range []string{"SEARCH_LIMIT_OVER ", ""} {
		message := prefix + "LIMIT exceeds maximum of 137"
		for _, tc := range []struct {
			name string
			err  error
			ok   bool
		}{
			{"native", controlScriptNativeError(message), true},
			{"plain", errors.New(message), false},
			{"wrapped", fmt.Errorf("transport: %w", controlScriptNativeError(message)), false},
			{"joined", errors.Join(controlScriptNativeError(message), errors.New("lost reply")), false},
			{"suffix", controlScriptNativeError(message + " after execution"), false},
			{"other reducer", controlScriptNativeError("SEARCH_LIMIT_OVER Aggregate GROUPBY exceeded MAX_AGGREGATE_GROUPS limit of 137 groups"), false},
			{"negative", controlScriptNativeError(prefix + "LIMIT exceeds maximum of -1"), false},
			{"overflow", controlScriptNativeError(prefix + "LIMIT exceeds maximum of 9223372036854775808"), false},
			{"noncanonical", controlScriptNativeError(prefix + "LIMIT exceeds maximum of 0137"), false},
			{"not lower", controlScriptNativeError(prefix + "LIMIT exceeds maximum of 2147483648"), false},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				cap, ok := snapshotAggregateLimitRefusal(tc.err)
				if ok != tc.ok || ok && cap != 137 {
					t.Fatalf("cap=%d ok=%v", cap, ok)
				}
			})
		}
	}
}
