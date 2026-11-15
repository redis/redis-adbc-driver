// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

// A standalone go-redis RESP3/TLS GET/PING routing diagnostic. It uses no ADBC,
// SQL, Search, Lua or managed storage and removes only its own expiring key.
package main

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type once struct{ *goredis.Cmd }

func (once) NoRetry() bool { return true }

type observation struct {
	Policy     string    `json:"pool_policy"`
	Round      int       `json:"round"`
	Bytes      int       `json:"payload_bytes"`
	Route      string    `json:"ping_route"`
	Before     []float64 `json:"before_get_milliseconds"`
	After      []float64 `json:"after_get_milliseconds"`
	ProofFirst float64   `json:"get_plus_first_ping_milliseconds"`
}

func run() (map[string]any, error) {
	uri := os.Getenv("REDIS_BENCH_URI")
	if !strings.HasPrefix(uri, "rediss://") {
		return nil, errors.New("a private REDIS_BENCH_URI using verified TLS is required")
	}
	options, err := goredis.ParseURL(uri)
	if err != nil || options.TLSConfig == nil {
		return nil, errors.New("invalid private TLS endpoint configuration")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if ca := os.Getenv("SSL_CERT_FILE"); ca != "" {
		pem, e := os.ReadFile(ca)
		if e != nil || !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("private CA trust could not be loaded")
		}
	}
	options.TLSConfig.RootCAs = roots
	options.TLSConfig.InsecureSkipVerify = false
	options.Protocol, options.MaxRetries, options.DisableIdentity = 3, -1, true
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, errors.New("owned control key identity could not be allocated")
	}
	keyPrefix := "codex:reply-route-control:" + hex.EncodeToString(token[:])
	var observations []observation
	var topologies []map[string]any
	for _, policy := range []string{"default", "no_dedicated_pipeline_pool"} {
		opts := *options
		opts.PipelinePoolSize = 0
		if policy != "default" {
			opts.PipelinePoolSize = -1
		}
		proof, main := goredis.NewClient(&opts), goredis.NewClient(&opts)
		defer proof.Close()
		defer main.Close()
		keys := map[int]string{}
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			var owned []string
			for _, key := range keys {
				owned = append(owned, key)
			}
			if len(owned) > 0 {
				_ = proof.Del(cleanup, owned...).Err()
			}
		}()
		if proof.Ping(ctx).Err() != nil || main.Ping(ctx).Err() != nil {
			return nil, errors.New("TLS/RESP3 control clients did not connect")
		}
		wantDedicated := policy == "default"
		if (proof.PoolStats().PipelineStats != nil) != wantDedicated || (main.PoolStats().PipelineStats != nil) != wantDedicated {
			return nil, errors.New("requested native pipeline policy was not effective")
		}
		topologies = append(topologies, map[string]any{"policy": policy, "proof_dedicated_pipeline": proof.PoolStats().PipelineStats != nil, "main_dedicated_pipeline": main.PoolStats().PipelineStats != nil})
		sizes := []int{4096, 8000, 8191, 8192, 8193, 16384, 32768}
		for _, n := range sizes {
			keys[n] = keyPrefix + ":" + strconv.Itoa(n)
			if err := proof.Set(ctx, keys[n], strings.Repeat("x", n), 5*time.Minute).Err(); err != nil {
				return nil, errors.New("owned expiring control key was not prepared")
			}
		}
		routes := []string{"scalar_main", "scalar_proof", "pipeline_main", "pipeline_proof"}
		for round := 0; round < 8; round++ {
			for position := 0; position < len(sizes)*len(routes); position++ {
				index := position
				if round%2 == 1 {
					index = len(sizes)*len(routes) - 1 - position
				}
				n, route := sizes[index/len(routes)], routes[index%len(routes)]
				value := strings.Repeat("x", n)
				key := keys[n]
				ping := func() (float64, error) {
					client := main
					if strings.HasSuffix(route, "proof") {
						client = proof
					}
					command := goredis.NewCmd(ctx, "PING")
					started := time.Now()
					if strings.HasPrefix(route, "pipeline_") {
						pipe := client.Pipeline()
						if err := pipe.Process(ctx, once{command}); err != nil {
							pipe.Discard()
							return 0, errors.New("native PING queue failed")
						}
						if _, err := pipe.Exec(ctx); err != nil {
							return 0, errors.New("native pipeline PING failed")
						}
					} else if err := client.Process(ctx, once{command}); err != nil {
						return 0, errors.New("native scalar PING failed")
					}
					elapsed := float64(time.Since(started)) / float64(time.Millisecond)
					if command.Err() != nil || command.Val() != "PONG" {
						return 0, errors.New("native PING oracle failed")
					}
					return elapsed, nil
				}
				item := observation{Policy: policy, Round: round, Bytes: n, Route: route}
				for i := 0; i < 3; i++ {
					ms, err := ping()
					if err != nil {
						return nil, err
					}
					item.Before = append(item.Before, ms)
				}
				started := time.Now()
				pipe := proof.Pipeline()
				command := goredis.NewCmd(ctx, "GET", key)
				if err := pipe.Process(ctx, once{command}); err != nil {
					pipe.Discard()
					return nil, errors.New("native GET queue failed")
				}
				if _, err := pipe.Exec(ctx); err != nil || command.Err() != nil || command.Val() != value {
					return nil, errors.New("complete native bulk GET oracle failed")
				}
				for i := 0; i < 3; i++ {
					ms, err := ping()
					if err != nil {
						return nil, err
					}
					item.After = append(item.After, ms)
					if i == 0 {
						item.ProofFirst = float64(time.Since(started)) / float64(time.Millisecond)
					}
				}
				observations = append(observations, item)
			}
		}
	}
	deps := map[string]string{}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range build.Deps {
			if dependency.Path == "github.com/redis/go-redis/v9" {
				deps[dependency.Path] = dependency.Version
			}
		}
	}
	return map[string]any{"scope": "Standalone go-redis native RESP3/TLS GET then PING; no ADBC/SQL/Search/Lua/managed storage; helper elapsed, not server CPU or SQL latency", "dependencies": deps, "samples": observations, "rounds": 8, "pool_topologies": topologies, "caller_retries": 0, "tls": "certificate and hostname verified", "owned_cleanup": "only seven expiring random control keys deleted; no FLUSH/SCAN/other-key deletion"}, nil
}

func main() {
	result, err := run()
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"validated": false, "error": err.Error()})
		os.Exit(1)
	}
	result["validated"] = true
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
