// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func retainedSourceFixture(t *testing.T) (context.Context, goredis.UniversalClient, *clientConfig, timeouts, bool, string) {
	t.Helper()
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run retained transport integration tests")
	}
	d := &databaseImpl{uri: uri, cluster: "auto", tlsCAFile: os.Getenv("REDIS_RETAINED_TLS_CA_FILE")}
	ctx := context.Background()
	client, cfg, timeouts, _, err := d.dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_, cluster := client.(*goredis.ClusterClient)
	id, err := retainedRandomID()
	if err != nil {
		t.Fatal(err)
	}
	key := "it_retained_source:" + id
	t.Cleanup(func() {
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Error(err)
		}
	})
	return ctx, client, cfg, timeouts, cluster, key
}

func TestRetainedSourceRESPDefaultAndBoundedReads(t *testing.T) {
	ctx, raw, cfg, timeouts, cluster, key := retainedSourceFixture(t)
	if cfg.single.Protocol != 3 {
		t.Fatal("retained transport did not inherit RESP3")
	}
	payload := []byte{0, 255, 'e', 'x', 'a', 'c', 't', 0}
	if err := raw.HSet(ctx, key, "binary", payload, "id", "9223372036854775807").Err(); err != nil {
		t.Fatal(err)
	}
	client := newRetainedSourceClient(cfg, cluster, timeouts, newRetainedBudget(64<<20))
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	values, err := client.HMGet(ctx, key, "id", "binary", "absent").Result()
	if err != nil || len(values) != 3 || values[0] != "9223372036854775807" || values[2] != nil || !bytes.Equal([]byte(values[1].(string)), payload) {
		t.Fatal("guard changed source values", err)
	}
	if _, err := client.Time(ctx).Result(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(ctx, "FT._LIST").Result(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.HGetAll(ctx, key).Result(); err != nil {
		t.Fatal(err)
	}
	if cfg.single.Dialer != nil || cfg.single.MaxRetries == -1 {
		t.Fatal("ordinary client configuration changed")
	}
}

func TestRetainedSourceRESPRejectsOversizedValue(t *testing.T) {
	ctx, raw, cfg, timeouts, cluster, key := retainedSourceFixture(t)
	payload := strings.Repeat("x", retainedRESPBulk+1)
	if err := raw.Set(ctx, key, payload, 0).Err(); err != nil {
		t.Fatal(err)
	}
	client := newRetainedSourceClient(cfg, cluster, timeouts, newRetainedBudget(64<<20))
	defer client.Close()
	value, err := client.Get(ctx, key).Result()
	if value != "" || !isStatus(err, adbc.StatusInvalidData) {
		t.Fatal("oversized value reached the source decoder", len(value), err)
	}
	// Exhaustion must not be mistaken for a missing row or modify source data.
	if length, err := raw.StrLen(ctx, key).Result(); err != nil || length != int64(len(payload)) {
		t.Fatal("source value changed", length, err)
	}
}

func TestRetainedSourceRESPCumulativeReadBudget(t *testing.T) {
	ctx, raw, cfg, timeouts, cluster, key := retainedSourceFixture(t)
	if err := raw.Set(ctx, key, strings.Repeat("b", 32768), 0).Err(); err != nil {
		t.Fatal(err)
	}
	budget := newRetainedBudget(64 << 10)
	client := newRetainedSourceClient(cfg, cluster, timeouts, budget)
	defer client.Close()
	if value, err := client.Get(ctx, key).Result(); value != "" || !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("source allocation budget not enforced", len(value), err)
	}
	if left := budget.remaining.Load(); left < 0 {
		t.Fatal("budget overflowed", left)
	}
}

func TestRetainedSourceRESPPreservesTLSVerification(t *testing.T) {
	ctx, _, cfg, timeouts, cluster, _ := retainedSourceFixture(t)
	if cfg.single.TLSConfig == nil {
		t.Skip("requires the dedicated TLS fixture")
	}
	opts := *cfg.single
	opts.TLSConfig = opts.TLSConfig.Clone()
	opts.TLSConfig.RootCAs = x509.NewCertPool()
	bad := newClientConfig(&opts)
	if cluster {
		bad.cluster.Addrs = cfg.cluster.Addrs
	}
	client := newRetainedSourceClient(bad, cluster, timeouts, newRetainedBudget(64<<20))
	defer client.Close()
	err := client.Ping(ctx).Err()
	var authority x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &authority) {
		t.Fatal("TLS verification was bypassed", err)
	}
}
