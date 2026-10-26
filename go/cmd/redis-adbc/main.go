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

// Command redis-adbc makes HASHes that an application wrote usable with the
// ADBC driver: check guesses their table structure and lists what the
// driver needs, copy copies them into a driver table, and adopt makes them
// a driver table in place.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"sort"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

const defaultURI = "redis://localhost:6379/0"

const usage = `usage: redis-adbc <command> [flags]

Commands:
  check    check an existing HASH collection: guess its table structure, and
           list what the ADBC driver needs to query it
  copy     copy an existing HASH collection into a driver table
  adopt    make an existing HASH collection a driver table, in place

Run "redis-adbc <command> -h" for a command's flags.
`

// usageError is a bad command line; the flag package has already printed
// what is wrong.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	var ue usageError
	switch {
	case err == nil:
	case errors.Is(err, flag.ErrHelp):
	case errors.As(err, &ue):
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "redis-adbc: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return usageError{errors.New("no command given")}
	}
	switch args[0] {
	case "check":
		return runCheck(ctx, args[1:], stdout, stderr)
	case "copy":
		return runCopy(ctx, args[1:], stdout, stderr)
	case "adopt":
		return runAdopt(ctx, args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	}
	fmt.Fprint(stderr, usage)
	return usageError{fmt.Errorf("unknown command %q", args[0])}
}

// options collects repeated key=value flags.
type options map[string]string

func (o options) String() string {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k+"="+o[k])
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func (o options) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("want key=value, got %q", s)
	}
	o[k] = v
	return nil
}

// connFlags are the flags that every command takes to reach Redis.
type connFlags struct {
	uri  string
	opts options
}

func (c *connFlags) register(fs *flag.FlagSet) {
	c.opts = options{}
	// The default is resolved when connecting, so that -h doesn't print the
	// credentials in $REDIS_URI.
	fs.StringVar(&c.uri, "uri", "", "Redis URI (default $REDIS_URI, else "+defaultURI+"); $REDIS_URI keeps a password off the command line")
	fs.Var(c.opts, "option", "database option `key=value`, such as adbc.redis.read_timeout=30m (repeatable)")
}

// options are the driver's database options: the -option flags and the URI.
func (c *connFlags) options() map[string]string {
	uri := c.uri
	if uri == "" {
		uri = os.Getenv("REDIS_URI")
	}
	if uri == "" {
		uri = defaultURI
	}
	opts := map[string]string{}
	maps.Copy(opts, c.opts)
	opts[adbc.OptionKeyURI] = uri
	return opts
}

// hashFlags are the flags that pick a HASH collection and shape its
// guessed columns.
type hashFlags struct {
	prefix  string
	types   options
	renames options
}

func (h *hashFlags) register(fs *flag.FlagSet) {
	h.types, h.renames = options{}, options{}
	fs.StringVar(&h.prefix, "prefix", "", "the collection's key `prefix`, such as user:")
	fs.Var(h.types, "type", "a column's type, `column=TYPE` such as age=BIGINT (repeatable)")
	fs.Var(h.renames, "rename", "a field's column name, `field=column` (repeatable)")
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: redis-adbc %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
	}
	return fs
}

// parseFlags parses a command's flags; the command takes no arguments.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %q (flags go before them)", fs.Args())
	}
	return nil
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// countList renders "a (3 HASHes), b (1 HASH)", by name.
func countList(m map[string]int64, one, many string) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s (%d %s)", k, m[k], plural(m[k], one, many))
	}
	return strings.Join(parts, ", ")
}

// indexColumns splits an -index-columns list (nil when it is empty).
func indexColumns(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, c := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}
