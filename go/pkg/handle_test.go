// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
//go:build driverlib

package main

import (
	"runtime"
	"runtime/cgo"
	"sync"
	"testing"
)

func TestHandleHolderSurvivesGCAndDeletesRegistryEntry(t *testing.T) {
	value := &struct{ text string }{text: "retained owner"}
	h := cgo.NewHandle(value)
	ptr := createHandle(h)
	if ptr == nil {
		t.Fatal("missing C holder")
	}
	runtime.GC()
	if got := handleFromPtr(ptr).Value(); got != value {
		t.Fatal("holder lost registry identity")
	}
	if got := takeHandle(ptr); got != value {
		t.Fatal("release lost owner")
	}
	// Check the captured integer, not the freed C holder.
	defer func() {
		if recover() == nil {
			t.Error("registry entry survived release")
		}
	}()
	h.Value()
}

func TestIndependentHandleHolders(t *testing.T) {
	var workers sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 128; i++ {
				value := &struct{ worker, iteration int }{worker, i}
				ptr := createHandle(cgo.NewHandle(value))
				if got := getFromHandle[struct{ worker, iteration int }](ptr); got != value {
					t.Error("independent holder aliased another owner")
				}
				if got := takeHandle(ptr); got != value {
					t.Error("independent release lost owner")
				}
			}
		}(worker)
	}
	workers.Wait()
}
