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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type fragmentReader struct{ data []byte }

func (r *fragmentReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
func TestFrameSegmentationAndCoalescing(t *testing.T) {
	wire := []byte("%2\r\n+ok\r\n#t\r\n+rows\r\n*2\r\n$3\r\na\r\n\r\n_\r\n:42\r\n")
	for _, input := range []io.Reader{bytes.NewReader(wire), &fragmentReader{data: wire}} {
		reader, output := bufio.NewReader(input), &bytes.Buffer{}
		kind, err := readFrame(reader, output, 0)
		if err != nil || kind != '%' {
			t.Fatalf("frame: %q %v", kind, err)
		}
		if _, err := readFrame(reader, output, 0); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(output.Bytes(), wire) {
			t.Fatal("wire encoding changed")
		}
	}
}
func TestFrameInvalidLengthAndTerminator(t *testing.T) {
	for _, wire := range []string{"$3\r\nabc!!", "$-2\r\n", "*1000001\r\n", "+oops\n"} {
		if _, err := readFrame(bufio.NewReader(bytes.NewBufferString(wire)), &bytes.Buffer{}, 0); err == nil {
			t.Fatalf("accepted %q", wire)
		}
	}
}
func TestCommandDelayScope(t *testing.T) {
	for _, tc := range []struct {
		command string
		delay   bool
	}{{"FT.AGGREGATE", true}, {"ft.cursor", true}, {"HSET", false}, {"PING", false}} {
		var wire bytes.Buffer
		fmt.Fprintf(&wire, "*1\r\n$%d\r\n%s\r\n", len(tc.command), tc.command)
		if delayedCommand(wire.Bytes()) != tc.delay {
			t.Fatal(tc.command)
		}
	}
}

func TestPipelineDelayCountsAndWirePreservation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstreamDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			upstreamDone <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for i := 0; i < 3; i++ {
			if _, err := readFrame(reader, &bytes.Buffer{}, 0); err != nil {
				upstreamDone <- err
				return
			}
			for _, part := range []string{"*1\r\n", "$3\r\n", "abc\r\n"} {
				if _, err := io.WriteString(conn, part); err != nil {
					upstreamDone <- err
					return
				}
			}
		}
		upstreamDone <- nil
	}()
	client, proxy := net.Pipe()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	var count, failures atomic.Int64
	relayDone := make(chan struct{})
	go func() { relay(proxy, listener.Addr().String(), time.Millisecond, &count, &failures); close(relayDone) }()
	// Write and read concurrently as a real socket permits pipelining.
	go io.WriteString(client, "*1\r\n$12\r\nFT.AGGREGATE\r\n*1\r\n$4\r\nPING\r\n*1\r\n$9\r\nFT.CURSOR\r\n")
	reader := bufio.NewReader(client)
	for i := 0; i < 3; i++ {
		output := &bytes.Buffer{}
		if _, err := readFrame(reader, output, 0); err != nil {
			t.Fatal(err)
		}
		if output.String() != "*1\r\n$3\r\nabc\r\n" {
			t.Fatal("response changed")
		}
	}
	if count.Load() != 2 || failures.Load() != 0 {
		t.Fatalf("delay accounting: %d errors %d", count.Load(), failures.Load())
	}
	if err := <-upstreamDone; err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case <-relayDone:
	case <-time.After(5 * time.Second):
		t.Fatal("relay leaked")
	}
}
