// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// UUID uses canonical text in HASHes and Arrow utf8 with SQL type metadata.
// Its fixed punctuation makes lexical ordering equal to byte ordering.
var typeUUID = ColType{Kind: KindString, Alias: "uuid"}

func parseUUID(input string) (Value, error) {
	s := input
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	var digits strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			// PostgreSQL permits a hyphen after any group of four digits.
			if digits.Len() == 0 || digits.Len()%4 != 0 || i == len(s)-1 || s[i-1] == '-' {
				return Value{}, uuidSyntax(input)
			}
			continue
		}
		digits.WriteByte(s[i])
	}
	var b [16]byte
	if digits.Len() != 32 {
		return Value{}, uuidSyntax(input)
	}
	if _, err := hex.Decode(b[:], []byte(digits.String())); err != nil {
		return Value{}, uuidSyntax(input)
	}
	return uuidValue(b), nil
}

func uuidSyntax(s string) error {
	return stateErr(stateInvalidText, "invalid input syntax for type uuid: %q", s)
}

func uuidValue(b [16]byte) Value {
	s := hex.EncodeToString(b[:])
	return Value{T: typeUUID, S: s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]}
}

func randomUUID() (Value, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Value{}, err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return uuidValue(b), nil
}
