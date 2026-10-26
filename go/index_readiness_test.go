// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestWaitIndexReady(t *testing.T) {
	calls := 0
	err := waitIndexReady(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errorf(adbc.StatusInvalidState, "building")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatal(calls, err)
	}
	for _, code := range []adbc.Status{adbc.StatusIO, adbc.StatusUnauthorized, adbc.StatusNotImplemented, adbc.StatusInvalidData} {
		calls = 0
		expected := errorf(code, "terminal")
		err = waitIndexReady(context.Background(), func(context.Context) error { calls++; return expected })
		if !isStatus(err, code) || err.Error() != expected.Error() || calls != 1 {
			t.Fatal("retried a terminal readiness error", calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	err = waitIndexReady(ctx, func(context.Context) error { calls++; cancel(); return errorf(adbc.StatusInvalidState, "building") })
	if calls != 1 || !isStatus(err, adbc.StatusCancelled) {
		t.Fatal(calls, err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err = waitIndexReady(ctx, func(context.Context) error { return errorf(adbc.StatusInvalidState, "building") })
	if !isStatus(err, adbc.StatusTimeout) {
		t.Fatal(err)
	}
}

func TestInconsistentIndexState(t *testing.T) {
	for _, message := range []string{"Inconsistent index state", "SEARCH_VALUE_BAD Inconsistent index state"} {
		if !inconsistentIndexState(readinessServerError(message)) || !inconsistentIndexState(fmt.Errorf("wrapped: %w", readinessServerError(message))) {
			t.Fatal("missed known server error", message)
		}
		if inconsistentIndexState(errors.New(message)) {
			t.Fatal("classified a transport error as transient", message)
		}
	}
	for _, err := range []error{nil, readinessServerError("NOPERM Inconsistent index state"), readinessServerError("Unknown index name"), readinessServerError("ERR Inconsistent index state"), readinessServerError("Inconsistent index state: permanent")} {
		if inconsistentIndexState(err) {
			t.Fatal("classified an unrelated error as transient", err)
		}
	}
}
