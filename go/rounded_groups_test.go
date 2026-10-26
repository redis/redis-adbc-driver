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
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func TestIndexValueExact(t *testing.T) {
	micros := timestampType(arrow.Microsecond, "")
	cases := []struct {
		t    ColType
		s    string
		want bool
	}{
		{typeInt64, "5", true},
		{typeInt64, "9007199254740991", true},
		{typeInt64, "-9007199254740991", true},
		{typeInt64, "9007199254740992", false},
		{typeInt64, "-9007199254740992", false},
		{typeInt64, "9223372036854775807", false},
		// 2^63 on amd64, which ParseFloat reads as 9223372036850000000.
		{typeInt64, "9.22337203685e+18", false},
		{micros, "1717200000000000", true},
		// 9999-12-31 23:59:59.999999, as the double 10000-01-01.
		{micros, "253402300800000000", false},
		{timeType(arrow.Nanosecond), "86399999999999", true},
		{decimalType(38, 9), "999.999999999", true},
		{decimalType(38, 9), "-999.999999999", true},
		{decimalType(38, 9), "1e-07", true},
		{decimalType(38, 9), "1000", false},
		{decimalType(38, 9), "1000000", false},
		{decimalType(20, 17), "9.99999999999e-06", true},
		{decimalType(20, 17), "1e-05", false},
		{decimalType(20, 17), "0.5", false},
		{decimalType(14, 2), "9999999999.99", true},
		{decimalType(14, 2), "123456789012", false},
		{decimalType(12, 2), "9999999999.99", true},
		{decimalType(12, 0), "999999999999", true},
		{typeFloat32, "0.1", true},
		{typeFloat32, "-1e+18", true},
		{typeFloat32, "inf", true},
		{typeFloat32, "-inf", true},
		// 2^63 as arm64 prints it, and amd64's long long overflow.
		{typeFloat32, "9223372036854775807", false},
		{typeFloat32, "-9223372036854775808", false},
		{typeFloat32, "3.40282346639e+38", false},
		{typeFloat64, "5", false},
		{typeFloat64, "0.3", false},
		{typeInt32, "2147483647", true},
		{typeDate, "-719162", true},
		{typeBool, "1", true},
	}
	for _, c := range cases {
		if got := indexValueExact(c.t, c.s); got != c.want {
			t.Errorf("%v %q: %v, want %v", c.t, c.s, got, c.want)
		}
	}
}

func TestGroupable(t *testing.T) {
	cases := []struct {
		t          ColType
		exact, all bool
	}{
		{typeString, true, true},
		{typeInt32, true, true},
		{typeDate, true, true},
		{typeInt64, false, true},
		{decimalType(38, 9), false, true},
		{typeFloat32, false, true},
		{timestampType(arrow.Microsecond, "UTC"), false, true},
		{timeType(arrow.Nanosecond), false, true},
		// Its values are past 2^53 from April 1970 on.
		{timestampType(arrow.Nanosecond, ""), false, false},
		// Replied with 12 digits.
		{typeFloat64, false, false},
	}
	for _, c := range cases {
		col := columnMeta{Name: "c", Type: c.t, Indexed: true}
		if got := col.groupable(PushdownExact); got != c.exact {
			t.Errorf("%v: groupable in the exact mode %v, want %v", c.t, got, c.exact)
		}
		if got := col.groupable(PushdownAll); got != c.all {
			t.Errorf("%v: groupable in the all mode %v, want %v", c.t, got, c.all)
		}
	}
	nanos := columnMeta{Name: "c", Type: timestampType(arrow.Nanosecond, ""), Indexed: true}
	if nanos.pushable(PushdownAll) {
		t.Error("TIMESTAMP(9): pushable in the all mode")
	}
}

func TestExactIntSum(t *testing.T) {
	cases := []struct {
		k      Kind
		nn     float64
		lo, hi string
		want   bool
	}{
		{KindInt32, 1<<22 - 1, "", "", true},
		{KindInt32, 1 << 22, "", "", false},
		{KindInt16, 1 << 37, "", "", true},
		{KindBool, 1 << 40, "", "", true},
		{KindInt64, 3, "-1000", "5", true},
		{KindInt64, 1, "9007199254740991", "9007199254740991", true},
		{KindInt64, 2, "9007199254740991", "9007199254740991", false},
		{KindInt64, 3, "-9007199254740992", "9007199254740992", false},
		{KindInt64, 2, "-4503599627370496", "1", false},
		{KindInt64, 2, "-4503599627370495", "1", true},
		{KindInt64, 1, "inf", "-inf", false},
		{KindInt64, 1, "nan", "5", false},
		{KindInt64, 1, "", "5", false},
	}
	for _, c := range cases {
		if got := exactIntSum(c.k, c.nn, c.lo, c.hi); got != c.want {
			t.Errorf("%v of %v in [%s, %s]: %v, want %v", c.k, c.nn, c.lo, c.hi, got, c.want)
		}
	}
}
