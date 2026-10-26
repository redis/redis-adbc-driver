// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

// Standalone, read-only #498 reproduction with no ADBC driver dependency.
// First verify a dedicated Software three-shard / 4x QPF fixture with
// make -C go redis-up. Run from the module with:
// go run ../scripts/software_eof_client.go -count 50000
// REDIS_URI must privately select that fixture's loopback endpoint.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func main() {
	count := flag.Int("count", 50000, "number of fresh RESP3 clients")
	flag.Parse()
	u, err := url.Parse(os.Getenv("REDIS_URI"))
	if err != nil || *count < 1 || u == nil || u.Scheme != "redis" ||
		(u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") ||
		u.Port() == "" || u.User != nil || (u.Path != "" && u.Path != "/0") || u.RawQuery != "" || u.Fragment != "" {
		println("require positive -count and REDIS_URI for the dedicated loopback fixture, database 0")
		os.Exit(2)
	}
	opts, err := goredis.ParseURL(u.String())
	if err != nil {
		println("invalid private fixture URI")
		os.Exit(2)
	}
	opts.Protocol, opts.DisableIdentity, opts.MaxRetries = 3, true, -1
	// Reject go-redis's fallback to RESP2 if initial HELLO negotiation fails.
	opts.OnConnect = func(ctx context.Context, conn *goredis.Conn) error {
		reply, err := conn.Hello(ctx, 3, "", "", "").Result()
		if err != nil {
			return err
		}
		if reply["proto"] != int64(3) {
			return errors.New("RESP3 not confirmed")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	counts := map[string]int{
		"completed": 0, "passed": 0, "ping_failed": 0, "read_failed": 0,
		"eof": 0, "timeout": 0, "other": 0, "close_failed": 0,
	}
	classify := func(err error) {
		var ne net.Error
		switch {
		case errors.Is(err, io.EOF):
			counts["eof"]++
		case errors.As(err, &ne) && ne.Timeout():
			counts["timeout"]++
		default:
			counts["other"]++
		}
	}
	start := time.Now()
	for i := 0; i < *count && ctx.Err() == nil; i++ {
		client := goredis.NewClient(opts)
		if err := client.Ping(ctx).Err(); err != nil {
			counts["ping_failed"]++
			classify(err)
		} else if err := client.HGet(ctx, "eof:498:absent-probe", "id").Err(); !errors.Is(err, goredis.Nil) {
			counts["read_failed"]++
			if err == nil {
				counts["other"]++ // The owned fixture must not contain this key.
			} else {
				classify(err)
			}
		} else {
			counts["passed"]++
		}
		if err := client.Close(); err != nil {
			counts["close_failed"]++
		}
		counts["completed"]++
	}
	// Errors, URIs, addresses and response contents never enter this summary.
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"requested": *count, "counts": counts, "seconds": time.Since(start).Seconds(),
	}); err != nil {
		os.Exit(2)
	}
	if counts["passed"] != *count || counts["close_failed"] != 0 {
		os.Exit(1)
	}
}
