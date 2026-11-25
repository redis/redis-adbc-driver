// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// redisFunctionRegistered accepts either registration form for one name.
func redisFunctionRegistered(code, name string) bool {
	return strings.Contains(code, "redis.register_function('"+name+"', function(KEYS, ARGV)\n") ||
		strings.Contains(code, "redis.register_function{function_name='"+name+"', callback=function(KEYS, ARGV)\n")
}

// The audited no-writes entry points: every command each body can issue is a
// read, and redis.acl_check_cmd only inspects permissions. Adding a body here
// requires the same audit; a body that writes on any path stays unflagged.
var auditedReadOnlyRedisFunctions = map[string]string{
	"connection probe":         "return 1",
	"replacement permission":   unmanagedReplacePermissionLua,
	"replacement inspection":   unmanagedReplaceInspectLua,
	"reader domain discovery":  snapshotReaderDomainProbeLua,
	"catalog write capability": snapshotCatalogWriteProbeLua,
	"reader namespace read":    snapshotNamespaceRead,
	"operation status":         snapshotOperationStatusLua,
	"point read":               snapshotPointReadLua,
	"search admission":         snapshotSearchAdmission,
}

// Commands without the write or may-replicate flag; Redis refuses either kind
// from a no-writes function.
var redisReadOnlyFunctionCommands = map[string]bool{
	"DUMP": true, "EXISTS": true, "HGET": true, "HGETALL": true, "HMGET": true,
	"MEMORY": true, "PEXPIRETIME": true, "SCARD": true, "SSCAN": true,
	"TIME": true, "TYPE": true, "ZCARD": true, "ZSCORE": true,
}

var redisFunctionWriteCommands = map[string]bool{
	"COPY": true, "DEL": true, "HDEL": true, "HINCRBY": true, "HSET": true,
	"INCRBY": true, "PEXPIREAT": true, "SADD": true, "SET": true, "SREM": true,
	"UNLINK": true, "ZADD": true, "ZREM": true, "ZREMRANGEBYSCORE": true,
}

var (
	redisFunctionCallSite = regexp.MustCompile(`\b(?:redis|server)\.p?call\(`)
	redisFunctionLiteral  = regexp.MustCompile(`\b(?:(?:redis|server)\.p?call|checked)\(\s*'([A-Za-z_.|]+)'`)
)

// redisFunctionCommandsOf returns the literal commands a body can issue and
// whether every call site names its command literally. The point read's
// checked(...) forwarder is the one audited exception: it forwards only the
// literal commands of its own checked('...') calls.
func redisFunctionCommandsOf(body string) ([]string, bool) {
	literal := true
	forwarder := strings.Contains(body, "local function checked(...)\n local reply=redis.pcall(...)")
	for _, at := range redisFunctionCallSite.FindAllStringIndex(body, -1) {
		rest := strings.TrimLeft(body[at[1]:], " ")
		if strings.HasPrefix(rest, "'") || (forwarder && strings.HasPrefix(body[at[0]:], "redis.pcall(...)")) {
			continue
		}
		literal = false
	}
	var commands []string
	for _, match := range redisFunctionLiteral.FindAllStringSubmatch(body, -1) {
		commands = append(commands, strings.ToUpper(match[1]))
	}
	return commands, literal
}

func TestRedisFunctionReadOnlyFlags(t *testing.T) {
	expected := map[string]bool{}
	for purpose, body := range auditedReadOnlyRedisFunctions {
		if expected[body] {
			t.Fatal("duplicate audited body", purpose)
		}
		expected[body] = true
	}
	declared := redisReadOnlyFunctionBodySet()
	if len(declared) != len(redisReadOnlyFunctionBodies) || len(declared) != len(expected) {
		t.Fatalf("read-only declarations %d (unique %d), audited %d; run go generate .", len(redisReadOnlyFunctionBodies), len(declared), len(expected))
	}
	for purpose, body := range auditedReadOnlyRedisFunctions {
		if !declared[body] || !slices.Contains(redisFunctionBodies, body) {
			t.Fatal("audited read-only body is not declared with newReadOnlyRedisFunction", purpose)
		}
	}

	_, code := redisFunctionLibrary()
	if n := strings.Count(code, "redis.register_function"); n != len(redisFunctionBodies) {
		t.Fatalf("library registers %d entry points, want %d", n, len(redisFunctionBodies))
	}
	if n := strings.Count(code, "flags="); n != len(expected) {
		t.Fatalf("library declares %d flag tables, want %d", n, len(expected))
	}
	for _, body := range redisFunctionBodies {
		name := redisFunctionName(body)
		positional := "redis.register_function('" + name + "', function(KEYS, ARGV)\n" + body + "\nend)\n"
		flagged := "redis.register_function{function_name='" + name + "', callback=function(KEYS, ARGV)\n" + body + "\nend, flags={'no-writes'}}\n"
		commands, literal := redisFunctionCommandsOf(body)
		if expected[body] {
			if !strings.Contains(code, flagged) || strings.Contains(code, positional) {
				t.Fatal("read-only entry point is not registered with flags={'no-writes'}", name)
			}
			if redisFunctionCommand(name) != "FCALL_RO" || newReadOnlyRedisFunction(body).Command() != "FCALL_RO" {
				t.Fatal("read-only entry point is not invoked with FCALL_RO", name)
			}
			if !literal {
				t.Fatal("read-only entry point issues a command chosen at run time", name)
			}
			for _, command := range commands {
				if !redisReadOnlyFunctionCommands[command] {
					t.Fatalf("read-only entry point %s issues %s", name, command)
				}
			}
			continue
		}
		if !strings.Contains(code, positional) || strings.Contains(code, flagged) {
			t.Fatal("writing entry point is registered with flags", name)
		}
		if redisFunctionCommand(name) != "FCALL" || newRedisFunction(body).Command() != "FCALL" {
			t.Fatal("writing entry point is not invoked with FCALL", name)
		}
		// Every unflagged body writes. One that cannot would be denied under
		// OOM for no reason: audit it and declare it read-only instead.
		if !slices.ContainsFunc(commands, func(command string) bool { return redisFunctionWriteCommands[command] }) {
			t.Fatal("unflagged entry point issues no write command; audit it for newReadOnlyRedisFunction", name)
		}
	}
	if got := newReadOnlyRedisFunction(snapshotPointReadLua).Args(2); len(got) != 3 || got[0] != "FCALL_RO" || got[1] != snapshotPointReadDigest || got[2] != 2 {
		t.Fatal("read-only invocation prefix", got)
	}
}

// FCALL_RO refuses a function registered without no-writes, and Redis refuses
// a library whose function names already exist. A flag change must therefore
// rename every entry point and the library, so an older driver's unflagged
// library and this one load side by side.
func TestRedisFunctionFlagsVersionEntryPoints(t *testing.T) {
	unflagged := redisFunctionsVersionFor(redisFunctionBodies, nil)
	if unflagged == redisFunctionsVersion {
		t.Fatal("flags do not change the Functions version")
	}
	for _, body := range redisReadOnlyFunctionBodies {
		fewer := redisReadOnlyFunctionBodySet()
		delete(fewer, body)
		if redisFunctionsVersionFor(redisFunctionBodies, fewer) == redisFunctionsVersion {
			t.Fatal("one entry point's flags do not change the Functions version", redisFunctionName(body))
		}
	}
	name, code := redisFunctionLibrary()
	if !strings.HasPrefix(code, "#!lua name="+name+"\n") {
		t.Fatal("library header does not carry its content-addressed name")
	}
	for _, body := range redisFunctionBodies {
		if !strings.HasPrefix(redisFunctionName(body), "adbc_"+redisFunctionsVersion+"_") || strings.Contains(code, "'adbc_"+unflagged+"_") {
			t.Fatal("entry point name does not carry the flag-aware version")
		}
	}
}
