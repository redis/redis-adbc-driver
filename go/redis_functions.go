// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

//go:generate go run ./internal/cmd/genfunctions

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

// Content-addressed names let different driver versions coexist without
// replacing code used by an in-flight statement or another client.
var redisFunctionsVersion = func() string {
	bodies := append([]string(nil), redisFunctionBodies...)
	sort.Strings(bodies)
	var framed strings.Builder
	for _, body := range bodies {
		fmt.Fprintf(&framed, "%d:%s", len(body), body)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(framed.String())))
}()

func redisFunctionName(body string) string {
	return fmt.Sprintf("adbc_%s_%x", redisFunctionsVersion, sha256.Sum256([]byte(body)))
}

var redisFunctionLibraryName, redisFunctionLibraryCode = buildRedisFunctionLibrary()

func redisFunctionLibrary() (string, string) {
	return redisFunctionLibraryName, redisFunctionLibraryCode
}

func buildRedisFunctionLibrary() (string, string) {
	bodies := make(map[string]string)
	for _, body := range redisFunctionBodies {
		bodies[redisFunctionName(body)] = body
	}
	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}
	sort.Strings(names)
	var source strings.Builder
	for _, name := range names {
		fmt.Fprintf(&source, "redis.register_function('%s', function(KEYS, ARGV)\n%s\nend)\n", name, bodies[name])
	}
	code := source.String()
	name := fmt.Sprintf("adbc_%x", sha256.Sum256([]byte(code)))
	return name, "#!lua name=" + name + "\n" + code
}

// Installation is connection initialization, never mutation recovery. There
// is no EVAL fallback and no retry of a failed FCALL. Functions survive RDB/AOF
// and replication; an administrator flushing them must reconnect clients.
func ensureRedisFunctions(ctx context.Context, client processor) error {
	probe := redisFunctionName("return 1")
	result := once(ctx, client, "FCALL", probe, 0)
	if result.Err() == nil {
		return nil
	}
	native, ok := result.Err().(interface {
		error
		RedisError()
	})
	if !ok || native.Error() != "ERR Function not found" {
		return fmt.Errorf("Redis Functions are required: %w", result.Err())
	}
	_, code := redisFunctionLibrary()
	loaded := once(ctx, client, "FUNCTION", "LOAD", code)
	if loaded.Err() != nil {
		// A concurrent initializer may have installed exactly this immutable
		// library. Verify only a direct native name collision; unknown load replies
		// fail initialization without replaying the load or any user operation.
		native, ok := loaded.Err().(interface {
			error
			RedisError()
		})
		name, _ := redisFunctionLibrary()
		if !ok || native.Error() != "ERR Library '"+name+"' already exists" {
			return fmt.Errorf("install Redis Functions (FUNCTION LOAD permission required): %w", loaded.Err())
		}
	}
	if err := once(ctx, client, "FCALL", probe, 0).Err(); err != nil {
		return fmt.Errorf("verify Redis Functions: %w", err)
	}
	return nil
}

func requireModernConnection(ctx context.Context, conn *goredis.Conn) error {
	if err := requireRESP3(ctx, conn); err != nil {
		return err
	}
	if err := ensureRedisFunctions(ctx, conn); err != nil {
		// go-redis unwraps initialization errors once. Keep the capability/permission
		// diagnostic intact, as requireRESP3 does for failed negotiation.
		return fmt.Errorf("Redis Functions initialization failed: %v", err)
	}
	return nil
}

type redisFunction string

func newRedisFunction(body string) redisFunction { return redisFunction(body) }
func (f redisFunction) Name() string             { return redisFunctionName(string(f)) }
func (f redisFunction) Run(ctx context.Context, client processor, keys []string, argv ...any) *goredis.Cmd {
	args := []any{"FCALL", f.Name(), len(keys)}
	for _, key := range keys {
		args = append(args, key)
	}
	args = append(args, argv...)
	return once(ctx, client, args...)
}
