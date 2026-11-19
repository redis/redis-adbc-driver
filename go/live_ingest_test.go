// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"testing"
)

func TestLiveIngestSourceOption(t *testing.T) {
	s := &statementImpl{}
	ctx := context.Background()
	for _, bad := range []string{`{}`, `null`, `{"prefix":"adbc:{meta}:"}`, `{"prefix":"app:","index":"mine"}`, `{"prefix":"app:","replace":{}}`, `{"prefix":"app:","unknown":true}`, `{"prefix":"app:"} {}`, `{"prefix":"app:","encodings":{"__key":"text"}}`} {
		if err := s.SetOption(ctx, OptionStringIngestLiveSource, bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	for _, good := range []string{`{"prefix":"app:"}`, `{"prefix":"app:","index":"adbc_live1_{meta}_identity","replace":{"schema":"redis_live","table":"t","exclusive_quiescent":true,"discard_ttls":true,"reset_index":true}}`, ""} {
		if err := s.SetOption(ctx, OptionStringIngestLiveSource, good); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetOption(ctx, OptionStringIngestLiveSource); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveIngestTextIndexPolicy(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "active", Type: arrow.FixedWidthTypes.Boolean}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	reader, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	for _, policy := range []map[string]bool{nil, {"active": true, "n": true}} {
		s := &statementImpl{params: reader, indexColumns: policy}
		meta, err := s.newSourceIngestMeta(liveSchema, &unmanagedIngestSource{Prefix: "app:", Encodings: map[string]string{"active": "text"}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Columns[0].Indexed || !meta.Columns[1].Indexed {
			t.Fatal("live text encoding indexed as a native numeric field")
		}
	}
}
