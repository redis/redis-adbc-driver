// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type readerCacheClient struct {
	*goredis.Client
	t                 *testing.T
	names             []string
	lookups, controls int
	lookupError       error
	step              func(goredis.Cmder) error
}

func (c *readerCacheClient) Process(ctx context.Context, cmd goredis.Cmder) error {
	c.t.Helper()
	if marker, ok := cmd.(interface{ NoRetry() bool }); !ok || !marker.NoRetry() {
		c.t.Fatal("cache command can retry unknown outcomes")
	}
	a := cmd.Args()
	set := cmd.(interface{ SetVal(any) })
	if len(a) > 1 && a[0] == "EVAL" && a[1] == snapshotNamespaceRead {
		c.lookups++
		if c.lookupError != nil {
			cmd.SetErr(c.lookupError)
			return c.lookupError
		}
		var names []any
		for _, name := range c.names {
			names = append(names, name)
		}
		set.SetVal(names)
		return nil
	}
	if len(a) > 1 && (a[0] == "EVALSHA" && a[1] == snapshotControlDigest || a[0] == "EVAL" && (a[1] == snapshotControl || a[1] == snapshotReaderControl)) {
		c.controls++
		if c.step != nil {
			if err := c.step(cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
		}
		set.SetVal("0123456789abcdef0123456789abcdef")
		return nil
	}
	c.t.Fatalf("unexpected command %v", a[0])
	return nil
}
func newReaderCacheClient(t *testing.T) *readerCacheClient {
	c := &readerCacheClient{Client: goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"}), t: t}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSnapshotReaderKeyCacheStableAdmission(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(fmt.Sprint(reader), func(t *testing.T) {
			c := newReaderCacheClient(t)
			s := &store{client: c, restrictedReader: reader, readerNamespace: snapshotReaderNamespace("test-domain")}
			if reader {
				c.names = []string{s.readerNamespace}
			}
			for i := 0; i < 4; i++ {
				if err := s.snapshotControl(context.Background(), "acquire", snapshotID(), "", "").Err(); err != nil {
					t.Fatal(err)
				}
			}
			if c.lookups != 1 || c.controls != 4 {
				t.Fatalf("directory reads=%d controls=%d", c.lookups, c.controls)
			}
		})
	}
}

func TestSnapshotReaderKeyCacheRefreshesOnlyNativeRefusal(t *testing.T) {
	native := controlScriptNativeError("reader namespace changed before mutation")
	for _, tc := range []struct {
		name  string
		cause error
		retry bool
	}{
		{"native", native, true},
		{"plain", errors.New(native.Error()), false},
		{"wrapped", fmt.Errorf("transport: %w", native), false},
		{"joined", errors.Join(native, io.EOF), false},
		{"EOF", io.ErrUnexpectedEOF, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newReaderCacheClient(t)
			s := &store{client: c}
			if _, err := s.snapshotReaderKeys(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			c.lookups = 0
			c.names = []string{snapshotReaderNamespace("new-domain")}
			c.step = func(goredis.Cmder) error {
				if c.controls == 1 {
					return tc.cause
				}
				return nil
			}
			err := s.snapshotControl(context.Background(), "acquire", snapshotID(), "", "").Err()
			if tc.retry {
				if err != nil || c.lookups != 1 || c.controls != 2 {
					t.Fatal(err, c.lookups, c.controls)
				}
			} else if err != tc.cause || c.lookups != 0 || c.controls != 1 {
				t.Fatal(err, c.lookups, c.controls)
			}
		})
	}
}

func TestSnapshotReaderKeyCacheCancellationStopsRefresh(t *testing.T) {
	c := newReaderCacheClient(t)
	s := &store{client: c}
	if _, err := s.snapshotReaderKeys(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	c.lookups = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.step = func(goredis.Cmder) error {
		cancel()
		return controlScriptNativeError("reader namespace changed before mutation")
	}
	if err := s.snapshotControl(ctx, "acquire", snapshotID(), "", "").Err(); err != context.Canceled || c.lookups != 0 || c.controls != 1 {
		t.Fatal(err, c.lookups, c.controls)
	}
}

func TestSnapshotReaderKeyCacheDiscoveryFailureStopsAdmission(t *testing.T) {
	c := newReaderCacheClient(t)
	c.lookupError = io.ErrUnexpectedEOF
	s := &store{client: c}
	if err := s.snapshotControl(context.Background(), "acquire", snapshotID(), "", "").Err(); err != io.ErrUnexpectedEOF || c.lookups != 1 || c.controls != 0 {
		t.Fatal(err, c.lookups, c.controls)
	}
}

func TestSnapshotReaderKeyCacheChurnIsBounded(t *testing.T) {
	for _, reader := range []bool{false, true} {
		t.Run(fmt.Sprint(reader), func(t *testing.T) {
			c := newReaderCacheClient(t)
			s := &store{client: c, restrictedReader: reader, readerNamespace: snapshotReaderNamespace("churn-domain")}
			c.step = func(goredis.Cmder) error {
				return controlScriptNativeError("reader namespace changed before mutation")
			}
			err := s.snapshotControl(context.Background(), "acquire", snapshotID(), "", "").Err()
			if err == nil || c.lookups != 8 || c.controls != 8 {
				t.Fatal("unbounded directory churn", err, c.lookups, c.controls)
			}
		})
	}
}
