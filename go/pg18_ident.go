// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

// Keyword categories from PostgreSQL REL_18_6 src/include/parser/kwlist.h:
// https://github.com/postgres/postgres/blob/REL_18_6/src/include/parser/kwlist.h
// SHA-256: fdcdf3694513cba63b4016f63032472b686e381bb35f17c5d645bc2f6f1dac16
// PostgreSQL permits unreserved keywords without quotes; other categories
// require quotes in quote_ident, even if the lexer accepts them as identifiers.
var pgQuotedKeywords = func() map[string]bool {
	m := map[string]bool{}
	for _, word := range strings.Fields(`all analyse analyze and any array as asc asymmetric authorization between bigint binary bit boolean both case cast char character check coalesce collate collation column concurrently constraint create cross current_catalog current_date current_role current_schema current_time current_timestamp current_user dec decimal default deferrable desc distinct do else end except exists extract false fetch float for foreign freeze from full grant greatest group grouping having ilike in initially inner inout int integer intersect interval into is isnull join json json_array json_arrayagg json_exists json_object json_objectagg json_query json_scalar json_serialize json_table json_value lateral leading least left like limit localtime localtimestamp merge_action national natural nchar none normalize not notnull null nullif numeric offset on only or order out outer overlaps overlay placing position precision primary real references returning right row select session_user setof similar smallint some substring symmetric system_user table tablesample then time timestamp to trailing treat trim true union unique user using values varchar variadic verbose when where window with xmlattributes xmlconcat xmlelement xmlexists xmlforest xmlnamespaces xmlparse xmlpi xmlroot xmlserialize xmltable`) {
		m[word] = true
	}
	return m
}()

func pgQuoteIdent(s string) string {
	safe := len(s) > 0 && (s[0] >= 'a' && s[0] <= 'z' || s[0] == '_')
	for i := 1; safe && i < len(s); i++ {
		c := s[i]
		safe = c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
	}
	if safe && !pgQuotedKeywords[s] {
		return s
	}
	return quoteSQLName(s)
}
