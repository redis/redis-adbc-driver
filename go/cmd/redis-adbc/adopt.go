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
	"encoding/json"
	"errors"
	"fmt"
	"io"

	redis "github.com/adbc-drivers/redis/go"
)

func runAdopt(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("adopt", stderr)
	var cf connFlags
	cf.register(fs)
	var hf hashFlags
	hf.register(fs)
	schema := fs.String("schema", "", "the table's schema (default: the database's)")
	table := fs.String("table", "", "the table's name (default: made from the prefix)")
	index := fs.String("index-columns", "", "comma-separated `columns` to index (default: every one that can be)")
	trust := fs.Bool("trust-strings", false, "trust the string values to stay as they are now (see the README)")
	apply := fs.Bool("apply", false, "do it; without -apply, adopt only checks and prints what it would do")
	asJSON := fs.Bool("json", false, "write the result as JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if hf.prefix == "" {
		return errors.New("-prefix is required (redis-adbc check lists the collections)")
	}
	o := redis.HashAdoptOptions{Prefix: hf.prefix, Schema: *schema, Table: *table, Types: hf.types, Renames: hf.renames,
		IndexColumns: indexColumns(*index), TrustStrings: *trust, Apply: *apply,
		Progress: func(msg string) { fmt.Fprintln(stderr, msg) }}
	res, err := redis.AdoptHashes(ctx, cf.options(), o)
	if res != nil && *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if jerr := enc.Encode(res); jerr != nil && err == nil {
			err = jerr
		}
		return err
	}
	if res != nil {
		printAdoption(stdout, res)
	}
	return err
}

func printAdoption(w io.Writer, res *redis.HashAdoption) {
	r := res.Report
	if res.AlreadyAdopted {
		fmt.Fprintf(w, "Collection %s on %s: the adopted table %s\n", r.Prefix, serverLine(r), res.Table)
		printChecks(w, r.Checks)
	} else {
		printReport(w, r)
	}
	ready := len(r.Checks) == 0 || r.Checks[0].Ready
	switch {
	case res.Applied:
		fmt.Fprintf(w, "\nAdopted: %s now reads the HASHes under %s, and those the application writes later (index %s).\n",
			res.Table, r.Prefix, res.Index)
	case !ready:
		fmt.Fprintln(w, "\nNothing was changed: fix the blockers first.")
	case res.AlreadyAdopted:
		fmt.Fprintf(w, "\nNothing to do: %s reads the HASHes under %s.\n", res.Table, r.Prefix)
	default:
		fmt.Fprintln(w, "\nNothing was changed. With -apply, adopt would:")
		for i, s := range res.Steps {
			fmt.Fprintf(w, "  %d. %s\n", i+1, s)
		}
	}
}
