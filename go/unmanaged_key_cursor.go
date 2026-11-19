// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Key-only scalar discovery does not need generic RESP maps per result. The
// client still owns framing, TLS, routing and push handling; parse its owned raw
// frame strictly and never replay an uncertain or unsupported cursor response.
type onceRawCmd struct{ *goredis.RawCmd }

type rawSearchError string

func (e rawSearchError) Error() string { return string(e) }
func (rawSearchError) RedisError()     {}

func (onceRawCmd) NoRetry() bool { return true }
func (n searchConn) raw(ctx context.Context, args ...any) ([]byte, error) {
	cmd := goredis.NewRawCmd(ctx, args...)
	_ = n.c.Process(ctx, onceRawCmd{cmd})
	raw, err := cmd.Result()
	if err == nil && len(raw) > 0 && (raw[0] == '-' || raw[0] == '!') {
		r := keyRESP{data: raw}
		kind, line, parseErr := r.line()
		if parseErr == nil && kind == '-' && r.at == len(raw) {
			err = rawSearchError(line)
		}
		if parseErr == nil && kind == '!' {
			length, e := strconv.Atoi(string(line))
			if e == nil && length >= 0 && length == len(raw)-r.at-2 && raw[len(raw)-2] == '\r' && raw[len(raw)-1] == '\n' {
				err = rawSearchError(raw[r.at : r.at+length])
			}
		}
		if err == nil {
			err = errorf(adbc.StatusInternal, "invalid raw Search error reply")
		}
	}
	return raw, err
}

func (c *aggCursor) nextKeys(ctx context.Context) ([]string, error) {
	if c.pending {
		c.pending = false
		keys := c.firstKeys
		c.firstKeys = nil
		return keys, nil
	}
	if c.cursor == 0 {
		return nil, io.EOF
	}
	raw, err := c.node.raw(ctx, "FT.CURSOR", "READ", c.index, c.cursor, "COUNT", c.count)
	if err != nil {
		c.close(ctx)
		return nil, wrapRedis(err, "FT.CURSOR READ failed")
	}
	keys, cursor, warning, err := parseUnmanagedKeyCursor(raw)
	if err == nil || cursor != 0 {
		c.cursor = cursor
	}
	if err == nil && c.snapshot && warning {
		err = errorf(adbc.StatusIO, "snapshot cursor returned a partial-result warning")
	}
	if err != nil {
		c.close(ctx)
	}
	return keys, err
}

type keyRESP struct {
	data []byte
	at   int
}

func (r *keyRESP) line() (byte, []byte, error) {
	if r.at >= len(r.data) {
		return 0, nil, io.ErrUnexpectedEOF
	}
	kind := r.data[r.at]
	r.at++
	n := bytes.IndexByte(r.data[r.at:], '\n')
	if n < 1 || r.data[r.at+n-1] != '\r' {
		return 0, nil, fmt.Errorf("invalid RESP line")
	}
	value := r.data[r.at : r.at+n-1]
	r.at += n + 1
	return kind, value, nil
}
func (r *keyRESP) count(want byte) (int, error) {
	kind, raw, err := r.line()
	if err != nil {
		return 0, err
	}
	if kind != want {
		return 0, fmt.Errorf("unexpected RESP type %q", kind)
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 0 || n > len(r.data)-r.at {
		return 0, fmt.Errorf("invalid RESP length")
	}
	return n, nil
}
func (r *keyRESP) text() (string, error) {
	kind, line, err := r.line()
	if err != nil {
		return "", err
	}
	if kind == '+' {
		return string(line), nil
	}
	if kind != '$' {
		return "", fmt.Errorf("unexpected key RESP type %q", kind)
	}
	n, err := strconv.Atoi(string(line))
	if err != nil || n < 0 || n > len(r.data)-r.at-2 {
		return "", fmt.Errorf("invalid RESP string length")
	}
	if r.data[r.at+n] != '\r' || r.data[r.at+n+1] != '\n' {
		return "", fmt.Errorf("invalid RESP string terminator")
	}
	value := string(r.data[r.at : r.at+n])
	r.at += n + 2
	return value, nil
}
func (r *keyRESP) skip(depth int) error {
	if depth > 64 {
		return fmt.Errorf("RESP nesting limit")
	}
	kind, line, err := r.line()
	if err != nil {
		return err
	}
	switch kind {
	case '+', '-', ':', ',', '(', '#', '_':
		return nil
	case '$', '!', '=':
		n, err := strconv.Atoi(string(line))
		if err != nil || n < -1 {
			return fmt.Errorf("invalid RESP blob length")
		}
		if n == -1 {
			return nil
		}
		if n > len(r.data)-r.at-2 || r.data[r.at+n] != '\r' || r.data[r.at+n+1] != '\n' {
			return fmt.Errorf("invalid RESP blob")
		}
		r.at += n + 2
		return nil
	case '*', '~', '>', '%', '|':
		n, err := strconv.Atoi(string(line))
		if err != nil || n < -1 {
			return fmt.Errorf("invalid RESP container length")
		}
		if n == -1 {
			return nil
		}
		if n > len(r.data)-r.at {
			return fmt.Errorf("invalid RESP container size")
		}
		if kind == '%' || kind == '|' {
			if n > (len(r.data)-r.at)/2 {
				return fmt.Errorf("invalid RESP map size")
			}
			n *= 2
		}
		for range n {
			if err := r.skip(depth + 1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported RESP type %q", kind)
	}
}
func (r *keyRESP) warning(depth int) (bool, error) {
	if depth > 64 || r.at >= len(r.data) {
		return true, fmt.Errorf("invalid warning reply")
	}
	probe := *r
	kind, line, err := probe.line()
	if err != nil {
		return true, err
	}
	if kind == '_' || (kind == '$' || kind == '*') && string(line) == "-1" {
		*r = probe
		return depth != 0, nil
	}
	switch r.data[r.at] {
	case '$', '+':
		s, err := r.text()
		return s != "None", err
	case '*':
		n, err := r.count('*')
		if err != nil {
			return true, err
		}
		warning := false
		for range n {
			w, err := r.warning(depth + 1)
			if err != nil {
				return true, err
			}
			warning = warning || w
		}
		return warning, nil
	default:
		return true, r.skip(depth)
	}
}
func (r *keyRESP) attributes() (string, error) {
	n, err := r.count('%')
	if err != nil {
		return "", err
	}
	key := ""
	found := false
	for range n {
		name, err := r.text()
		if err != nil {
			return "", err
		}
		if name == keyField {
			key, err = r.text()
			found = true
		} else {
			err = r.skip(0)
		}
		if err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("missing unmanaged discovery key")
	}
	if !unmanagedSourceKey(key) {
		return "", errorf(adbc.StatusInvalidArgument, "unmanaged index contains a key overlapping driver storage")
	}
	return key, nil
}
func (r *keyRESP) result() (string, error) {
	n, err := r.count('%')
	if err != nil {
		return "", err
	}
	key := ""
	found := false
	for range n {
		name, err := r.text()
		if err != nil {
			return "", err
		}
		if name == "extra_attributes" {
			key, err = r.attributes()
			found = true
		} else {
			err = r.skip(0)
		}
		if err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("missing unmanaged discovery attributes")
	}
	return key, nil
}
func parseUnmanagedKeyCursor(raw []byte) (keys []string, cursor int64, warning bool, err error) {
	defer func() {
		if err != nil {
			var ae adbc.Error
			if !errors.As(err, &ae) {
				err = errorf(adbc.StatusInternal, "invalid unmanaged key cursor reply: %v", err)
			}
			keys = nil
		}
	}()
	r := keyRESP{data: raw}
	n, err := r.count('*')
	if err != nil {
		return nil, 0, false, err
	}
	if n != 2 {
		return nil, 0, false, fmt.Errorf("expected cursor pair")
	}
	start := r.at
	// Recover the cursor before interpreting row semantics so parse failures can
	// delete the correct server cursor without issuing its READ a second time.
	if err = r.skip(0); err != nil {
		return nil, 0, false, err
	}
	kind, line, err := r.line()
	if err != nil {
		return nil, 0, false, err
	}
	if kind != ':' {
		return nil, 0, false, fmt.Errorf("invalid cursor id type")
	}
	cursor, err = strconv.ParseInt(string(line), 10, 64)
	if err != nil || cursor < 0 {
		return nil, 0, false, fmt.Errorf("invalid cursor id")
	}
	if r.at != len(raw) {
		return nil, cursor, false, fmt.Errorf("trailing cursor data")
	}
	r.at = start
	n, err = r.count('%')
	if err != nil {
		return nil, cursor, false, err
	}
	found := false
	for range n {
		name, e := r.text()
		if e != nil {
			return nil, cursor, false, e
		}
		switch name {
		case "results":
			count, e := r.count('*')
			if e != nil {
				return nil, cursor, false, e
			}
			keys = make([]string, 0, count)
			found = true
			for range count {
				key, e := r.result()
				if e != nil {
					return nil, cursor, false, e
				}
				keys = append(keys, key)
			}
		case "warning", "warnings":
			w, e := r.warning(0)
			if e != nil {
				return nil, cursor, false, e
			}
			warning = warning || w
		default:
			if e := r.skip(0); e != nil {
				return nil, cursor, false, e
			}
		}
	}
	if !found {
		return nil, cursor, false, fmt.Errorf("missing discovery results")
	}
	return keys, cursor, warning, nil
}
