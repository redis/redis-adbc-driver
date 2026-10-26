// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestRetainedOptionsRejectInvalidWithoutChangingPolicy(t *testing.T) {
	ctx := context.Background()
	database := &databaseImpl{}
	statement := &statementImpl{}
	for key, values := range map[string][]string{
		OptionStringPartitionResultNamespace:    {"bad{slot}", "a:b", strings.Repeat("n", 65)},
		OptionStringPartitionReaderOnly:         {"1", "TRUE", ""},
		OptionStringPartitionControlPersistence: {"", "true", "allkeys-lru"},
		OptionStringPartitionResultLimits:       {"null", "[]", "{}{}", `{"unknown":1}`, `{"job_bytes":0}`, `{"chunks":1025}`, `{"total_bytes":1}`, strings.Repeat(" ", 4097)},
	} {
		for _, value := range values {
			if err := database.SetOption(ctx, key, value); !isStatus(err, adbc.StatusInvalidArgument) {
				t.Fatal(key, value, err)
			}
		}
	}
	if database.retainedNamespace != "" || database.readerOnly || database.partitionControlPersistence != "" || database.resultLimits() != defaultRetainedLimits() {
		t.Fatal("invalid options changed database policy")
	}
	for key, values := range map[string][]string{
		OptionStringPartitionResultMode:      {"", "snapshot", "RETAINED"},
		OptionIntPartitionResultTTL:          {"0", "86401", "1s", "-1"},
		OptionStringPartitionExecutionLimits: {"null", `{"work":0}`, `{"working_bytes":1073741825}`, `{"source_rows":1048577}`, `{"unknown":1}`},
	} {
		for _, value := range values {
			if err := statement.SetOption(ctx, key, value); !isStatus(err, adbc.StatusInvalidArgument) {
				t.Fatal(key, value, err)
			}
		}
	}
	if statement.resultMode() != "live" || statement.resultTTL() != 5*time.Minute || statement.executionLimits() != defaultRetainedExecutionLimits() {
		t.Fatal("invalid options changed execution policy")
	}
	if err := statement.SetOption(ctx, OptionStringPartitionExecutionLimits, `{"source_bytes":134217728}`); err != nil {
		t.Fatal(err)
	}
	limits := defaultRetainedExecutionLimits()
	limits.SourceBytes = 128 << 20
	if statement.executionLimits() != limits {
		t.Fatal("partial override lost defaults")
	}
}

func TestRetainedControlPolicyPreservesPersistentQuotas(t *testing.T) {
	for _, policy := range []string{"noeviction", "volatile-lru", "volatile-lfu", "volatile-random", "volatile-ttl", "volatile-lrm"} {
		if !retainedControlPolicyAllowed(policy) {
			t.Fatal(policy)
		}
	}
	for _, policy := range []string{"", "allkeys-lru", "allkeys-lfu", "allkeys-random", "allkeys-lrm", "unknown"} {
		if retainedControlPolicyAllowed(policy) {
			t.Fatal(policy)
		}
	}
}
