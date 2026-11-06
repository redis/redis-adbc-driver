// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// OptionStringRBAC is a database-only, write-only JSON management configuration.
const OptionStringRBAC = "adbc.redis.rbac"

type rbacBinding struct {
	RoleUID int `json:"role_uid"`
	ACLUID  int `json:"acl_uid"`
}
type rbacConfig struct {
	APIURL      string                 `json:"api_url"`
	Username    string                 `json:"username"`
	Password    string                 `json:"password"`
	CAFile      string                 `json:"ca_file"`
	DatabaseUID int                    `json:"database_uid"`
	Roles       map[string]rbacBinding `json:"roles"`
}
type rbacManager struct {
	cfg    rbacConfig
	client *http.Client
}

// The baseline reveals metadata and index names, but permits no row reads until
// a prefix is added. Command permissions never include writes or Search DDL.
// Keep rules additive; resetting command/key permissions is outside our ownership.
// Import capture journals contain row values. Keep metadata grants positive:
// a wildcard over workspace values would reveal unpublished captured payloads.
const rbacBaseACL = "+ping +info +ft._list +ft.info +ft.aggregate +ft.cursor +ft.search +ft.profile +get +exists +hget +hmget +hgetall +smembers +sismember +zmscore" +
	" %R~adbc:schemas %R~" + snapshotCatalogKey + " %R~" + snapshotPrefix + "metadata:*" +
	" %R~" + snapshotPrefix + "workspace:*:__keys" +
	" %R~" + snapshotPrefix + "workspace:*:schemas" +
	" %R~" + snapshotPrefix + "workspace:*:namespace:*" +
	" %R~" + snapshotPrefix + "workspace:*:tables:*" +
	" %R~" + snapshotPrefix + "workspace:*:table:*" +
	" %R~" + snapshotPrefix + "workspace:*:views:*" +
	" %R~" + snapshotPrefix + "workspace:*:view:*" +
	" %R~" + snapshotPrefix + "workspace:*:sql-sequences:*" +
	" %R~" + snapshotPrefix + "workspace:*:sql-sequence:*" +
	" %R~" + snapshotPrefix + "workspace:*:sql-index:*" +
	" %R~" + snapshotPrefix + "workspace:*:seq:*" +
	" %R~" + snapshotPrefix + "workspace:*:sequence-state:*"

// ACL selectors keep EVAL's declared-key envelope separate from commands a
// script may execute. Only this backend role/ACL domain HASH is writable; SADD registers
// retention but cannot remove another principal's namespace.
func rbacReaderACL(domain string) string {
	own := snapshotReaderPrefix + snapshotReaderNamespace(domain)
	return rbacBaseACL + " +time +scard +hlen +hstrlen +zcard +zrange +zrangebylex +zscore +zlexcount +memory|usage %R~" + snapshotReaderDirectory + " %R~" + snapshotReaderDomains + " %R~" + snapshotReaderPrefix + "* %R~" + snapshotPrefix + "members:*" +
		" (+eval ~" + snapshotCatalogKey + " ~" + snapshotPrefix + "*)" +
		" (+hset +hdel ~" + own + ") (+sadd ~" + snapshotReaderDirectory + ")"
}

// SnapshotReaderPermissions returns the namespace and canonical baseline ACL
// for one backend authorization domain. Users sharing the ACL share this trust
// boundary. SQL RBAC uses the dedicated ACL name as domain; row SELECT grants
// are added separately. Register the returned namespace as an administrator.
type SnapshotReaderPolicy struct {
	Format    int    `json:"format"`
	Namespace string `json:"namespace"`
	ACL       string `json:"acl"`
	Registry  string `json:"registry"`
}

func SnapshotReaderPermissions(domain string) SnapshotReaderPolicy {
	return SnapshotReaderPolicy{16, snapshotReaderNamespace(domain), rbacReaderACL(domain), snapshotReaderDomains}
}

func newRBAC(raw string) (*rbacManager, error) {
	var cfg rbacConfig
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid RBAC configuration JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid RBAC configuration JSON")
	}
	u, err := url.Parse(cfg.APIURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || cfg.DatabaseUID <= 0 || cfg.Username == "" || cfg.Password == "" || len(cfg.Roles) == 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "RBAC requires an HTTPS API origin, management credentials, database_uid and roles")
	}
	seen := map[int]bool{}
	for role, b := range cfg.Roles {
		if role == "" || b.RoleUID <= 0 || b.ACLUID <= 0 || seen[b.ACLUID] {
			return nil, errorf(adbc.StatusInvalidArgument, "RBAC role bindings must use distinct positive ACL and role IDs")
		}
		seen[b.ACLUID] = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errorf(adbc.StatusInvalidArgument, "cannot read RBAC CA file")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errorf(adbc.StatusInvalidArgument, "invalid RBAC CA file")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	return &rbacManager{cfg, &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (r *rbacManager) request(ctx context.Context, method, path string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errorf(adbc.StatusInternal, "cannot encode RBAC request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.APIURL+"/v1/"+path, bytes.NewReader(data))
	if err != nil {
		return errorf(adbc.StatusInvalidArgument, "invalid RBAC request")
	}
	req.SetBasicAuth(r.cfg.Username, r.cfg.Password)
	req.Header.Set("Content-Type", "application/json")
	res, err := r.client.Do(req)
	// Do not expose server bodies or transport errors containing URLs/credentials.
	if err != nil {
		if cause := cancelled(ctx); cause != nil {
			return cause
		}
		return errorf(adbc.StatusIO, "RBAC management API request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errorf(adbc.StatusIO, "RBAC management API returned HTTP %d", res.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
			return errorf(adbc.StatusIO, "invalid RBAC management response")
		}
	}
	return nil
}

type rbacACL struct {
	Domain string `json:"-"`
	UID    int    `json:"uid"`
	Name   string `json:"name"`
	ACL    string `json:"acl"`
}
type rbacPermission struct {
	RoleUID int `json:"role_uid"`
	ACLUID  int `json:"redis_acl_uid"`
}
type rbacDatabase struct {
	UID         int              `json:"uid"`
	Permissions []rbacPermission `json:"roles_permissions"`
}

func rbacACLName(db, role int) string { return fmt.Sprintf("adbc-select-db%d-role%d", db, role) }

// Only canonical ACLs owned by this feature can be changed. Other ACLs and role
// assignments are never written. Refuse ACLs reused by any other binding.
func (r *rbacManager) readRole(ctx context.Context, role string) (rbacACL, []string, error) {
	b, ok := r.cfg.Roles[role]
	if !ok {
		return rbacACL{}, nil, errorf(adbc.StatusInvalidArgument, "SQL role %q has no Redis RBAC binding", role)
	}
	var rr struct {
		UID  int    `json:"uid"`
		Name string `json:"name"`
	}
	if err := r.request(ctx, "GET", "roles/"+strconv.Itoa(b.RoleUID), nil, &rr); err != nil {
		return rbacACL{}, nil, err
	}
	if rr.UID != b.RoleUID || rr.Name != role {
		return rbacACL{}, nil, errorf(adbc.StatusInvalidArgument, "RBAC role name/ID mismatch")
	}
	var dbs []rbacDatabase
	if err := r.request(ctx, "GET", "bdbs", nil, &dbs); err != nil {
		return rbacACL{}, nil, err
	}
	matches := 0
	for _, db := range dbs {
		for _, p := range db.Permissions {
			if p.ACLUID == b.ACLUID {
				if db.UID != r.cfg.DatabaseUID || p.RoleUID != b.RoleUID {
					return rbacACL{}, nil, errorf(adbc.StatusInvalidArgument, "RBAC ACL is shared with another database or role")
				}
				matches++
			}
		}
	}
	if matches != 1 {
		return rbacACL{}, nil, errorf(adbc.StatusInvalidArgument, "RBAC ACL must already be bound to the configured database and role")
	}
	// Combined-role ACL behavior is not a supported SQL authorization mapping.
	// Fail before writing if any user of this role has additional memberships.
	var users []struct {
		Roles []int  `json:"role_uids"`
		Email string `json:"email"`
	}
	if err := r.request(ctx, "GET", "users", nil, &users); err != nil {
		return rbacACL{}, nil, err
	}
	// A role is a backend authorization domain. Multiple users may share it,
	// but additive memberships still have no unambiguous SQL-role mapping.
	for _, user := range users {
		if slices.Contains(user.Roles, b.RoleUID) && len(user.Roles) != 1 {
			return rbacACL{}, nil, errorf(adbc.StatusNotImplemented, "RBAC SELECT grants require users with a single Redis role")
		}
	}
	domain := rbacACLName(r.cfg.DatabaseUID, b.RoleUID)
	var acl rbacACL
	if err := r.request(ctx, "GET", "redis_acls/"+strconv.Itoa(b.ACLUID), nil, &acl); err != nil {
		return acl, nil, err
	}
	if acl.UID != b.ACLUID || acl.Name != rbacACLName(r.cfg.DatabaseUID, b.RoleUID) {
		return acl, nil, errorf(adbc.StatusInvalidArgument, "RBAC ACL is not dedicated to this binding")
	}
	acl.Domain = domain
	prefixes, err := rbacPrefixes(acl.ACL, rbacReaderACL(domain))
	return acl, prefixes, err
}

func safeRBACPrefix(prefix string) bool {
	if prefix == "" || strings.HasPrefix(prefix, "adbc:") {
		return false
	}
	for _, c := range prefix {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_:{}.-%~", c)) {
			return false
		}
	}
	return true
}
func rbacACLString(prefixes []string, base ...string) string {
	prefixes = slices.Clone(prefixes)
	slices.Sort(prefixes)
	prefixes = slices.Compact(prefixes)
	s := rbacBaseACL
	if len(base) > 0 {
		s = base[0]
	}
	for _, prefix := range prefixes {
		s += " %R~" + prefix + "*"
	}
	return s
}
func rbacPrefixes(acl string, bases ...string) ([]string, error) {
	base := rbacBaseACL
	if len(bases) > 0 {
		base = bases[0]
	}
	var prefixes []string
	if acl == base {
		return prefixes, nil
	}
	if strings.HasPrefix(acl, base+" ") {
		for _, token := range strings.Split(strings.TrimPrefix(acl, base+" "), " ") {
			prefix := strings.TrimSuffix(strings.TrimPrefix(token, "%R~"), "*")
			if !strings.HasPrefix(token, "%R~") || !strings.HasSuffix(token, "*") || !safeRBACPrefix(prefix) {
				return nil, errorf(adbc.StatusInvalidArgument, "RBAC ACL contains unmanaged rules")
			}
			prefixes = append(prefixes, prefix)
		}
		if rbacACLString(prefixes, base) == acl {
			return prefixes, nil
		}
	}
	return nil, errorf(adbc.StatusInvalidArgument, "RBAC ACL contains unmanaged rules")
}

func (s *store) rbacControlKey(suffix string) string {
	if s.snapshots {
		return snapshotPrefix + "rbac:" + suffix
	}
	return metaPrefix + "rbac:" + suffix
}
func (s *store) rbacTablePrefix(m *tableMeta) string {
	if s.snapshots {
		return snapshotAccessPrefix(m)
	}
	return m.prefix()
}

func (e *executor) runPrivilege(ctx context.Context, st *PrivilegeStmt) error {
	if e.rbac == nil {
		return errorf(adbc.StatusNotImplemented, "SQL GRANT/REVOKE requires administrator-configured adbc.redis.rbac")
	}
	schema, name, err := e.resolveTable(st.Table)
	if err != nil {
		return err
	}
	m, err := e.store.getTable(ctx, schema, name)
	if err != nil {
		return err
	}
	if m.Foreign || m.Adopted || m.MaterializedSQL != "" || isTempSchema(schema) || m.RekeyTo != "" {
		return errorf(adbc.StatusNotImplemented, "RBAC SELECT grants require a permanent driver-owned base table with no move in progress")
	}
	prefix := e.store.rbacTablePrefix(m)
	if !safeRBACPrefix(prefix) {
		return errorf(adbc.StatusNotImplemented, "table prefix cannot be represented safely in an RBAC ACL")
	}
	// A durable lock serializes API read-modify-write operations across driver
	// processes. No expiry: a stalled API writer must never outlive its lease.
	// On an ambiguous API write failure it is retained for admin reconciliation.
	// Management state is durable across SQL roots: an uncertain API write
	// must keep blocking every future grant writer even if its SQL pin expires.
	controlCtx := context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
	lock := e.store.rbacControlKey("lock")
	token := uuid.NewString()
	ok, err := e.store.client.SetNX(controlCtx, lock, token, 0).Result()
	if err != nil {
		return wrapRedis(err, "cannot acquire RBAC lock")
	}
	if !ok {
		return errorf(adbc.StatusInvalidState, "RBAC update locked; reconcile any interrupted management operation before removing its durable administrative lock")
	}
	keepLock := false
	defer func() {
		if !keepLock {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Only this process removes its lock; administrators must quiesce all
			// grant writers before manually recovering an abandoned lock.
			_ = once(cleanup, e.store.client, "EVAL", `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, 1, lock, token).Err()
		}
	}()
	acl, _, err := e.rbac.readRole(ctx, st.Role)
	if err != nil {
		return err
	}
	ledgerKey := e.store.rbacControlKey("grants:" + strconv.Itoa(e.rbac.cfg.DatabaseUID) + ":" + strconv.Itoa(acl.UID))
	var grants []rbacGrant
	raw, readErr := e.store.client.Get(controlCtx, ledgerKey).Result()
	if readErr != nil && readErr != goredis.Nil {
		return wrapRedis(readErr, "cannot read RBAC grant ledger")
	}
	if raw != "" && json.Unmarshal([]byte(raw), &grants) != nil {
		return errorf(adbc.StatusInvalidState, "corrupt RBAC grant ledger")
	}
	var recorded []string
	for _, grant := range grants {
		recorded = append(recorded, grant.Prefix)
	}
	if rbacACLString(recorded, rbacReaderACL(acl.Domain)) != acl.ACL {
		return errorf(adbc.StatusInvalidState, "RBAC ACL and grant ledger differ; administrator reconciliation required")
	}
	grants = slices.DeleteFunc(grants, func(g rbacGrant) bool { return g.TableOID == tableOID(m) })
	if !st.Revoke {
		grants = append(grants, rbacGrant{tableOID(m), prefix})
	}
	var prefixes []string
	for _, grant := range grants {
		prefixes = append(prefixes, grant.Prefix)
	}
	next := rbacACLString(prefixes, rbacReaderACL(acl.Domain))
	if next == acl.ACL {
		return nil
	}
	keepLock = true
	if err := e.rbac.request(ctx, "PUT", "redis_acls/"+strconv.Itoa(acl.UID), map[string]string{"acl": next}, nil); err != nil {
		return wrapErr(adbc.StatusIO, err, "RBAC lock retained: reconcile the ACL before retrying")
	}
	// Read back before reporting success. Management changes may still take a
	// short time to propagate to every proxy/shard; existing streams may finish.
	var observed rbacACL
	if err := e.rbac.request(ctx, "GET", "redis_acls/"+strconv.Itoa(acl.UID), nil, &observed); err != nil {
		return wrapErr(adbc.StatusIO, err, "RBAC write verification failed; lock retained")
	}
	if observed.UID != acl.UID || observed.Name != acl.Name || observed.ACL != next {
		return errorf(adbc.StatusIO, "RBAC ACL changed during update; lock retained")
	}
	ledger, _ := json.Marshal(grants)
	if err := once(controlCtx, e.store.client, "SET", ledgerKey, ledger).Err(); err != nil {
		return wrapErr(adbc.StatusIO, err, "RBAC ledger write failed after ACL update; lock retained")
	}
	keepLock = false
	return nil
}

// The ledger makes REVOKE remove stale physical prefixes of the same table.
// It is committed after API verification; ambiguous failures keep the lock.
type rbacGrant struct {
	TableOID int64  `json:"table_oid"`
	Prefix   string `json:"prefix"`
}

func (e *executor) rbacGrantTable(ctx context.Context, name string, cols []resultColumn) (*tableMeta, error) {
	var rows [][]Value
	if e.rbac == nil {
		return memTable(name, cols, nil, nil)
	}
	schemas, err := e.store.listSchemas(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0, len(e.rbac.cfg.Roles))
	for role := range e.rbac.cfg.Roles {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	for _, role := range roles {
		_, prefixes, err := e.rbac.readRole(ctx, role)
		if err != nil {
			return nil, err
		}
		for _, schema := range schemas {
			names, err := e.store.listTables(ctx, schema)
			if err != nil {
				return nil, err
			}
			metas, err := e.store.getTables(ctx, schema, names)
			if err != nil {
				return nil, err
			}
			for _, m := range metas {
				if m != nil && slices.Contains(prefixes, e.store.rbacTablePrefix(m)) {
					rows = append(rows, []Value{str("redis_rbac"), str(role), str(catalogName), str(schema), str(m.Name), str("SELECT"), str("NO"), str("NO")})
				}
			}
		}
	}
	return memTable(name, cols, rows, nil)
}
