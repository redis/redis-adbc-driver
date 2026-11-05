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

package testenv

import "testing"

func TestValidateURI(t *testing.T) {
	for _, tc := range []struct {
		uri   string
		valid bool
	}{
		{"", true}, {"rediss://default:secret@localhost:6379/0", true},
		{"redis://default:secret@localhost:6379/0", false},
		{"rediss://localhost:6379/0", false}, {"rediss://:secret@localhost/0", false},
		{"rediss://default@localhost/0", false}, {"rediss://default:secret@localhost/0?skip_verify=true", false},
		{"rediss://default:%invalid@localhost/0", false},
	} {
		err := ValidateURI(tc.uri)
		if (err == nil) != tc.valid {
			t.Errorf("valid=%v: %v", tc.valid, err)
		}
	}
}
