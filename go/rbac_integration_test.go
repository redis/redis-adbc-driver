// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// Requires an explicitly disposable Software database and its management API.
// The test provisions and deletes its own ACL, role and user; preserves all
// existing database role bindings. The local TLS proxy trusts the disposable
// Software API's self-signed certificate; production clients verify TLS.
func TestSQLRBACSelect(t *testing.T) {
	api := os.Getenv("REDIS_RBAC_TEST_API")
	if api == "" {
		t.Skip("set REDIS_RBAC_TEST_API and REDIS_URI for disposable Software RBAC tests")
	}
	if os.Getenv("REDIS_URI") == "" {
		t.Skip("set REDIS_URI")
	}
	upstream, err := url.Parse(api)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // disposable fixture only
	var failWrite atomic.Bool
	proxy.ModifyResponse = func(res *http.Response) error {
		if res.Request.Method == "PUT" && strings.HasPrefix(res.Request.URL.Path, "/v1/redis_acls/") && failWrite.Load() {
			res.StatusCode = 503
		}
		return nil
	}
	server := httptest.NewTLSServer(proxy)
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "api-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := rbacConfig{APIURL: server.URL, Username: os.Getenv("REDIS_RBAC_TEST_USER"), Password: os.Getenv("REDIS_RBAC_TEST_PASSWORD"), CAFile: ca, DatabaseUID: 1, Roles: map[string]rbacBinding{"placeholder": {1, 1}}}
	if cfg.Username == "" {
		cfg.Username = "admin@example.com"
	}
	if cfg.Password == "" {
		cfg.Password = "adbc-software-test"
	}
	raw, _ := json.Marshal(cfg)
	manager, err := newRBAC(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	call := func(method, path string, body, out any) {
		t.Helper()
		if err := fixtureRBACRequest(manager, ctx, method, path, body, out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	var dbs []rbacDatabase
	call("GET", "bdbs", nil, &dbs)
	if len(dbs) != 1 {
		t.Fatal("RBAC test requires an isolated, single-database Software instance")
	}
	cfg.DatabaseUID = dbs[0].UID
	roleName := "adbc_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var role struct {
		UID int `json:"uid"`
	}
	call("POST", "roles", map[string]any{"name": roleName, "management": "none"}, &role)
	defer fixtureRBACRequest(manager, ctx, "DELETE", "roles/"+strconv.Itoa(role.UID), nil, nil)
	var acl rbacACL
	call("POST", "redis_acls", map[string]string{"name": rbacACLName(cfg.DatabaseUID, role.UID), "acl": rbacBaseACL}, &acl)
	defer fixtureRBACRequest(manager, ctx, "DELETE", "redis_acls/"+strconv.Itoa(acl.UID), nil, nil)
	dbpath := "bdbs/" + strconv.Itoa(cfg.DatabaseUID)
	original := dbs[0].Permissions
	call("PUT", dbpath, map[string]any{"roles_permissions": append(slices.Clone(original), rbacPermission{role.UID, acl.UID})}, nil)
	defer fixtureRBACRequest(manager, ctx, "PUT", dbpath, map[string]any{"roles_permissions": original}, nil)
	user := roleName + "@example.com"
	password := "RBAC-test-" + uuid.NewString()
	var created struct {
		UID int `json:"uid"`
	}
	call("POST", "users", map[string]any{"name": roleName, "email": user, "password": password, "role_uids": []int{role.UID}}, &created)
	defer fixtureRBACRequest(manager, ctx, "DELETE", "users/"+strconv.Itoa(created.UID), nil, nil)
	cfg.Roles = map[string]rbacBinding{roleName: {role.UID, acl.UID}}
	raw, _ = json.Marshal(cfg)
	open := func(uri, rbac string) *sqlHarness {
		t.Helper()
		opts := map[string]string{adbc.OptionKeyURI: uri}
		if rbac != "" {
			opts[OptionStringRBAC] = rbac
		}
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Open(ctx)
		deadline := time.Now().Add(20 * time.Second)
		for err != nil && strings.Contains(err.Error(), "WRONGPASS") && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			conn, err = db.Open(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close(ctx); db.Close(ctx) })
		return &sqlHarness{t: t, ctx: ctx, conn: conn}
	}
	admin := open(os.Getenv("REDIS_URI"), string(raw))
	admin.dropTables("it_rbac_allowed", "it_rbac_denied", "it_rbac_renamed")
	admin.exec(`CREATE TABLE it_rbac_allowed (id integer)`)
	admin.exec(`INSERT INTO it_rbac_allowed VALUES (42)`)
	admin.exec(`CREATE TABLE it_rbac_denied (id integer)`)
	admin.exec(`INSERT INTO it_rbac_denied VALUES (99)`)
	uri, _ := url.Parse(os.Getenv("REDIS_URI"))
	uri.User = url.UserPassword(user, password)
	var reader *sqlHarness
	// User provisioning is propagated asynchronously to the data proxy.
	deadline := time.Now().Add(20 * time.Second)
	for {
		db, e := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{adbc.OptionKeyURI: uri.String()})
		if e != nil {
			t.Fatal(e)
		}
		conn, e := db.Open(ctx)
		if e == nil {
			reader = &sqlHarness{t: t, ctx: ctx, conn: conn}
			defer conn.Close(ctx)
			defer db.Close(ctx)
			break
		}
		db.Close(ctx)
		if time.Now().After(deadline) {
			t.Fatal("reader provisioning failed:", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	reader.expectError(`SELECT * FROM it_rbac_allowed`, "NOPERM")
	grant := `GRANT SELECT ON TABLE it_rbac_allowed TO ` + roleName
	admin.exec(grant)
	admin.exec(grant) // idempotent
	// A complete fetched result is required, including after proxy propagation.
	waitRows := func(sql, want string) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			st, _ := reader.conn.NewStatement(ctx)
			st.SetSqlQuery(ctx, sql)
			rr, _, e := st.ExecuteQuery(ctx)
			if e == nil {
				for rr.Next() {
				}
				e = rr.Err()
				rr.Release()
			}
			st.Close(ctx)
			if e == nil {
				reader.expectRows(sql, want)
				return
			}
			if time.Now().After(deadline) {
				t.Fatal(e)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitRows(`SELECT id FROM it_rbac_allowed`, "42")
	reader.expectError(`SELECT * FROM it_rbac_denied`, "NOPERM")
	if err := reader.execErr(`INSERT INTO it_rbac_allowed VALUES (7)`); err == nil {
		t.Fatal("reader could write")
	}
	for _, view := range []string{"role_table_grants", "table_privileges"} {
		admin.expectRows(`SELECT grantee, privilege_type FROM information_schema.`+view+` WHERE table_schema='public' AND table_name='it_rbac_allowed'`, roleName+"|SELECT")
	}
	admin.exec(`ALTER TABLE it_rbac_allowed RENAME TO it_rbac_renamed`)
	waitRows(`SELECT id FROM it_rbac_renamed`, "42")
	admin.exec(`REVOKE SELECT ON it_rbac_renamed FROM ` + roleName)
	admin.exec(`REVOKE SELECT ON it_rbac_renamed FROM ` + roleName)
	deadline = time.Now().Add(20 * time.Second)
	for {
		st, _ := reader.conn.NewStatement(ctx)
		st.SetSqlQuery(ctx, `SELECT id FROM it_rbac_renamed`)
		rr, _, e := st.ExecuteQuery(ctx)
		if e == nil {
			for rr.Next() {
			}
			e = rr.Err()
			rr.Release()
		}
		st.Close(ctx)
		if e != nil && strings.Contains(e.Error(), "NOPERM") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revocation did not deny fetched results", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	admin.expectRows(`SELECT count(*) FROM information_schema.role_table_grants WHERE grantee='`+roleName+`'`, "0")
	// A prefix-changing TRUNCATE fails closed until re-granted. REVOKE still
	// removes the earlier physical grant using the stable table identity.
	admin.exec(`GRANT SELECT ON it_rbac_renamed TO ` + roleName)
	admin.exec(`TRUNCATE it_rbac_renamed`)
	admin.exec(`INSERT INTO it_rbac_renamed VALUES (84)`)
	reader.expectError(`SELECT * FROM it_rbac_renamed`, "NOPERM")
	admin.exec(`REVOKE SELECT ON it_rbac_renamed FROM ` + roleName)
	var afterTruncate rbacACL
	call("GET", "redis_acls/"+strconv.Itoa(acl.UID), nil, &afterTruncate)
	if afterTruncate.ACL != rbacBaseACL {
		t.Fatal("revocation retained the old prefix")
	}
	admin.exec(`GRANT SELECT ON it_rbac_renamed TO ` + roleName)
	waitRows(`SELECT id FROM it_rbac_renamed`, "84")
	admin.exec(`REVOKE SELECT ON it_rbac_renamed FROM ` + roleName)

	// An unrelated ACL remains byte-for-byte unchanged; no membership mutation.
	var actual rbacDatabase
	call("GET", dbpath, nil, &actual)
	if len(actual.Permissions) != len(original)+1 {
		t.Fatal("changed unrelated bindings")
	}
	var roleAfter struct {
		UID  int    `json:"uid"`
		Name string `json:"name"`
	}
	call("GET", "roles/"+strconv.Itoa(role.UID), nil, &roleAfter)
	if roleAfter.Name != roleName {
		t.Fatal("changed role")
	}
	// Serialize across driver processes and reject a second writer.
	opts, err := goredis.ParseURL(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureTLSRoots(opts, os.Getenv("SSL_CERT_FILE")); err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(opts)
	defer client.Close()
	client.Set(ctx, metaPrefix+"rbac:lock", "other writer", 0)
	if e := admin.execErr(`GRANT SELECT ON it_rbac_renamed TO ` + roleName); e == nil || !strings.Contains(e.Error(), "locked") {
		t.Fatal(e)
	}
	client.Del(ctx, metaPrefix+"rbac:lock")
	// Simulate an accepted PUT whose response reports failure. The lock must
	// remain, so another writer cannot overwrite an ambiguous outcome.
	failWrite.Store(true)
	if e := admin.execErr(`GRANT SELECT ON it_rbac_renamed TO ` + roleName); e == nil || !strings.Contains(e.Error(), "lock retained") {
		t.Fatal(e)
	}
	if n := client.Exists(ctx, metaPrefix+"rbac:lock").Val(); n != 1 {
		t.Fatal("lost uncertain-write lock")
	}
	failWrite.Store(false)
	// Explicit fixture reconciliation restores the last committed empty ACL.
	call("PUT", "redis_acls/"+strconv.Itoa(acl.UID), map[string]string{"acl": rbacBaseACL}, nil)
	client.Del(ctx, metaPrefix+"rbac:lock")

	// Provision an independent ACL and a mixed-role user. Rejected SQL must
	// leave both ACLs, the ledger and the user's memberships unchanged.
	var independentRole struct {
		UID int `json:"uid"`
	}
	call("POST", "roles", map[string]any{"name": roleName + "_other", "management": "none"}, &independentRole)
	var currentGrants []rbacGrant
	// Read the current physical prefix from driver metadata through its catalog.
	rows, _ := admin.query(`SELECT key_prefix FROM information_schema.tables WHERE table_name='it_rbac_renamed' AND table_schema='public'`)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	var independentACL rbacACL
	call("POST", "redis_acls", map[string]string{"name": roleName + "_independent", "acl": rbacACLString([]string{rows[0]})}, &independentACL)
	bound := append(slices.Clone(original), rbacPermission{role.UID, acl.UID})
	var independentUser struct {
		UID int `json:"uid"`
	}
	defer func() {
		fixtureRBACRequest(manager, ctx, "DELETE", "users/"+strconv.Itoa(independentUser.UID), nil, nil)
		fixtureRBACRequest(manager, ctx, "PUT", dbpath, map[string]any{"roles_permissions": bound}, nil)
		fixtureRBACRequest(manager, ctx, "DELETE", "redis_acls/"+strconv.Itoa(independentACL.UID), nil, nil)
		fixtureRBACRequest(manager, ctx, "DELETE", "roles/"+strconv.Itoa(independentRole.UID), nil, nil)
	}()
	call("PUT", dbpath, map[string]any{"roles_permissions": append(slices.Clone(bound), rbacPermission{independentRole.UID, independentACL.UID})}, nil)
	independentEmail := roleName + "_both@example.com"
	call("POST", "users", map[string]any{"name": roleName + "_both", "email": independentEmail, "password": password, "role_uids": []int{role.UID, independentRole.UID}}, &independentUser)
	// The mixed-role reader is outside the supported authorization contract.
	// Retain this regression: SQL must fail before changing the dedicated ACL.
	if err := admin.execErr(`GRANT SELECT ON it_rbac_renamed TO ` + roleName); err == nil || !strings.Contains(err.Error(), "single Redis role") {
		t.Fatal(err)
	}
	var memberships struct {
		Roles []int `json:"role_uids"`
	}
	call("GET", "users/"+strconv.Itoa(independentUser.UID), nil, &memberships)
	if len(memberships.Roles) != 2 || !slices.Contains(memberships.Roles, role.UID) || !slices.Contains(memberships.Roles, independentRole.UID) {
		t.Fatal("changed mixed-role memberships")
	}
	var dedicatedAfter rbacACL
	call("GET", "redis_acls/"+strconv.Itoa(acl.UID), nil, &dedicatedAfter)
	if dedicatedAfter.ACL != rbacBaseACL {
		t.Fatal("changed ACL for unsupported mixed-role binding")
	}

	var unchanged rbacACL
	call("GET", "redis_acls/"+strconv.Itoa(independentACL.UID), nil, &unchanged)
	if unchanged.ACL != independentACL.ACL {
		t.Fatal("changed independent ACL")
	}
	ledgerRaw, _ := client.Get(ctx, metaPrefix+"rbac:grants:"+strconv.Itoa(cfg.DatabaseUID)+":"+strconv.Itoa(acl.UID)).Result()
	json.Unmarshal([]byte(ledgerRaw), &currentGrants)
	if len(currentGrants) != 0 {
		t.Fatal("revocation left managed grants")
	}
	client.Del(ctx, metaPrefix+"rbac:grants:"+strconv.Itoa(cfg.DatabaseUID)+":"+strconv.Itoa(acl.UID))
}

// Software serializes database configuration tasks. Retry only explicit busy
// responses while provisioning/cleaning our disposable fixture, not SQL writes.
func fixtureRBACRequest(r *rbacManager, ctx context.Context, method, path string, body, out any) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := r.request(ctx, method, path, body, out)
		if err == nil || !strings.Contains(err.Error(), "HTTP 409") || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
