// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	redis "github.com/adbc-drivers/redis/go"
)

func runLive(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: redis-adbc live <register|evolve|resume|cancel-evolution|unregister> [flags]")
	}
	command := args[0]
	if command != "register" && command != "evolve" && command != "resume" && command != "cancel-evolution" && command != "unregister" {
		return fmt.Errorf("unknown live command %q", command)
	}
	fs := newFlagSet("live "+command, stderr)
	var cf connFlags
	cf.register(fs)
	table := fs.String("table", "", "live table name (required for resume/unregister)")
	schema := fs.String("schema", "redis_live", "live schema: redis_live or a schema created with schema_ownership=live")
	apply := fs.Bool("apply", false, "apply the operation; otherwise report a preview")
	var hf hashFlags
	encodings := options{}
	var index *string
	var columns, expectedIndex *string
	var quiescent *bool
	if command == "evolve" || command == "cancel-evolution" {
		expectedIndex = fs.String("expected-index", "", "exact current driver-owned index identity")
	}
	if command == "evolve" {
		columns = fs.String("columns", "", "complete comma-separated public column selection")
		quiescent = fs.Bool("exclusive-quiescent", false, "all application/SQL writers and registration administrators remain stopped through completion")
	}
	if command == "register" || command == "evolve" {
		hf.register(fs)
		fs.Var(encodings, "encoding", "storage column=text or column=native (repeatable)")
		index = fs.String("index-columns", "", "comma-separated columns to index (default: all eligible columns)")
	}
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	if command == "register" || command == "evolve" {
		shape := redis.HashAdoptOptions{Prefix: hf.prefix, Schema: *schema, Table: *table, Types: hf.types, Renames: hf.renames, Encodings: encodings, IndexColumns: indexColumns(*index), Apply: *apply, Progress: func(s string) { fmt.Fprintln(stderr, s) }}
		var r *redis.LiveHashRegistration
		var err error
		if command == "evolve" {
			r, err = redis.EvolveLiveHashes(ctx, cf.options(), redis.LiveHashEvolutionOptions{HashAdoptOptions: shape, Columns: strings.Split(*columns, ","), ExpectedIndex: *expectedIndex, ExclusiveQuiescent: *quiescent})
		} else {
			r, err = redis.RegisterLiveHashes(ctx, cf.options(), shape)
		}
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
		fmt.Fprintf(stdout, "With -apply, %s the live registration %s.%s. Source HASHes stay unchanged.\n", command, *schema, *table)
		return nil
	}
	if command == "cancel-evolution" {
		if *expectedIndex == "" {
			return fmt.Errorf("-expected-index is required")
		}
		return redis.CancelLiveHashEvolutionInSchema(ctx, cf.options(), *schema, *table, *expectedIndex)
	}
	if command == "resume" {
		return redis.ResumeLiveHashesInSchema(ctx, cf.options(), *schema, *table)
	}
	return redis.UnregisterLiveHashesInSchema(ctx, cf.options(), *schema, *table)
}
