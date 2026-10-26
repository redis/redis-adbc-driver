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
	"fmt"
	"io"
	"text/tabwriter"

	redis "github.com/adbc-drivers/redis/go"
)

func runSweep(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("sweep", stderr)
	var cf connFlags
	cf.register(fs)
	apply := fs.Bool("apply", false, "delete the HASHes; without -apply, sweep only counts them")
	asJSON := fs.Bool("json", false, "write the result as JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	res, err := redis.SweepReleased(ctx, cf.options(), redis.SweepOptions{
		Apply: *apply, Progress: func(msg string) { fmt.Fprintln(stderr, msg) }})
	if res != nil && *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if jerr := enc.Encode(res); jerr != nil && err == nil {
			err = jerr
		}
		return err
	}
	if res != nil {
		printSweep(stdout, res, err == nil)
	}
	return err
}

func printSweep(w io.Writer, res *redis.Sweep, finished bool) {
	kind := "standalone"
	if res.Cluster {
		kind = "cluster"
	}
	server := "Redis (" + kind + ")"
	if res.ServerVersion != "" {
		server = fmt.Sprintf("Redis %s (%s)", res.ServerVersion, kind)
	}
	fmt.Fprintf(w, "Released key prefixes on %s: %d\n", server, res.Released)
	if len(res.Prefixes) > 0 {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if res.Applied {
			fmt.Fprintln(tw, "  PREFIX\tHASHES\tDELETED")
		} else {
			fmt.Fprintln(tw, "  PREFIX\tHASHES")
		}
		for _, p := range res.Prefixes {
			if res.Applied {
				fmt.Fprintf(tw, "  %s\t%d\t%d\n", p.Prefix, p.Found, p.Deleted)
			} else {
				fmt.Fprintf(tw, "  %s\t%d\n", p.Prefix, p.Found)
			}
		}
		_ = tw.Flush()
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintln(w, "\nLeft alone:")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, s := range res.Skipped {
			fmt.Fprintf(tw, "  %s\t%s\n", s.Prefix, s.Reason)
		}
		_ = tw.Flush()
	}
	under := fmt.Sprintf("%d %s under %d %s", res.Found, plural(res.Found, "HASH", "HASHes"),
		len(res.Prefixes), plural(int64(len(res.Prefixes)), "prefix", "prefixes"))
	switch {
	case !finished && res.Applied:
		fmt.Fprintf(w, "\nStopped after deleting %d of the %s found so far.\n", res.Deleted, under)
	case !finished:
		fmt.Fprintf(w, "\nStopped after finding %s.\n", under)
	case res.Found == 0:
		fmt.Fprintln(w, "\nFound no HASHes to delete.")
	case res.Applied && res.Deleted == res.Found:
		fmt.Fprintf(w, "\nDeleted %s.\n", under)
	case res.Applied:
		fmt.Fprintf(w, "\nDeleted %d of the %s; the others were gone already.\n", res.Deleted, under)
	default:
		fmt.Fprintf(w, "\nNothing was deleted. With -apply, sweep deletes the %s.\n", under)
	}
}
