// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

// Fault-injection/model functions use a separate test namespace and never
// replace driver entry points. Only the creating test retires the library.
func installTestFunction(t *testing.T, admin processor, body string) string {
	t.Helper()
	name := fmt.Sprintf("adbc_test_%x", sha256.Sum256([]byte(body)))
	code := "#!lua name=" + name + "\nredis.register_function('" + name + "',function(KEYS,ARGV)\n" + body + "\nend)"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := once(ctx, admin, "FUNCTION", "LOAD", code).Err()
	if err != nil {
		native, ok := err.(interface {
			error
			RedisError()
		})
		if !ok || native.Error() != "ERR Library '"+name+"' already exists" {
			t.Fatal("install test function", err)
		}
	} else {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := once(ctx, admin, "FUNCTION", "DELETE", name).Err(); err != nil {
				t.Error("remove test function", err)
			}
		})
	}
	return name
}
