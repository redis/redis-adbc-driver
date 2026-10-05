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
	"strings"
	"testing"
)

// Every abbreviation of Postgres's Default set resolves, in either case, to
// its offset or to its zone, which the embedded tz database has.
func TestZoneAbbrevs(t *testing.T) {
	if len(zoneAbbrevs) != 195 {
		t.Errorf("%d abbreviations; Postgres 16's Default has 195", len(zoneAbbrevs))
	}
	for name, a := range zoneAbbrevs {
		for _, spelling := range []string{name, strings.ToUpper(name)} {
			z, err := resolveZone(spelling)
			switch {
			case err != nil:
				t.Errorf("%s: %v", spelling, err)
			case a.zone == "" && (!z.fixed() || z.off != a.off || z.abbr != strings.ToUpper(name)):
				t.Errorf("%s: %+v, want the fixed offset %d", spelling, z, a.off)
			case a.zone != "" && (z.loc == nil || z.loc.String() != a.zone || z.dyn == nil):
				t.Errorf("%s: %+v, want the zone %s", spelling, z, a.zone)
			}
		}
	}
}
