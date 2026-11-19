// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	redis "github.com/adbc-drivers/redis/go"
)

func runLive(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: redis-adbc live <register|resume|unregister> [flags]")
	}
	command := args[0]
	if command != "register" && command != "resume" && command != "unregister" {
		return fmt.Errorf("unknown live command %q", command)
	}
	fs := newFlagSet("live "+command, stderr)
	var cf connFlags
	cf.register(fs)
	table := fs.String("table", "", "table name in redis_live (required for resume/unregister)")
	apply := fs.Bool("apply", false, "apply the operation; otherwise report a preview")
	var hf hashFlags
	encodings := options{}
	var index *string
	if command == "register" {
		hf.register(fs)
		fs.Var(encodings, "encoding", "storage column=text or column=native (repeatable)")
		index = fs.String("index-columns", "", "comma-separated columns to index (default: all eligible columns)")
	}
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	if command == "register" {
		r, err := redis.RegisterLiveHashes(ctx, cf.options(), redis.HashAdoptOptions{Prefix: hf.prefix, Table: *table, Types: hf.types, Renames: hf.renames, Encodings: encodings, IndexColumns: indexColumns(*index), Apply: *apply, Progress: func(s string) { fmt.Fprintln(stderr, s) }})
		if r != nil {
			if e := json.NewEncoder(stdout).Encode(r); e != nil && err == nil {
				err = e
			}
		}
		return err
	}
	if *table == "" {
		return fmt.Errorf("-table is required")
	}
	if !*apply {
		fmt.Fprintf(stdout, "With -apply, %s the live registration redis_live.%s. Source HASHes stay unchanged.\n", command, *table)
		return nil
	}
	if command == "resume" {
		return redis.ResumeLiveHashes(ctx, cf.options(), *table)
	}
	return redis.UnregisterLiveHashes(ctx, cf.options(), *table)
}
