// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestUnicodeNormalizationBoundaries(t *testing.T) {
	if norm.Version != "17.0.0" {
		t.Fatalf("requalify PostgreSQL Unicode 16 exclusions for x/text Unicode %s", norm.Version)
	}
	for _, tc := range []struct{ in, nfc, nfd, nfkc, nfkd string }{
		{"A\u030a", "Å", "A\u030a", "Å", "A\u030a"},
		{"\ufb03", "ﬃ", "ﬃ", "ffi", "ffi"},
		{"\uac01", "각", "각", "각", "각"},
		{"\u0344", "\u0308\u0301", "\u0308\u0301", "\u0308\u0301", "\u0308\u0301"},
		{"a\u034f\u0300", "a\u034f\u0300", "a\u034f\u0300", "a\u034f\u0300", "a\u034f\u0300"},
		// Unicode 16 Tulu-Tigalari composition (not present in Unicode 15).
		{"\U00011382\U000113c9", "\U00011383", "\U00011382\U000113c9", "\U00011383", "\U00011382\U000113c9"},
	} {
		for i, form := range []norm.Form{norm.NFC, norm.NFD, norm.NFKC, norm.NFKD} {
			want := []string{tc.nfc, tc.nfd, tc.nfkc, tc.nfkd}[i]
			if got := pgNormalize(tc.in, form); got != want {
				t.Errorf("%U form %d: %U, want %U", []rune(tc.in), i, []rune(got), []rune(want))
			}
		}
	}
	for r := rune(0); r <= 0x10ffff; r++ {
		if !unicode17Normalization(r) {
			continue
		}
		in := "a" + string(r) + "\u0300"
		for _, form := range []norm.Form{norm.NFC, norm.NFD, norm.NFKC, norm.NFKD} {
			if got := pgNormalize(in, form); got != in {
				t.Errorf("Unicode 16 inert %U: %U", r, []rune(got))
			}
		}
	}
}

// Optional full UAX #15 conformance input, independently published by Unicode.
// The file is not downloaded by tests; validation.md pins URL and SHA-256.
func TestUnicodeNormalizationConformance(t *testing.T) {
	path := os.Getenv("UNICODE_NORMALIZATION_TEST_FILE")
	if path == "" {
		t.Skip("set UNICODE_NORMALIZATION_TEST_FILE to Unicode 16 NormalizationTest.txt")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decode := func(s string) string {
		var out []rune
		for _, hex := range strings.Fields(s) {
			r, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, rune(r))
		}
		return string(out)
	}
	count := 0
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(strings.SplitN(scan.Text(), "#", 2)[0])
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 5 {
			t.Fatalf("bad conformance line %d", count)
		}
		cols := make([]string, 5)
		for i := range cols {
			cols[i] = decode(fields[i])
		}
		for i, in := range cols {
			nfc, nfd := cols[1], cols[2]
			if i >= 3 {
				nfc, nfd = cols[3], cols[4]
			}
			for j, form := range []norm.Form{norm.NFC, norm.NFD, norm.NFKC, norm.NFKD} {
				want := []string{nfc, nfd, cols[3], cols[4]}[j]
				if got := pgNormalize(in, form); got != want {
					t.Fatalf("vector %d column %d form %d: %U, want %U", count, i, j, []rune(got), []rune(want))
				}
			}
		}
		count++
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 19965 {
		t.Fatalf("expected pinned Unicode 16 vectors, got %d", count)
	}
	t.Logf("%d Unicode 16 vectors, %d exact normalization assertions", count, count*20)
}
