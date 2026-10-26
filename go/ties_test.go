// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func TestTieBound(t *testing.T) {
	for _, c := range []struct {
		t    ColType
		want float64
	}{
		{typeInt64, 1 << 53},
		{timestampType(arrow.Nanosecond, ""), 1 << 53},
		{timestampType(arrow.Microsecond, "UTC"), 1 << 53},
		{timeType(arrow.Nanosecond), 1 << 53},
		{decimalType(38, 9), 1e6},
		{decimalType(16, 2), 1e13},
		{decimalType(20, 17), 0.01},
		{decimalType(15, 2), 0},
		{typeInt32, 0},
		{typeInt16, 0},
		{typeFloat64, 0},
		{typeDate, 0},
		{typeString, 0},
	} {
		if got := tieBound(c.t); got != c.want {
			t.Errorf("tieBound(%s) = %g, want %g", c.t.SQLName(), got, c.want)
		}
	}
	// Below the bound, values are distinct doubles; from it on, they may
	// not be.
	if float64(int64(1<<53)) == float64(int64(1<<53-1)) || float64(int64(1<<53)) != float64(int64(1<<53+1)) {
		t.Error("2^53 is not where integers start to round")
	}
	for _, p := range [][2]string{{"999999.999999999", "999999.999999998"}, {"0.00999999999999999", "0.00999999999999998"}} {
		a, _ := strconv.ParseFloat(p[0], 64)
		b, _ := strconv.ParseFloat(p[1], 64)
		if a == b {
			t.Errorf("%s and %s are the same double", p[0], p[1])
		}
	}
}

func TestTieRuns(t *testing.T) {
	// ORDER BY g (a string, which doesn't round), b (BIGINT), c (BIGINT).
	cols := []columnMeta{
		{Name: "g", Type: typeString, Indexed: true},
		{Name: "b", Type: typeInt64, Indexed: true},
		{Name: "c", Type: typeInt64, Indexed: true},
	}
	ts := newTieSort(cols, make([]planOrder, 3))
	if ts == nil || ts.first != 1 {
		t.Fatalf("newTieSort: %+v, want the first key that rounds to be 1", ts)
	}
	if newTieSort(cols[:1], make([]planOrder, 1)) != nil {
		t.Error("newTieSort of a string key: want nil")
	}
	row := func(kv ...string) aggRow {
		r := aggRow{}
		for i := 0; i+1 < len(kv); i += 2 {
			r[kv[i]] = kv[i+1]
		}
		return r
	}
	// Run keys: the keys up to b, NULLs apart from every value.
	same := [][2]aggRow{
		{row("__sort0", "x", "__sort1", "9007199254740992", "__sort2", "1"), row("__sort0", "x", "__sort1", "9007199254740992", "__sort2", "2")},
		{row("__sort0", "x"), row("__sort0", "x", "__sort2", "5")},
	}
	for _, p := range same {
		if ts.runKey(p[0]) != ts.runKey(p[1]) {
			t.Errorf("%v and %v: different runs", p[0], p[1])
		}
	}
	apart := [][2]aggRow{
		{row("__sort0", "x", "__sort1", "1"), row("__sort0", "y", "__sort1", "1")},
		{row("__sort0", "x", "__sort1", ""), row("__sort0", "x")},
		{row("__sort0", "", "__sort1", "1"), row("__sort1", "1")},
		{row("__sort0", "1:a", "__sort1", "1"), row("__sort0", "1", "__sort1", "a1")},
	}
	for _, p := range apart {
		if ts.runKey(p[0]) == ts.runKey(p[1]) {
			t.Errorf("%v and %v: the same run", p[0], p[1])
		}
	}
	// Wide: a key that rounds, from 2^53 on, also as the index prints it
	// (12 significant digits for a fraction), or that doesn't parse.
	for _, c := range []struct {
		r    aggRow
		want bool
	}{
		{row("__sort1", "9007199254740992"), true},
		{row("__sort1", "-9007199254740992"), true},
		{row("__sort1", "9.00719925474e+15"), true},
		{row("__sort1", "9007199254740"), false},
		{row("__sort0", "9007199254740992"), false}, // g doesn't round
		{row("__sort2", "1.84467440737e+19"), true},
		{row("__sort1", "1", "__sort2", "2"), false},
		{row("__sort1", "x"), true},
		{row(), false},
	} {
		if got := ts.wide(c.r); got != c.want {
			t.Errorf("wide(%v) = %v, want %v", c.r, got, c.want)
		}
	}
	n := newTieSort([]columnMeta{{Name: "n", Type: decimalType(38, 9), Indexed: true}}, make([]planOrder, 1))
	for v, want := range map[string]bool{"999999.999999": true, "999999.99": false, "1000000": true, "-1234567.12346": true, "0.5": false} {
		if got := n.wide(row("__sort0", v)); got != want {
			t.Errorf("NUMERIC wide(%s) = %v, want %v", v, got, want)
		}
	}
}

func TestTieWindow(t *testing.T) {
	cols := []columnMeta{{Name: "b", Type: typeInt64, Indexed: true}}
	ts := newTieSort(cols, make([]planOrder, 1))
	big := strconv.FormatFloat(math.Exp2(53), 'f', -1, 64)
	// rows parses "b5 b b5 7": wide (b) and other values, x for a row
	// deleted after the index matched it.
	rows := func(s string) []tieRow {
		var out []tieRow
		for _, f := range strings.Fields(s) {
			switch f {
			case "x":
				out = append(out, tieRow{})
			case "b":
				out = append(out, tieRow{raw: aggRow{"__sort0": big}})
			default:
				out = append(out, tieRow{raw: aggRow{"__sort0": f}})
			}
		}
		return out
	}
	for _, c := range []struct {
		rows   string
		lo, hi int
		first  bool
		want   bool
	}{
		{"b b 5", 1, 3, true, true},
		{"5 b b", 0, 2, false, true},
		{"5 b b", 1, 2, true, false},
		{"5 5 b", 1, 2, true, false}, // a run without a wide row
		{"b 7 b", 1, 3, true, false},
		{"x b 5", 1, 2, true, true}, // a deleted row may be in any run
		{"b x b 5", 2, 3, true, true},
		{"b x 5", 1, 3, true, false},
		{"5 b x x", 0, 2, false, true},
		{"5 b b 6", 0, 2, false, false},
		{"5 b x 5", 1, 2, false, false},
		{"x x 5", 1, 2, true, false}, // nothing wide on that side
	} {
		if got := ts.reachesEdge(rows(c.rows), c.lo, c.hi, c.first); got != c.want {
			t.Errorf("reachesEdge(%q, %d, %d, first %v) = %v, want %v", c.rows, c.lo, c.hi, c.first, got, c.want)
		}
	}
	// By value: only keys the index printed as the exact integer of a
	// double below 2^63.
	for key, want := range map[string]bool{
		"5": true, "-5": true, "9007199254740992": true, "1234567890123456768": true,
		"9223372036854774784":  true,  // 2^63 - 1024
		"9223372036854775807":  false, // 2^63, as a saturated long long
		"-9223372036854775808": false, // -2^63, or 2^63 overflowed
		"9223372036854775808":  false,
		"1234567890123456789":  false, // not a double
		"9.22337203685e+18":    false,
		"":                     false,
		"\x00":                 false,
	} {
		if got := ts.byValue(key); got != want {
			t.Errorf("byValue(%q) = %v, want %v", key, got, want)
		}
	}
	dec := newTieSort([]columnMeta{{Name: "n", Type: decimalType(38, 9), Indexed: true}}, make([]planOrder, 1))
	if dec.byValue("5") {
		t.Error("byValue of a NUMERIC key: want false")
	}

	// The LIMIT the index applies first: a row more each side.
	for _, c := range []struct{ in, want [2]int64 }{
		{[2]int64{0, 10}, [2]int64{0, 11}},
		{[2]int64{5, 10}, [2]int64{4, 12}},
		{[2]int64{1, 1}, [2]int64{0, 3}},
	} {
		var s tieScan
		if got := *s.windowLimit(c.in); got != c.want {
			t.Errorf("windowLimit(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
