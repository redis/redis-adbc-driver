// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestRESP3Required(t *testing.T) {
	for _, query := range []string{"protocol=2", "protocol=1", "protocol=0", "protocol=4", "protocol=", "protocol=2&protocol=3", "protocol=3&protocol=2", "protocol=2&addr=h2:7001"} {
		t.Run(query, func(t *testing.T) {
			_, err := (&databaseImpl{uri: "redis://h1:7000?" + query}).clientConfig()
			var ae adbc.Error
			if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidArgument || !strings.Contains(err.Error(), "RESP3") {
				t.Fatalf("expected RESP3 invalid argument, got %v", err)
			}
		})
	}
}

func TestRESP3NoFallback(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		for _, mode := range []string{"REJECT_HELLO", "RESP2_HELLO"} {
			t.Run(fmt.Sprintf("cluster=%t/%s", cluster, mode), func(t *testing.T) {
				f := newFakeRedis(t, mode)
				cfg, err := (&databaseImpl{address: f.addr}).clientConfig()
				if err != nil {
					t.Fatal(err)
				}
				client := newClient(cfg, cluster, timeouts{read: time.Second, write: time.Second, dial: time.Second})
				defer client.Close()
				err = client.Ping(context.Background()).Err()
				if err == nil || !strings.Contains(err.Error(), "RESP3") {
					t.Fatalf("expected RESP3 negotiation failure, got %v", err)
				}
				if f.count("PING") != 0 {
					t.Fatal("application command sent after failed RESP3 negotiation")
				}
			})
		}
	}
}
