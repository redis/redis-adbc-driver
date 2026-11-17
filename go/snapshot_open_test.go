// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Model a first opener whose initial catalog read completed before another
// opener initialized the database and published its first table. Later reads
// use the real server state, without depending on scheduler timing.
type snapshotStaleOpen struct {
	snapshotHook
	databaseSize bool
	formatError  error
	observed     bool
}

func (h *snapshotStaleOpen) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if h.formatError != nil && h.observed && cmd.Name() == "hget" && len(cmd.Args()) == 3 && fmt.Sprint(cmd.Args()[1]) == snapshotCatalogKey && fmt.Sprint(cmd.Args()[2]) == "format" {
			cmd.SetErr(h.formatError)
			return h.formatError
		}
		if err := next(ctx, cmd); err != nil {
			return err
		}
		if cmd.Name() == "exists" && len(cmd.Args()) == 2 && fmt.Sprint(cmd.Args()[1]) == snapshotCatalogKey && !h.observed {
			h.observed = true
			cmd.(*goredis.IntCmd).SetVal(0)
		}
		if h.databaseSize && cmd.Name() == "ft._list" {
			// The competing publication followed this empty index-list read.
			cmd.(interface{ SetVal(any) }).SetVal([]any{})
		}
		return nil
	}
}

func TestSnapshotOpenConcurrentInitialization(t *testing.T) {
	for _, databaseSize := range []bool{false, true} {
		t.Run(fmt.Sprintf("database_size_%t", databaseSize), func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			h.exec("CREATE TABLE initialized(v INTEGER)")
			h.exec("INSERT INTO initialized VALUES(42)")
			before, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
			if err != nil {
				t.Fatal(err)
			}
			client := goredis.NewClient(raw.Options())
			defer client.Close()
			hook := &snapshotStaleOpen{databaseSize: databaseSize}
			client.AddHook(hook)
			s := &store{client: client}
			if err := s.openSnapshotCatalog(h.ctx, true); err != nil {
				t.Fatal("concurrent initializer rejected a compatible catalog:", err)
			}
			if !hook.observed {
				t.Fatal("missing stale catalog observation")
			}
			after, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
			if err != nil || !maps.Equal(before, after) {
				t.Fatal("opening changed the competing initializer's catalog", err)
			}
			h.expectRows("SELECT v FROM initialized", "42")
		})
	}
}

func TestSnapshotOpenConcurrentInitializationRefusals(t *testing.T) {
	fault := errors.Join(goredis.Nil, errors.New("lost catalog format reply"))
	for _, tc := range []struct {
		name   string
		format string
		fault  error
	}{
		{name: "older_format", format: "16"},
		{name: "unknown_format_reply", format: "17", fault: fault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			h.exec("CREATE TABLE initialized(v INTEGER)")
			h.exec("INSERT INTO initialized VALUES(42)")
			if err := raw.HSet(h.ctx, snapshotCatalogKey, "format", tc.format).Err(); err != nil {
				t.Fatal(err)
			}
			defer raw.HSet(h.ctx, snapshotCatalogKey, "format", "17")
			before, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
			if err != nil {
				t.Fatal(err)
			}
			client := goredis.NewClient(raw.Options())
			defer client.Close()
			client.AddHook(&snapshotStaleOpen{formatError: tc.fault})
			err = (&store{client: client}).openSnapshotCatalog(h.ctx, true)
			if tc.fault != nil {
				if !errors.Is(err, tc.fault) {
					t.Fatal("lost format-read failure:", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "unsupported snapshot catalog format") {
				t.Fatal("old format was not rejected:", err)
			}
			after, readErr := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
			if readErr != nil || !maps.Equal(before, after) {
				t.Fatal("refused open modified catalog", readErr)
			}
			if err := raw.HSet(h.ctx, snapshotCatalogKey, "format", "17").Err(); err != nil {
				t.Fatal(err)
			}
			h.expectRows("SELECT v FROM initialized", "42")
		})
	}
}
