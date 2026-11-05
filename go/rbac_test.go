// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func testRBAC(t *testing.T, handler http.HandlerFunc) (*rbacManager, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := rbacConfig{APIURL: server.URL, Username: "manager", Password: "management-secret", CAFile: ca, DatabaseUID: 1, Roles: map[string]rbacBinding{"analyst": {7, 9}}}
	raw, _ := json.Marshal(cfg)
	manager, err := newRBAC(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.client.CloseIdleConnections)
	return manager, string(raw)
}

func TestRBACManagementBoundaries(t *testing.T) {
	for _, mode := range []string{"valid", "shared-role", "shared-database", "missing-binding", "wrong-role", "wrong-acl-name", "extra-rule", "multiple-roles", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			manager, _ := testRBAC(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("unexpected mutation: %s", r.Method)
				}
				user, pass, ok := r.BasicAuth()
				if !ok || user != "manager" || pass != "management-secret" {
					t.Error("missing authentication")
				}
				if mode == "http-error" {
					w.WriteHeader(403)
					w.Write([]byte("management-secret"))
					return
				}
				var out any
				switch r.URL.Path {
				case "/v1/roles/7":
					name := "analyst"
					if mode == "wrong-role" {
						name = "other"
					}
					out = map[string]any{"uid": 7, "name": name}
				case "/v1/bdbs":
					perms := []rbacPermission{{7, 9}}
					dbs := []rbacDatabase{{1, perms}}
					if mode == "shared-role" {
						dbs[0].Permissions = append(perms, rbacPermission{8, 9})
					}
					if mode == "shared-database" {
						dbs = append(dbs, rbacDatabase{2, perms})
					}
					if mode == "missing-binding" {
						dbs[0].Permissions = nil
					}
					out = dbs
				case "/v1/users":
					roles := []int{7}
					if mode == "multiple-roles" {
						roles = append(roles, 8)
					}
					out = []map[string]any{{"role_uids": roles}}
				case "/v1/redis_acls/9":
					name := rbacACLName(1, 7)
					acl := rbacBaseACL
					if mode == "wrong-acl-name" {
						name = "administrator-acl"
					}
					if mode == "extra-rule" {
						acl += " +@all"
					}
					out = rbacACL{9, name, acl}
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				json.NewEncoder(w).Encode(out)
			})
			_, _, err := manager.readRole(context.Background(), "analyst")
			if (err == nil) != (mode == "valid") {
				t.Fatal(err)
			}
			if err != nil && strings.Contains(err.Error(), "management-secret") {
				t.Fatal("leaked management secret")
			}
		})
	}
}

func TestRBACConfigAndTLS(t *testing.T) {
	manager, raw := testRBAC(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]any{}) })
	var cfg map[string]any
	json.Unmarshal([]byte(raw), &cfg)
	for _, key := range []string{"api_url", "username", "password", "database_uid", "roles"} {
		t.Run(key, func(t *testing.T) {
			cp := map[string]any{}
			for k, v := range cfg {
				cp[k] = v
			}
			delete(cp, key)
			b, _ := json.Marshal(cp)
			if _, err := newRBAC(string(b)); err == nil {
				t.Fatal("accepted missing", key)
			}
		})
	}
	for _, origin := range []string{"http://localhost", "https://user:pass@localhost", "https://localhost/path", "https://localhost?secret=x"} {
		next := manager.cfg
		next.APIURL = origin
		b, _ := json.Marshal(next)
		if _, err := newRBAC(string(b)); err == nil {
			t.Fatal("accepted", origin)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.request(ctx, "GET", "roles/7", nil, new(any)); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal("lost cancellation status", err)
	}
	// Without the explicit test CA, the API certificate must not be trusted.
	next := manager.cfg
	next.CAFile = ""
	b, _ := json.Marshal(next)
	untrusted, err := newRBAC(string(b))
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.client.CloseIdleConnections()
	if err := untrusted.request(context.Background(), "GET", "roles/7", nil, new(any)); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	// Configuration cannot be fetched back with its embedded management password.
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(context.Background(), map[string]string{OptionStringRBAC: raw})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := db.GetOption(context.Background(), OptionStringRBAC); err == nil || strings.Contains(value, "management-secret") {
		t.Fatal("RBAC option must be write-only", value, err)
	}
	if _, err := ParseScript(`GRANT INSERT ON t TO analyst`); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
}
