// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestLiveCLI(t *testing.T) {
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run live CLI integration")
	}
	ctx := context.Background()
	raw := rawClient(t, uri)
	prefix := "it:cli:live:"
	deleteKeys(t, raw, prefix)
	t.Cleanup(func() { deleteKeys(t, raw, prefix) })
	if err := raw.HSet(ctx, prefix+"one", "n", "10").Err(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	args := []string{"live", "register", "-prefix", prefix, "-table", "it_cli_live"}
	if err := run(ctx, args, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var preview redis.LiveHashRegistration
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Applied {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	out.Reset()
	if err := run(ctx, append(args, "-apply"), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := redis.UnregisterLiveHashes(ctx, map[string]string{adbc.OptionKeyURI: uri}, "it_cli_live"); err != nil {
			t.Error(err)
		}
	})
	db, err := redis.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{adbc.OptionKeyURI: uri, redis.OptionStringLiveTables: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	stmt, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, "UPDATE redis_live.it_cli_live SET n=11"); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	if raw.HGet(ctx, prefix+"one", "n").Val() != "11" {
		t.Fatal("CLI registration did not expose writable original HASH")
	}
	out.Reset()
	if err := run(ctx, []string{"live", "unregister", "-table", "it_cli_live"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if raw.HGet(ctx, prefix+"one", "n").Val() != "11" {
		t.Fatal("unregister preview changed source")
	}
}
