// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	redis "github.com/adbc-drivers/redis/go"
)

func runReaderACL(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("reader-acl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	domain := fs.String("domain", "", "backend authorization domain (SQL RBAC: dedicated ACL name)")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	if *domain == "" || len(*domain) > 1024 || fs.NArg() != 0 {
		return usageError{errors.New("reader-acl requires one -domain value, at most 1024 bytes")}
	}
	return json.NewEncoder(stdout).Encode(redis.SnapshotReaderPermissions(*domain))
}
