// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type retainedTestConn struct {
	io.Reader
	fragment int
}

func (c *retainedTestConn) Read(p []byte) (int, error) {
	if c.fragment > 0 {
		p = p[:min(len(p), c.fragment)]
	}
	return c.Reader.Read(p)
}
func (*retainedTestConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*retainedTestConn) Close() error                     { return nil }
func (*retainedTestConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*retainedTestConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*retainedTestConn) SetDeadline(time.Time) error      { return nil }
func (*retainedTestConn) SetReadDeadline(time.Time) error  { return nil }
func (*retainedTestConn) SetWriteDeadline(time.Time) error { return nil }

func TestRetainedBudgetConcurrentReservation(t *testing.T) {
	b := newRetainedBudget(1000)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.reserve(25) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 40 || b.remaining.Load() != 0 {
		t.Fatal(success, b.remaining.Load())
	}
	if err := b.reserve(1); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
	if err := b.reserve(-1); err == nil {
		t.Fatal("negative reservation increased quota")
	}
	if b.remaining.Load() != 0 {
		t.Fatal("failed reservation changed quota")
	}
}

func TestRetainedRESPRejectsLengthsBeforeDecoder(t *testing.T) {
	for _, header := range []string{"$1073741824\r\n", "$9223372036854775807\r\n", "$9223372036854775808\r\n", "$-2\r\n", "*1073741824\r\n", "%9223372036854775807\r\n", "~4097\r\n", ">-1\r\n", "|-1\r\n", "$?\r\n", "*?\r\n", strings.Repeat("x", retainedRESPHeader) + "\r\n"} {
		t.Run(strings.TrimSpace(header[:min(len(header), 30)]), func(t *testing.T) {
			conn := guardRetainedRESP(&retainedTestConn{Reader: strings.NewReader(header), fragment: 1}, newRetainedBudget(64<<20))
			data, err := io.ReadAll(conn)
			if len(data) != 0 || !isStatus(err, adbc.StatusInvalidData) {
				t.Fatalf("unsafe header exposed: %q: %v", data, err)
			}
		})
	}
}

func TestRetainedRESPFragmentedAndNested(t *testing.T) {
	replies := []string{
		"+OK\r\n-ERR bounded\r\n:42\r\n,1.25\r\n#t\r\n_\r\n(12345678901234567890\r\n",
		"$3\r\na\x00b\r\n$-1\r\n$0\r\n\r\n!-1\r\n", // invalid final blob error
		"*3\r\n%1\r\n+k\r\n$5\r\nvalue\r\n~2\r\n:1\r\n:2\r\n*0\r\n",
		"|1\r\n+meta\r\n+value\r\n*1\r\n+reply\r\n>2\r\n+message\r\n+body\r\n",
		"=7\r\ntxt:abc\r\n!3\r\nERR\r\n",
	}
	for index, reply := range replies {
		for _, fragment := range []int{1, 3, 0} {
			conn := guardRetainedRESP(&retainedTestConn{Reader: strings.NewReader(reply), fragment: fragment}, newRetainedBudget(64<<20))
			data, err := io.ReadAll(conn)
			if index == 1 {
				if err == nil || strings.HasSuffix(string(data), "!-1\r\n") {
					t.Fatal("invalid blob error accepted")
				}
				continue
			}
			if err != nil || string(data) != reply {
				t.Fatalf("fragment %d reply %d changed: %q %v", fragment, index, data, err)
			}
			if len(conn.(*retainedRESPConn).stack) != 0 {
				t.Fatal("collection accounting not balanced")
			}
		}
	}
}

func TestRetainedRESPSharedAllocationBudgetAndDepth(t *testing.T) {
	shared := newRetainedBudget(1000)
	for index := 0; index < 2; index++ {
		conn := guardRetainedRESP(&retainedTestConn{Reader: strings.NewReader("$400\r\n" + strings.Repeat("a", 400) + "\r\n")}, shared)
		data, err := io.ReadAll(conn)
		if index == 0 && (err != nil || string(data) != "$400\r\n"+strings.Repeat("a", 400)+"\r\n") {
			t.Fatal(data, err)
		}
		if index == 1 && (len(data) != 0 || !isStatus(err, adbc.StatusInvalidState)) {
			t.Fatal("a new connection reset the shared budget", data, err)
		}
	}
	conn := guardRetainedRESP(&retainedTestConn{Reader: strings.NewReader(strings.Repeat("*1\r\n", retainedRESPDepth+1) + "+ok\r\n")}, newRetainedBudget(64<<20))
	data, err := io.ReadAll(conn)
	if !isStatus(err, adbc.StatusInvalidData) || bytes.Count(data, []byte("*1\r\n")) != retainedRESPDepth {
		t.Fatal("unbounded nesting accepted", len(data), err)
	}
	bulk := guardRetainedRESP(&retainedTestConn{Reader: strings.NewReader("$1000\r\n")}, newRetainedBudget(1000))
	data, err = io.ReadAll(bulk)
	if len(data) != 0 || !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal("bulk allocation was not reserved before its header", data, err)
	}
}

func TestRetainedSourceClientPreservesDialerAndIsolation(t *testing.T) {
	expected := errors.New("private dialer sentinel")
	var mu sync.Mutex
	called := 0
	options := &goredis.Options{Addr: "not-dialed.invalid:1", Protocol: 3, Username: "test-user", Password: "test-password", DisableIdentity: true,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			mu.Lock()
			called++
			mu.Unlock()
			return nil, expected
		}}
	cfg := newClientConfig(options)
	client := newRetainedSourceClient(cfg, false, timeouts{}, newRetainedBudget(64<<20))
	defer client.Close()
	err := client.Ping(context.Background()).Err()
	if !errors.Is(err, expected) {
		t.Fatal("configured dialer was replaced", err)
	}
	if options.MaxRetries != 0 || cfg.single != options {
		t.Fatal("ordinary config was mutated")
	}
	mu.Lock()
	defer mu.Unlock()
	if called == 0 {
		t.Fatal("custom dialer not invoked")
	}
}

func FuzzRetainedRESPAdmission(f *testing.F) {
	for _, seed := range []string{"+OK\r\n", "$1073741824\r\n", "*2\r\n$1\r\nx\r\n%1\r\n+k\r\n:1\r\n", "|1\r\n+k\r\n+v\r\n+reply\r\n", strings.Repeat("*1\r\n", 33) + "_\r\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 64<<10 {
			return
		}
		c := guardRetainedRESP(&retainedTestConn{Reader: bytes.NewReader(p), fragment: 1}, newRetainedBudget(1<<20))
		data, _ := io.ReadAll(c)
		if len(data) > len(p) {
			t.Fatal("RESP guard amplified input")
		}
	})
}

func TestRetainedSourceLifetimeInterruptsBlockedIO(t *testing.T) {
	socket, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := &goredis.Options{Addr: "owned-pipe:1", Protocol: 3, DisableIdentity: true,
		Dialer: func(context.Context, string, string) (net.Conn, error) { return socket, nil }}
	client := newRetainedSourceClient(newClientConfig(options), false, timeouts{}, newRetainedBudget(1<<20), ctx)
	defer client.Close()
	// Read the HELLO request but never answer it. Cancellation must interrupt
	// the private job's socket rather than wait for the ordinary SQL timeout.
	request := make(chan struct{})
	go func() { buffer := make([]byte, 4096); _, _ = server.Read(buffer); close(request) }()
	done := make(chan error, 1)
	go func() { done <- client.Ping(ctx).Err() }()
	select {
	case <-request:
	case <-time.After(3 * time.Second):
		t.Fatal("private source request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked source I/O succeeded after cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("private source cancellation did not interrupt blocked I/O")
	}
}
