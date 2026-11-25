// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// parseDecimalText is parseDecimal's original big.Int text path, the
// reference for parseSmallDecimal.
func parseDecimalText(t *testing.T, s string) (string, int32, bool) {
	t.Helper()
	s = strings.TrimSpace(s)
	exp := int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil {
			return "", 0, false
		}
		exp, s = e, s[:i]
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	intPart, frac, _ := strings.Cut(s, ".")
	digits := intPart + frac
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return "", 0, false
	}
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return "", 0, false
	}
	if neg {
		v.Neg(v)
	}
	return v.String(), int32(int64(len(frac)) - exp), true
}

// #652: stored decimals of up to 18 digits parse without big.Int text
// scanning, to the same unscaled value and scale (and the same errors).
func TestParseSmallDecimalMatchesTextPath(t *testing.T) {
	cases := []string{"0", "-0", "+0", "0.00", "-0.00", "1", "-1", ".5", "-.5", "5.", "+5.", "007.0700",
		"123456789012345678", "-123456789012345678", "1234567890123456789", "99999999999999999.9",
		"9.99999999999999999", "-9223372036854775808", "1.5e3", "-2.25E-2", "12e0", " 42.10 ",
		"", "-", "+", ".", "1.2.3", "1a", "--1", "1e", "1e1.5", "١٢", "1 2", "0x10", "1_000"}
	r := rand.New(rand.NewSource(652))
	for range 2000 {
		n := 1 + r.Intn(20)
		var b strings.Builder
		if r.Intn(3) == 0 {
			b.WriteByte('-')
		}
		dot := r.Intn(n + 1)
		for i := range n {
			if i == dot && r.Intn(2) == 0 {
				b.WriteByte('.')
			}
			b.WriteByte(byte('0' + r.Intn(10)))
		}
		cases = append(cases, b.String())
	}
	for _, c := range cases {
		want, wantScale, wantOK := parseDecimalText(t, c)
		got, scale, err := parseDecimal(c)
		if (err == nil) != wantOK {
			t.Fatalf("parseDecimal(%q): err %v, want ok %v", c, err, wantOK)
		}
		if err == nil && (got.String() != want || scale != wantScale) {
			t.Fatalf("parseDecimal(%q) = %s scale %d, want %s scale %d", c, got, scale, want, wantScale)
		}
	}
}

// Decoding a stored decimal at its column's scale allocates only its value,
// and decimal + / - copy no operand (#652). Results and operands are intact.
func TestDecimalRowAllocations(t *testing.T) {
	typ := decimalType(15, 2)
	v, err := decodeStored("12345.67", typ)
	if err != nil || v.D.String() != "1234567" || v.T.Scale != 2 {
		t.Fatal(v, err)
	}
	if n := testing.AllocsPerRun(200, func() { _, _ = decodeStored("12345.67", typ) }); n > 2 {
		t.Errorf("decodeStored DECIMAL(15,2): %.0f allocations, want at most 2 (big.Int and its digits)", n)
	}
	// A rescaled stored value (fewer stored fractional digits) is still exact.
	if w, err := decodeStored("1.5", typ); err != nil || w.D.String() != "150" {
		t.Fatal(w, err)
	}
	a, _ := decodeStored("10.25", typ)
	b, _ := decodeStored("0.75", typ)
	for _, c := range []struct{ op, want string }{{"+", "1100"}, {"-", "950"}} {
		got, err := binaryOp(c.op, a, b, tzZone{})
		if err != nil || got.D.String() != c.want || got.T.Scale != 2 {
			t.Fatalf("10.25 %s 0.75 = %v, %v", c.op, got, err)
		}
	}
	// The same operand twice, and operands at different scales.
	if got, _ := binaryOp("-", a, a, tzZone{}); got.D.Sign() != 0 {
		t.Fatal("a - a", got)
	}
	one := intValue(typeInt64, 1)
	if got, _ := binaryOp("-", one, b, tzZone{}); got.D.String() != "25" || got.T.Scale != 2 {
		t.Fatal("1 - 0.75", got)
	}
	if a.D.String() != "1025" || b.D.String() != "75" {
		t.Fatalf("operands changed: %s %s", a.D, b.D)
	}
	if n := testing.AllocsPerRun(200, func() { _, _ = binaryOp("+", a, b, tzZone{}) }); n > 2 {
		t.Errorf("decimal +: %.0f allocations, want at most 2 (the sum)", n)
	}
}

// A table's newest segment shares one key string for both keys (#652).
func TestSnapshotKeyPairShares(t *testing.T) {
	l, p := snapshotKeyPair("adbc:t:", "adbc:t:", "42")
	if l != "adbc:t:42" || p != l || unsafe.StringData(l) != unsafe.StringData(p) {
		t.Fatalf("same prefix: %q %q not one string", l, p)
	}
	l, p = snapshotKeyPair("adbc:t:", "adbc:t:seg1:", "42")
	if l != "adbc:t:42" || p != "adbc:t:seg1:42" {
		t.Fatalf("segment prefix: %q %q", l, p)
	}
}

// Retained dimension rows keep only output columns. A fresh row of at most
// smallRowEntries entries is compacted in place; any other row is copied and
// left unchanged, since something else may still read it (#652).
func TestCompactDimensionRowOwnership(t *testing.T) {
	shape := &starShape{outNeed: map[string]bool{"d.a": true, "d.b": true}}
	row := func(n int) map[string]Value {
		m := map[string]Value{"d.a": intValue(typeInt64, 1), "d.b": stringValue("x")}
		for i := 2; i < n; i++ {
			m["d.k"+strconv.Itoa(i)] = intValue(typeInt64, int64(i))
		}
		return m
	}
	check := func(what string, out map[string]Value) {
		t.Helper()
		if len(out) != 2 || out["d.a"].I != 1 || out["d.b"].S != "x" {
			t.Fatalf("%s: compacted to %v", what, out)
		}
	}
	small := row(5)
	out := shape.compactDimensionRow(small, true)
	check("fresh small", out)
	if len(small) != 2 {
		t.Fatal("a fresh small row was copied instead of compacted in place")
	}
	for _, c := range []struct {
		what  string
		n     int
		fresh bool
	}{{"shared small", 5, false}, {"fresh large", smallRowEntries + 1, true}, {"shared large", 12, false}} {
		in := row(c.n)
		check(c.what, shape.compactDimensionRow(in, c.fresh))
		if len(in) != c.n {
			t.Fatalf("%s: input changed to %d entries", c.what, len(in))
		}
	}
	all := map[string]Value{"d.a": intValue(typeInt64, 1)}
	if got := shape.compactDimensionRow(all, false); len(got) != 1 {
		t.Fatal("a row of only output columns changed", got)
	}
	for _, base := range []*tableMeta{{isMem: true}, {KeyPrefix: "adbc:t:", Schema: "public"}} {
		if !freshDimensionRows(base) {
			t.Errorf("%+v should read fresh rows", base)
		}
	}
	for _, base := range []*tableMeta{nil, {isMem: true, view: &lazyView{}}, {KeyPrefix: "adbc:t:", Schema: "public", MaterializedSQL: "x"}, {KeyPrefix: "adbc:t:", Schema: "public", Adopted: true}} {
		if freshDimensionRows(base) {
			t.Errorf("%+v may share rows", base)
		}
	}
}
