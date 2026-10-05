// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	redis "github.com/adbc-drivers/redis/go"
	"github.com/apache/arrow-adbc/go/adbc"
)

var ingestModes = map[string]string{
	"create":        adbc.OptionValueIngestModeCreate,
	"append":        adbc.OptionValueIngestModeAppend,
	"replace":       adbc.OptionValueIngestModeReplace,
	"create_append": adbc.OptionValueIngestModeCreateAppend,
}

func runCopy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("copy", stderr)
	var cf connFlags
	cf.register(fs)
	var hf hashFlags
	hf.register(fs)
	schema := fs.String("schema", "", "the table's schema (default: the database's)")
	table := fs.String("table", "", "the table's name (default: made from the prefix)")
	mode := fs.String("mode", "create", "create, append, replace or create_append, as in ADBC bulk ingest")
	sample := fs.Int("sample", 1000, "HASHes read to guess the columns; -1 reads all of them")
	keyColumn := fs.String("key-column", "_key", "column that holds each HASH's key; empty for none")
	index := fs.String("index-columns", "", "comma-separated `columns` to index (default: every one that can be)")
	onError := fs.String("on-error", "fail", "a value that doesn't convert to its column's type: fail, or null to copy it as NULL")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if hf.prefix == "" {
		return errors.New("-prefix is required (redis-adbc check lists the collections)")
	}
	ingestMode, ok := ingestModes[*mode]
	if !ok {
		return fmt.Errorf("unknown -mode %q (want create, append, replace or create_append)", *mode)
	}
	if *onError != "fail" && *onError != "null" {
		return fmt.Errorf("unknown -on-error %q (want fail or null)", *onError)
	}
	res, err := redis.CopyHashes(ctx, cf.options(), redis.HashCopyOptions{
		Prefix: hf.prefix, Sample: *sample, KeyColumn: *keyColumn, Types: hf.types, Renames: hf.renames,
		NullOnError: *onError == "null", Schema: *schema, Table: *table, Mode: ingestMode, IndexColumns: indexColumns(*index),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d %s copied into %s\n", res.Rows, plural(res.Rows, "row", "rows"), res.Table)
	if len(res.UnknownFields) > 0 {
		fmt.Fprintf(stderr, "skipped fields that the sample didn't have (-sample -1 guesses the columns from every HASH): %s\n",
			countList(res.UnknownFields, "HASH", "HASHes"))
	}
	if len(res.Nulls) > 0 {
		fmt.Fprintf(stderr, "values copied as NULL because they didn't convert: %s\n", countList(res.Nulls, "value", "values"))
	}
	return nil
}
