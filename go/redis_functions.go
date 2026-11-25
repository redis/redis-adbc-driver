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
// replacing code used by an in-flight statement or another client. Flags are
// part of each entry point's contract (FCALL_RO refuses an unflagged function),
// so the version frames every body with its flags: a flag change renames every
// entry point instead of colliding with, or reusing, an older library.
var redisFunctionsVersion = redisFunctionsVersionFor(redisFunctionBodies, redisReadOnlyFunctionBodySet())

func redisFunctionsVersionFor(entries []string, readOnly map[string]bool) string {
	bodies := append([]string(nil), entries...)
	sort.Strings(bodies)
	var framed strings.Builder
	for _, body := range bodies {
		flags := ""
		if readOnly[body] {
			flags = redisReadOnlyFunctionFlags
		}
		fmt.Fprintf(&framed, "%d:%s%d:%s", len(flags), flags, len(body), body)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(framed.String())))
}

// redisReadOnlyFunctionFlags is the Lua flags table of read-only entry points.
// no-writes implies allow-oom: Redis runs them over maxmemory, on replicas and
// during CLIENT PAUSE WRITE, and refuses any write command they attempt
// instead of applying it.
const redisReadOnlyFunctionFlags = "{'no-writes'}"

func redisReadOnlyFunctionBodySet() map[string]bool {
	set := make(map[string]bool, len(redisReadOnlyFunctionBodies))
	for _, body := range redisReadOnlyFunctionBodies {
		set[body] = true
	}
	return set
}

// redisReadOnlyFunctionNames holds the entry points invoked with FCALL_RO.
var redisReadOnlyFunctionNames = func() map[string]bool {
	names := make(map[string]bool, len(redisReadOnlyFunctionBodies))
	for _, body := range redisReadOnlyFunctionBodies {
		names[redisFunctionName(body)] = true
	}
	return names
}()

// redisFunctionCommand is FCALL_RO for a read-only entry point and FCALL
// otherwise. FCALL_RO is a read command with read-only key specifications;
// FCALL of an unflagged function is a write that Redis denies under OOM.
func redisFunctionCommand(name string) string {
	if redisReadOnlyFunctionNames[name] {
		return "FCALL_RO"
	}
	return "FCALL"
}

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
	readOnly := redisReadOnlyFunctionBodySet()
	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}
	sort.Strings(names)
	var source strings.Builder
	for _, name := range names {
		body := bodies[name]
		if readOnly[body] {
			fmt.Fprintf(&source, "redis.register_function{function_name='%s', callback=function(KEYS, ARGV)\n%s\nend, flags=%s}\n", name, body, redisReadOnlyFunctionFlags)
			continue
		}
		fmt.Fprintf(&source, "redis.register_function('%s', function(KEYS, ARGV)\n%s\nend)\n", name, body)
	}
	code := source.String()
	name := fmt.Sprintf("adbc_%x", sha256.Sum256([]byte(code)))
	return name, "#!lua name=" + name + "\n" + code
}

// Installation is connection initialization, never mutation recovery. There
// is no EVAL fallback and no retry of a failed FCALL. Functions survive RDB/AOF
// and replication; an administrator flushing them must reconnect clients.
func ensureRedisFunctions(ctx context.Context, client processor) error {
	// The read-only probe also runs over maxmemory, so memory pressure cannot
	// masquerade as a missing Functions capability on a new connection.
	probe := newReadOnlyRedisFunction("return 1")
	result := once(ctx, client, probe.Args(0)...)
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
	if err := once(ctx, client, probe.Args(0)...).Err(); err != nil {
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

// redisFunction is one statically declared library entry point. Declare a
// body with newReadOnlyRedisFunction only when no path through it can issue a
// write command (redis.acl_check_cmd only inspects permissions). go generate
// records the declaration in redis_functions_manifest.go; the library then
// registers it with flags={'no-writes'} and Command invokes it with FCALL_RO,
// so it keeps answering while the server is over maxmemory and Redis refuses
// any write it attempts rather than applying it. Every other
// body is invoked with FCALL, which Redis treats as a write denied under OOM.
// The generated manifest, not the constructor, selects the command, so a stale
// declaration can never send FCALL_RO to a function registered without flags.
type redisFunction string

func newRedisFunction(body string) redisFunction         { return redisFunction(body) }
func newReadOnlyRedisFunction(body string) redisFunction { return redisFunction(body) }
func (f redisFunction) Name() string                     { return redisFunctionName(string(f)) }

// Command is FCALL_RO for a read-only entry point and FCALL otherwise.
func (f redisFunction) Command() string { return redisFunctionCommand(f.Name()) }

// Args begins an invocation: command, entry point and declared key count.
// Callers append exactly numKeys keys, then the arguments.
func (f redisFunction) Args(numKeys int) []any { return []any{f.Command(), f.Name(), numKeys} }
func (f redisFunction) Run(ctx context.Context, client processor, keys []string, argv ...any) *goredis.Cmd {
	args := f.Args(len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	args = append(args, argv...)
	return once(ctx, client, args...)
}
