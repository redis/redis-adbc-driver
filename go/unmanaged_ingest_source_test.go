// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"
	"testing"
)

func TestUnmanagedSourceOption(t *testing.T) {
	for _, value := range []string{`{}`, `null`, `{"prefix":"app:"}`, `{"prefix":"adbc:{meta}:","index":"i"}`, `{"prefix":"app:","index":"adbc_snapshot17_bad"}`, `{"prefix":"app:","index":"i","typo":1}`, `{"prefix":"app:","index":"i"} {}`, `{"prefix":"app:","index":"i","encodings":{"x":"invalid"}}`, strings.Repeat("x", 65537)} {
		if _, err := parseUnmanagedIngestSource(value); err == nil {
			t.Fatalf("accepted %q", value[:min(80, len(value))])
		}
	}
	s := &statementImpl{}
	value := `{"prefix":"app:","index":"app_idx","encodings":{"b":"text"}}`
	// The impl needs an open connection only for SetOption; parser/getter are pure.
	cfg, err := parseUnmanagedIngestSource(value)
	if err != nil {
		t.Fatal(err)
	}
	s.unmanagedSource = cfg
	got, err := s.GetOption(context.Background(), OptionStringIngestUnmanagedSource)
	if err != nil || got != value {
		t.Fatalf("get %q %v", got, err)
	}
	if cfg, err := parseUnmanagedIngestSource(""); err != nil || cfg != nil {
		t.Fatal("empty did not clear", err)
	}
}
