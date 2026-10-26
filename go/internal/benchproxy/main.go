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

// Command benchproxy delays complete aggregate/cursor responses, not TCP chunks.
// It is a disposable benchmark tool, never part of the driver execution path.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const maxFrame = 128 << 20

// readFrame appends one RESP2/3 frame, including its original wire encoding.
func readFrame(r *bufio.Reader, out *bytes.Buffer, depth int) (byte, error) {
	if depth > 64 {
		return 0, errors.New("RESP nesting limit")
	}
	line, err := r.ReadSlice('\n')
	if err != nil {
		return 0, err
	}
	if len(line) < 3 || line[len(line)-2] != '\r' {
		return 0, errors.New("RESP line terminator")
	}
	kind := line[0]
	body := string(line[1 : len(line)-2])
	out.Write(line)
	switch kind {
	case '$', '!', '=':
		n, err := strconv.Atoi(body)
		if err != nil || n < -1 || n > maxFrame {
			return 0, errors.New("RESP bulk length")
		}
		if n >= 0 {
			if n+2 > maxFrame-out.Len() {
				return 0, errors.New("RESP frame size limit")
			}
			if _, err := io.CopyN(out, r, int64(n+2)); err != nil {
				return 0, err
			}
			data := out.Bytes()
			if !bytes.HasSuffix(data, []byte("\r\n")) {
				return 0, errors.New("RESP bulk terminator")
			}
		}
	case '*', '%', '~', '>', '|':
		n, err := strconv.Atoi(body)
		if err != nil || n < -1 || n > 1000000 {
			return 0, errors.New("RESP array length")
		}
		if kind == '%' || kind == '|' {
			n *= 2
		}
		for i := 0; i < n; i++ {
			if _, err := readFrame(r, out, depth+1); err != nil {
				return 0, err
			}
		}
	case '+', '-', ':', ',', '(', '#', '_':
	default:
		return 0, fmt.Errorf("unsupported RESP %q", kind)
	}
	if out.Len() > maxFrame {
		return 0, errors.New("RESP frame size limit")
	}
	return kind, nil
}

func delayedCommand(raw []byte) bool {
	r := bufio.NewReader(bytes.NewReader(raw))
	line, err := r.ReadString('\n')
	if err != nil || !bytes.HasPrefix([]byte(line), []byte("*")) {
		return false
	}
	line, err = r.ReadString('\n')
	if err != nil || !bytes.HasPrefix([]byte(line), []byte("$")) {
		return false
	}
	n, err := strconv.Atoi(line[1 : len(line)-2])
	if err != nil || n < 0 || n > 128 {
		return false
	}
	cmd := make([]byte, n)
	if _, err := io.ReadFull(r, cmd); err != nil {
		return false
	}
	return bytes.EqualFold(cmd, []byte("FT.AGGREGATE")) || bytes.EqualFold(cmd, []byte("FT.CURSOR"))
}

func relay(client net.Conn, upstream string, delay time.Duration, count, failures *atomic.Int64) {
	defer client.Close()
	server, err := net.Dial("tcp", upstream)
	if err != nil {
		failures.Add(1)
		return
	}
	defer server.Close()
	requests := make(chan bool, 1024)
	finished := make(chan struct{})
	defer close(finished)
	done := make(chan error, 2)
	go func() {
		r, frame := bufio.NewReader(client), &bytes.Buffer{}
		for {
			frame.Reset()
			if _, err := readFrame(r, frame, 0); err != nil {
				done <- err
				return
			}
			select {
			case requests <- delayedCommand(frame.Bytes()):
			case <-finished:
				return
			case <-time.After(time.Minute):
				done <- errors.New("request pipeline timeout")
				return
			}
			if _, err := server.Write(frame.Bytes()); err != nil {
				done <- err
				return
			}
		}
	}()
	go func() {
		r, frame := bufio.NewReader(server), &bytes.Buffer{}
		for {
			frame.Reset()
			kind, err := readFrame(r, frame, 0)
			if err != nil {
				done <- err
				return
			}
			if kind != '>' && kind != '|' {
				select {
				case wait := <-requests:
					if wait {
						time.Sleep(delay)
						count.Add(1)
					}
				case <-finished:
					return
				case <-time.After(time.Minute):
					done <- errors.New("response without request")
					return
				}
			}
			if _, err := client.Write(frame.Bytes()); err != nil {
				done <- err
				return
			}
		}
	}()
	err = <-done
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		// A client normally closes all pooled sockets at process exit.
		if _, ok := err.(*net.OpError); !ok {
			failures.Add(1)
		}
	}
}

func main() {
	endpoint := flag.String("uri", "", "isolated upstream redis:// URI")
	millis := flag.Int("delay-ms", 2, "complete command response delay")
	flag.Parse()
	u, err := url.Parse(*endpoint)
	if err != nil || u.Scheme != "redis" || u.User != nil || u.Hostname() == "" || *millis < 0 {
		panic("invalid isolated Redis URI/delay")
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	var count, failures atomic.Int64
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	go func() { <-signals; listener.Close() }()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"uri": "redis://" + listener.Addr().String() + u.Path})
	for {
		client, err := listener.Accept()
		if err != nil {
			break
		}
		go relay(client, net.JoinHostPort(u.Hostname(), port), time.Duration(*millis)*time.Millisecond, &count, &failures)
	}
	var usage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"delayed_responses": count.Load(), "errors": failures.Load(), "cpu_seconds": float64(usage.Utime.Nano()+usage.Stime.Nano()) / 1e9, "peak_rss_bytes": usage.Maxrss * 1024})
}
