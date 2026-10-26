// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"slices"
	"strconv"
	"strings"
)

const pgCatalog = "pg_catalog"

func isPgCatalog(schema string) bool { return schema == pgCatalog }

// Catalog column names/types follow PostgreSQL. Unsupported object kinds
// (domains, enums, sequences, inheritance) have empty, typed relations.
func catalogColumns(spec string) []resultColumn {
	var out []resultColumn
	for _, s := range strings.Fields(spec) {
		name, k, _ := strings.Cut(s, ":")
		t := typeString
		switch k {
		case "i":
			t = typeInt64
		case "b":
			t = typeBool
		case "f":
			t = typeFloat64
		case "a":
			t = arrayOf(typeInt32)
		case "s":
			t = arrayOf(typeString)
		}
		out = append(out, resultColumn{Name: name, Type: t})
	}
	return out
}

var pgCatalogColumns = map[string][]resultColumn{
	"pg_namespace":        catalogColumns("oid:i nspname nspowner:i nspacl:s"),
	"pg_class":            catalogColumns("oid:i relname relnamespace:i reltype:i reloftype:i relowner:i relam:i relfilenode:i reltablespace:i relpages:i reltuples:f relallvisible:i reltoastrelid:i relhasindex:b relisshared:b relpersistence relkind relnatts:i relchecks:i relhasrules:b relhastriggers:b relhassubclass:b relrowsecurity:b relforcerowsecurity:b relispopulated:b relreplident relispartition:b relrewrite:i reloptions:s relacl:s"),
	"pg_attribute":        catalogColumns("attrelid:i attname atttypid:i attstattarget:i attlen:i attnum:i attndims:i attcacheoff:i atttypmod:i attbyval:b attstorage attalign attnotnull:b atthasdef:b atthasmissing:b attidentity attgenerated attisdropped:b attislocal:b attinhcount:i attcollation:i"),
	"pg_attrdef":          catalogColumns("oid:i adrelid:i adnum:i adbin"),
	"pg_type":             catalogColumns("oid:i typname typnamespace:i typowner:i typlen:i typbyval:b typtype typcategory typispreferred:b typisdefined:b typdelim typrelid:i typelem:i typarray:i typinput:i typoutput:i typreceive:i typsend:i typmodin:i typmodout:i typanalyze:i typalign typstorage typnotnull:b typbasetype:i typtypmod:i typndims:i typcollation:i typdefault"),
	"pg_constraint":       catalogColumns("oid:i conname connamespace:i contype condeferrable:b condeferred:b convalidated:b conrelid:i contypid:i conindid:i conparentid:i confrelid:i confupdtype confdeltype confmatchtype conislocal:b coninhcount:i connoinherit:b conkey:a confkey:a conbin"),
	"pg_index":            catalogColumns("indexrelid:i indrelid:i indnatts:i indnkeyatts:i indisunique:b indnullsnotdistinct:b indisprimary:b indisexclusion:b indimmediate:b indisclustered:b indisvalid:b indcheckxmin:b indisready:b indislive:b indisreplident:b indkey:a indcollation:a indclass:a indoption:a indexprs indpred"),
	"pg_description":      catalogColumns("objoid:i classoid:i objsubid:i description"),
	"pg_tables":           catalogColumns("schemaname tablename tableowner tablespace hasindexes:b hasrules:b hastriggers:b rowsecurity:b"),
	"pg_views":            catalogColumns("schemaname viewname viewowner definition"),
	"pg_matviews":         catalogColumns("schemaname matviewname matviewowner tablespace hasindexes:b ispopulated:b definition"),
	"pg_indexes":          catalogColumns("schemaname tablename indexname tablespace indexdef"),
	"pg_settings":         catalogColumns("name setting unit category short_desc extra_desc context vartype source min_val max_val enumvals:s boot_val reset_val sourcefile sourceline:i pending_restart:b"),
	"pg_database":         catalogColumns("oid:i datname datdba:i encoding:i datlocprovider datistemplate:b datallowconn:b datconnlimit:i datcollate datctype dattablespace:i datacl:s"),
	"pg_roles":            catalogColumns("oid:i rolname rolsuper:b rolinherit:b rolcreaterole:b rolcreatedb:b rolcanlogin:b rolreplication:b rolbypassrls:b rolconnlimit:i rolpassword rolvaliduntil rolconfig:s"),
	"pg_user":             catalogColumns("usename usesysid:i usecreatedb:b usesuper:b userepl:b usebypassrls:b passwd valuntil useconfig:s"),
	"pg_proc":             catalogColumns("oid:i proname pronamespace:i proowner:i prolang:i procost:f prorows:f provariadic:i prokind prosecdef:b proleakproof:b proisstrict:b proretset:b provolatile proparallel pronargs:i pronargdefaults:i prorettype:i proargtypes:a proallargtypes:a proargmodes:s proargnames:s proargdefaults prosrc probin proconfig:s proacl:s"),
	"pg_am":               catalogColumns("oid:i amname amhandler:i amtype"),
	"pg_opclass":          catalogColumns("oid:i opcmethod:i opcname opcnamespace:i opcowner:i opcfamily:i opcintype:i opcdefault:b opckeytype:i"),
	"pg_collation":        catalogColumns("oid:i collname collnamespace:i collowner:i collprovider collisdeterministic:b collencoding:i collcollate collctype colliculocale collversion"),
	"pg_enum":             catalogColumns("oid:i enumtypid:i enumsortorder:f enumlabel"),
	"pg_sequence":         catalogColumns("seqrelid:i seqtypid:i seqstart:i seqincrement:i seqmax:i seqmin:i seqcache:i seqcycle:b"),
	"pg_inherits":         catalogColumns("inhrelid:i inhparent:i inhseqno:i inhdetachpending:b"),
	"pg_depend":           catalogColumns("classid:i objid:i objsubid:i refclassid:i refobjid:i refobjsubid:i deptype"),
	"pg_rewrite":          catalogColumns("oid:i rulename ev_class:i ev_type ev_enabled is_instead:b ev_qual ev_action"),
	"pg_stat_user_tables": catalogColumns("relid:i schemaname relname seq_scan:i seq_tup_read:i idx_scan:i idx_tup_fetch:i n_tup_ins:i n_tup_upd:i n_tup_del:i n_tup_hot_upd:i n_live_tup:i n_dead_tup:i n_mod_since_analyze:i n_ins_since_vacuum:i last_vacuum last_autovacuum last_analyze last_autoanalyze vacuum_count:i autovacuum_count:i analyze_count:i autoanalyze_count:i"),
}

func pgCatalogRelation(name string) bool { _, ok := pgCatalogColumns[name]; return ok }

type catalogSnapshot struct {
	namespaceIDs map[string]int64
	rows         map[string][]map[string]Value
	tables       map[int64]*tableMeta
	views        map[int64]*viewMeta
	definitions  map[int64]string
}

func (c *catalogSnapshot) add(name string, v map[string]Value) {
	c.rows[name] = append(c.rows[name], v)
}

func (c *catalogSnapshot) namespaceOID(name string) int64 {
	if id := c.namespaceIDs[name]; id != 0 {
		return id
	}
	return namespaceOID(name)
}
func oidValue(n int64) Value { return intValue(typeInt64, n) }
func str(s string) Value     { return stringValue(s) }
func intArray(nums []int64) Value {
	v := make([]Value, len(nums))
	for i, n := range nums {
		v[i] = intValue(typeInt32, n)
	}
	return arrayValue(v, typeInt32)
}
func quoteSQLName(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func qualifiedName(schema, name string) string {
	return quoteSQLName(schema) + "." + quoteSQLName(name)
}

func (e *executor) catalog(ctx context.Context) (*catalogSnapshot, error) {
	e.ensureCache()
	if e.cache.catalog != nil {
		return e.cache.catalog, nil
	}
	c := &catalogSnapshot{namespaceIDs: map[string]int64{}, rows: map[string][]map[string]Value{}, tables: map[int64]*tableMeta{}, views: map[int64]*viewMeta{}, definitions: map[int64]string{}}
	schemas := []string{defaultSchema}
	if e.store != nil {
		var err error
		schemas, err = e.store.listSchemas(ctx)
		if err != nil {
			return nil, err
		}
	}
	if e.store != nil && e.store.hasTempObjects() {
		schemas = append(schemas, e.store.tempSchema())
	}
	for _, schema := range append(slices.Clone(schemas), pgCatalog, infoSchema) {
		if e.store != nil {
			n, err := e.store.redis(ctx).Get(ctx, namespaceOIDKey(schema)).Int64()
			if err == nil {
				c.namespaceIDs[schema] = n
			} else if !errors.Is(err, goredis.Nil) {
				return nil, wrapRedis(err, "failed to read namespace OID")
			}
		}
		c.add("pg_namespace", map[string]Value{"oid": oidValue(c.namespaceOID(schema)), "nspname": str(schema), "nspowner": oidValue(10)})
	}
	for _, schema := range schemas {
		if e.store == nil {
			continue
		}
		names, err := e.store.listTables(ctx, schema)
		if err != nil {
			return nil, err
		}
		ms, err := e.store.getTables(ctx, schema, names)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if m == nil {
				continue
			}
			id := tableOID(m)
			c.tables[id] = m
			c.addRelation(m, id, "r")
			c.add("pg_tables", map[string]Value{"schemaname": str(displaySchema(schema)), "tablename": str(m.Name), "tableowner": str(e.catalogUser()), "hasindexes": boolValue(true), "hasrules": boolValue(false), "hastriggers": boolValue(false), "rowsecurity": boolValue(false)})
			var fields []string
			for _, col := range m.Columns {
				if col.Indexed {
					fields = append(fields, col.field())
				}
			}
			if len(fields) > 0 {
				c.addIndex(m, indexOID(m), m.index(), fields, false)
			}
			if m.PrimaryKey != nil {
				pk := m.PrimaryKey
				c.addIndex(m, keyOID(m, true), pk.Name, pk.Fields, true)
				c.definitions[keyOID(m, false)] = "PRIMARY KEY (" + quotedColumns(m, pk.Fields) + ")"
				c.add("pg_constraint", map[string]Value{"oid": oidValue(keyOID(m, false)), "conname": str(pk.Name), "connamespace": oidValue(c.namespaceOID(schema)), "contype": str("p"), "conrelid": oidValue(id), "conindid": oidValue(keyOID(m, true)), "contypid": oidValue(0), "confrelid": oidValue(0), "convalidated": boolValue(true), "conislocal": boolValue(true), "conkey": intArray(columnNumbers(m, pk.Fields))})
			}
			for _, ck := range m.Checks {
				cid := checkOID(m, ck)
				c.definitions[cid] = "CHECK (" + ck.Expr + ")"
				c.add("pg_constraint", map[string]Value{"oid": oidValue(cid), "conname": str(ck.Name), "connamespace": oidValue(c.namespaceOID(schema)), "contype": str("c"), "conrelid": oidValue(id), "contypid": oidValue(0), "conindid": oidValue(0), "confrelid": oidValue(0), "convalidated": boolValue(true), "conislocal": boolValue(true), "conbin": str(ck.Expr)})
			}
		}
		vs, err := e.store.listViews(ctx, schema)
		if err != nil {
			return nil, err
		}
		vms, err := e.store.getViews(ctx, schema, vs)
		if err != nil {
			return nil, err
		}
		for _, v := range vms {
			if v == nil {
				continue
			}
			id := viewOID(v)
			c.views[id] = v
			m := &tableMeta{Schema: v.Schema, Name: v.Name, Columns: v.Columns, Comment: v.Comment}
			c.addRelation(m, id, "v")
			c.definitions[id] = v.SQL
			c.add("pg_views", map[string]Value{"schemaname": str(displaySchema(schema)), "viewname": str(v.Name), "viewowner": str(e.catalogUser()), "definition": str(v.SQL)})
		}
	}
	for name, cols := range pgCatalogColumns {
		m := &tableMeta{Schema: pgCatalog, Name: name}
		for _, col := range cols {
			m.Columns = append(m.Columns, columnMeta{Name: col.Name, Type: col.Type, Nullable: true})
		}
		c.addRelation(m, systemRelationOID(name), "r")
	}
	for _, t := range builtinCatalogTypes() {
		id := postgresTypeOID(t)
		_, name := postgresTypeNames(t)
		coll := int64(0)
		if t.Kind == KindString {
			coll = 100
		}
		c.add("pg_type", map[string]Value{"oid": oidValue(id), "typname": str(name), "typnamespace": oidValue(11), "typowner": oidValue(10), "typlen": oidValue(-1), "typtype": str("b"), "typcategory": str(typeCategory(t)), "typisdefined": boolValue(true), "typdelim": str(","), "typnotnull": boolValue(false), "typbasetype": oidValue(0), "typtypmod": oidValue(-1), "typcollation": oidValue(coll)})
	}
	for _, v := range []struct {
		id   int64
		name string
	}{{100, "default"}, {950, "C"}, {951, "POSIX"}} {
		c.add("pg_collation", map[string]Value{"oid": oidValue(v.id), "collname": str(v.name), "collnamespace": oidValue(11), "collowner": oidValue(10), "collprovider": str("c"), "collisdeterministic": boolValue(true), "collencoding": oidValue(-1), "collcollate": str("C"), "collctype": str("C")})
	}
	c.add("pg_am", map[string]Value{"oid": oidValue(4096), "amname": str("redis_search"), "amtype": str("i")})
	c.add("pg_opclass", map[string]Value{"oid": oidValue(10001), "opcmethod": oidValue(4096), "opcname": str("redis_ops"), "opcnamespace": oidValue(11), "opcdefault": boolValue(true)})
	c.add("pg_database", map[string]Value{"oid": oidValue(5), "datname": str(catalogName), "datdba": oidValue(10), "encoding": oidValue(6), "datlocprovider": str("c"), "datistemplate": boolValue(false), "datallowconn": boolValue(true), "datconnlimit": oidValue(-1), "datcollate": str("C"), "datctype": str("C")})
	c.add("pg_roles", map[string]Value{"oid": oidValue(10), "rolname": str(e.catalogUser()), "rolsuper": boolValue(false), "rolinherit": boolValue(true), "rolcanlogin": boolValue(true), "rolconnlimit": oidValue(-1), "rolpassword": str("********")})
	c.add("pg_user", map[string]Value{"usesysid": oidValue(10), "usename": str(e.catalogUser()), "usesuper": boolValue(false), "usecreatedb": boolValue(false), "userepl": boolValue(false), "usebypassrls": boolValue(false), "passwd": str("********")})
	sess := e.sess
	if sess == nil {
		sess = newSession(defaultSchema)
	}
	for name, def := range settings {
		c.add("pg_settings", map[string]Value{"name": str(name), "setting": str(sess.value(name).text), "short_desc": str(def.desc), "context": str("user"), "vartype": str("string"), "source": str("session"), "boot_val": str(def.initial(sess).text), "reset_val": str(def.initial(sess).text), "pending_restart": boolValue(false)})
	}
	for _, def := range funcDefs {
		if def.internal {
			continue
		}
		c.add("pg_proc", map[string]Value{"oid": oidValue(legacyCatalogOID("proc", def.name)), "proname": str(strings.ToLower(def.name)), "pronamespace": oidValue(11), "proowner": oidValue(10), "prokind": str(map[bool]string{true: "a", false: "f"}[def.kind == aggregateKind]), "pronargs": oidValue(int64(def.min)), "prorettype": oidValue(25), "prosrc": str(def.name), "proretset": boolValue(def.kind == tableKind), "provolatile": str("s")})
	}
	e.cache.catalog = c
	return c, nil
}

func (e *executor) catalogUser() string {
	if e.sess != nil && e.sess.user != "" {
		return e.sess.user
	}
	return "default"
}
func systemRelationOID(name string) int64 {
	if n, ok := map[string]int64{"pg_class": 1259, "pg_attribute": 1249, "pg_type": 1247, "pg_namespace": 2615, "pg_constraint": 2606, "pg_index": 2610, "pg_description": 2609, "pg_proc": 1255, "pg_attrdef": 2604}[name]; ok {
		return n
	}
	return legacyCatalogOID("catalog", name)
}
func builtinCatalogTypes() []ColType {
	return []ColType{typeBool, typeBinary, typeInt16, typeInt32, typeInt64, typeFloat32, typeFloat64, decimalType(38, 0), typeString, {Kind: KindString, Text: true}, {Kind: KindString, Fixed: true}, typeDate, fracType(KindTime, 6, ""), typeTimeTZ, typeTimestamp, typeTimestampTZ, typeInterval, {Kind: KindInt64, Alias: "oid"}, {Kind: KindString, Alias: "regclass"}, {Kind: KindString, Alias: "regtype"}, {Kind: KindString, Alias: "regproc"}, {Kind: KindString, Alias: "regnamespace"}}
}
func typeCategory(t ColType) string {
	if t.Kind.isNumeric() {
		return "N"
	}
	if t.Kind == KindString {
		return "S"
	}
	if textEncodedKind(t.Kind) {
		if t.Kind == KindBool {
			return "B"
		}
		return "D"
	}
	return "U"
}
func columnNumbers(m *tableMeta, fields []string) []int64 {
	var out []int64
	for _, f := range fields {
		for i, c := range m.Columns {
			if c.field() == f {
				out = append(out, int64(i+1))
				break
			}
		}
	}
	return out
}
func quotedColumns(m *tableMeta, fields []string) string {
	var cols []string
	for _, f := range fields {
		for _, c := range m.Columns {
			if c.field() == f {
				cols = append(cols, quoteSQLName(c.Name))
				break
			}
		}
	}
	return strings.Join(cols, ", ")
}

func (c *catalogSnapshot) addRelation(m *tableMeta, id int64, kind string) {
	persistence := "p"
	if isTempSchema(m.Schema) {
		persistence = "t"
	}
	c.add("pg_class", map[string]Value{"oid": oidValue(id), "relname": str(m.Name), "relnamespace": oidValue(c.namespaceOID(m.Schema)), "relowner": oidValue(10), "relkind": str(kind), "relpersistence": str(persistence), "relnatts": oidValue(int64(len(m.Columns))), "relchecks": oidValue(int64(len(m.Checks))), "relhasindex": boolValue(kind == "r"), "relispartition": boolValue(false), "relispopulated": boolValue(true), "relam": oidValue(0), "reltype": oidValue(0)})
	if m.Comment != "" {
		c.add("pg_description", map[string]Value{"objoid": oidValue(id), "classoid": oidValue(1259), "objsubid": oidValue(0), "description": str(m.Comment)})
	}
	for i, col := range m.Columns {
		coll := int64(0)
		if col.Type.Kind == KindString {
			coll = 100
		}
		if col.Type.Collation == "C" {
			coll = 950
		}
		if col.Type.Collation == "POSIX" {
			coll = 951
		}
		c.add("pg_attribute", map[string]Value{"attrelid": oidValue(id), "attname": str(col.Name), "atttypid": oidValue(postgresTypeOID(col.Type)), "attnum": oidValue(int64(i + 1)), "atttypmod": oidValue(postgresTypmod(col.Type)), "attnotnull": boolValue(!col.Nullable), "atthasdef": boolValue(col.Default != ""), "attidentity": str(""), "attgenerated": str(""), "attisdropped": boolValue(false), "attislocal": boolValue(true), "attcollation": oidValue(coll), "attndims": oidValue(0), "attlen": oidValue(-1)})
		if col.Default != "" {
			c.add("pg_attrdef", map[string]Value{"oid": oidValue(defaultOID(m, col)), "adrelid": oidValue(id), "adnum": oidValue(int64(i + 1)), "adbin": str(col.Default)})
		}
		if col.Comment != "" {
			c.add("pg_description", map[string]Value{"objoid": oidValue(id), "classoid": oidValue(1259), "objsubid": oidValue(int64(i + 1)), "description": str(col.Comment)})
		}
	}
}
func (c *catalogSnapshot) addIndex(m *tableMeta, id int64, name string, fields []string, primary bool) {
	nums := columnNumbers(m, fields)
	zeros, classes := make([]int64, len(nums)), make([]int64, len(nums))
	for i := range classes {
		classes[i] = 10001
	}
	def := "CREATE INDEX " + quoteSQLName(name) + " ON " + qualifiedName(m.Schema, m.Name) + " USING redis_search (" + quotedColumns(m, fields) + ")"
	c.definitions[id] = def
	c.add("pg_class", map[string]Value{"oid": oidValue(id), "relname": str(name), "relnamespace": oidValue(c.namespaceOID(m.Schema)), "relowner": oidValue(10), "relkind": str("i"), "relpersistence": str("p"), "relam": oidValue(4096), "relnatts": oidValue(int64(len(nums))), "relispartition": boolValue(false)})
	c.add("pg_indexes", map[string]Value{"schemaname": str(displaySchema(m.Schema)), "tablename": str(m.Name), "indexname": str(name), "indexdef": str(def)})
	c.add("pg_index", map[string]Value{"indexrelid": oidValue(id), "indrelid": oidValue(tableOID(m)), "indnatts": oidValue(int64(len(nums))), "indnkeyatts": oidValue(int64(len(nums))), "indisunique": boolValue(primary), "indisprimary": boolValue(primary), "indnullsnotdistinct": boolValue(false), "indisvalid": boolValue(true), "indisready": boolValue(true), "indislive": boolValue(true), "indimmediate": boolValue(true), "indkey": intArray(nums), "indclass": intArray(classes), "indoption": intArray(zeros), "indcollation": intArray(zeros)})
}

func (e *executor) pgCatalogTable(ctx context.Context, name string) (*tableMeta, error) {
	cols, ok := pgCatalogColumns[name]
	if !ok {
		return nil, tableNotFound(pgCatalog, name)
	}
	if e.planOnly {
		return memTable(name, cols, nil, nil)
	}
	c, err := e.catalog(ctx)
	if err != nil {
		return nil, err
	}
	if name == "pg_stat_user_tables" && c.rows[name] == nil {
		for id, m := range c.tables {
			n, err := e.store.aggregate(ctx, &aggRequest{index: m.index(), query: "*", rowFilter: m.rowFilter(), groupBy: []any{"GROUPBY", 0, "REDUCE", "COUNT", 0, "AS", "n"}})
			if err != nil {
				return nil, err
			}
			count := int64(0)
			if len(n) > 0 {
				count, _ = strconv.ParseInt(n[0]["n"], 10, 64)
			}
			c.add(name, map[string]Value{"relid": oidValue(id), "schemaname": str(displaySchema(m.Schema)), "relname": str(m.Name), "n_live_tup": oidValue(count)})
		}
	}
	var rows [][]Value
	for _, values := range c.rows[name] {
		row := make([]Value, len(cols))
		for i, col := range cols {
			v, ok := values[col.Name]
			if !ok {
				v = nullValue(col.Type)
				if col.Type.Kind == KindBool {
					v = boolValue(false)
				}
			}
			row[i] = v
		}
		rows = append(rows, row)
	}
	return memTable(name, cols, rows, nil)
}
