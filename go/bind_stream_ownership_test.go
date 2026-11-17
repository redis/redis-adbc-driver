// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
)

type bindStreamReferences struct {
	array.RecordReader
	refs int
}

func (r *bindStreamReferences) Retain() {
	if r.refs <= 0 {
		panic("retaining a released reader")
	}
	r.refs++
}
func (r *bindStreamReferences) Release() {
	r.refs--
	if r.refs < 0 {
		panic("double release")
	}
}

func TestBindStreamRetainsCallerReferenceAndRebinds(t *testing.T) {
	ctx := context.Background()
	s := &statementImpl{}
	r := &bindStreamReferences{refs: 1}
	if err := s.BindStream(ctx, r); err != nil {
		t.Fatal(err)
	}
	if r.refs != 2 {
		t.Fatalf("BindStream must retain the reader: refs=%d", r.refs)
	}
	// Retain before releasing the previous binding, including the same reader.
	r.Release()
	if err := s.BindStream(ctx, r); err != nil {
		t.Fatal(err)
	}
	if r.refs != 1 {
		t.Fatalf("same-reader rebind lost ownership: refs=%d", r.refs)
	}
	if err := s.BindStream(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if r.refs != 0 || s.params != nil {
		t.Fatal("unbind retained the reader", r.refs)
	}
	s.clearParams()
}
func TestBindStreamRefusalPreservesCallerReference(t *testing.T) {
	for _, s := range []*statementImpl{{closed: true}, {substrait: &subPlan{}}} {
		r := &bindStreamReferences{refs: 1}
		if err := s.BindStream(context.Background(), r); err == nil {
			t.Fatal("expected refusal")
		}
		if r.refs != 1 {
			t.Fatalf("refusal consumed caller reference: refs=%d", r.refs)
		}
		r.Release()
	}
}
