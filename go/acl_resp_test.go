// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bufio"
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// An audit helper must not silently treat RESP3 map entries as an empty log.
func TestACLAuditRESP3Entries(t *testing.T) {
	socket, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		rd := bufio.NewReader(server)
		for {
			args, err := readRESPArray(rd)
			if err != nil {
				return
			}
			reply := "-ERR unexpected command\r\n"
			switch strings.ToUpper(args[0]) {
			case "HELLO":
				reply = "%1\r\n+proto\r\n:3\r\n"
			case "CLIENT":
				reply = "+OK\r\n"
			case "ACL":
				reply = "*3\r\n" +
					"%3\r\n+username\r\n+reader\r\n+reason\r\n+key\r\n+object\r\n+private:1\r\n" +
					"%3\r\n+username\r\n+other\r\n+reason\r\n+command\r\n+object\r\n+del\r\n" +
					"%3\r\n+username\r\n+reader\r\n+reason\r\n+key\r\n+object\r\n+private:1\r\n"
			}
			if _, err := server.Write([]byte(reply)); err != nil {
				return
			}
		}
	}()
	client := goredis.NewClient(&goredis.Options{Addr: "audit-pipe:1", Protocol: 3, DisableIdentity: true, MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) { return socket, nil }})
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("audit reply server did not stop")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := &aclHarness{sqlHarness: &sqlHarness{t: t, ctx: ctx}, raw: client}
	if got := a.denials("reader"); !slices.Equal(got, []string{"key private:1"}) {
		t.Fatalf("RESP3 audit entries lost: %v", got)
	}
}
