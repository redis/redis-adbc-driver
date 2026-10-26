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
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// arePattern handles syntax RE2 cannot represent. Constraining the match's
// endpoint makes selection leftmost-longest instead of Perl's first branch.
// Searches and endpoint probing share a deadline against pathological input.
type arePattern struct {
	body     string
	re       *regexp2.Regexp
	shortest bool
}

const regexTimeout = 250 * time.Millisecond

func compileARE(body string, shortest bool) (*arePattern, error) {
	re, err := regexp2.Compile(body, regexp2.RE2)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = regexTimeout
	return &arePattern{body: body, re: re, shortest: shortest}, nil
}

func (p *arePattern) at(s string, bytePos int) ([]int, error) {
	text := []rune(s)
	start := utf8.RuneCountInString(s[:bytePos])
	deadline := time.Now().Add(regexTimeout)
	match, err := p.re.FindRunesMatchStartingAt(text, start)
	if err != nil {
		return nil, invalidRegexp("", err, false)
	}
	if match == nil {
		return nil, nil
	}
	earliest := match.Index
	lo, hi, step := len(text), match.Index+match.Length, -1
	if p.shortest {
		lo, hi, step = earliest, match.Index+match.Length, 1
	}
	for end := lo; (step < 0 && end >= hi) || (step > 0 && end <= hi); end += step {
		if time.Now().After(deadline) {
			return nil, stateErr(stateQueryCanceled, "regular expression match timed out")
		}
		re, err := regexp2.Compile(fmt.Sprintf("(?:%s)(?=(?s:.{%d})\\z)", p.body, len(text)-end), regexp2.RE2)
		if err != nil {
			return nil, invalidRegexp("", err, false)
		}
		re.MatchTimeout = time.Until(deadline)
		candidate, err := re.FindRunesMatchStartingAt(text, earliest)
		if err != nil {
			return nil, stateErr(stateQueryCanceled, "regular expression match timed out")
		}
		if candidate != nil && candidate.Index == earliest {
			match = candidate
			break
		}
	}
	offsets := make([]int, len(text)+1)
	at := 0
	for i, c := range text {
		offsets[i] = at
		at += utf8.RuneLen(c)
	}
	offsets[len(text)] = len(s)
	groups := match.Groups()
	out := make([]int, 2*len(groups))
	for i, g := range groups {
		out[2*i], out[2*i+1] = -1, -1
		if len(g.Captures) > 0 {
			out[2*i], out[2*i+1] = offsets[g.Index], offsets[g.Index+g.Length]
		}
	}
	return out, nil
}

// translateARE preserves escapes and only rewrites PostgreSQL's word
// constraints. In AREs \b is a backspace, not Perl's word boundary.
func translateARE(s string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '[' {
			inClass = true
		}
		if c == ']' {
			inClass = false
		}
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		esc := s[i]
		if inClass {
			b.WriteByte('\\')
			b.WriteByte(esc)
			continue
		}
		switch esc {
		case 'y':
			b.WriteString(`\b`)
		case 'Y':
			b.WriteString(`\B`)
		case 'm':
			b.WriteString(`(?<!\w)(?=\w)`)
		case 'M':
			b.WriteString(`(?<=\w)(?!\w)`)
		case 'b':
			b.WriteString(`\x08`)
		default:
			b.WriteByte('\\')
			b.WriteByte(esc)
		}
	}
	return b.String()
}

// expandedRegexp removes insignificant whitespace/comments outside brackets.
func expandedRegexp(s string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			b.WriteByte(c)
			i++
			b.WriteByte(s[i])
			continue
		}
		if c == '[' {
			inClass = true
		}
		if c == ']' {
			inClass = false
		}
		if !inClass {
			if strings.ContainsRune(" \t\n\r\f\v", rune(c)) {
				continue
			}
			if c == '#' {
				for i < len(s) && s[i] != '\n' {
					i++
				}
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// basicRegexp converts BRE grouping and counts; +, ? and | are literal.
func basicRegexp(s string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '[' {
			inClass = true
		}
		if c == ']' {
			inClass = false
		}
		if c == '\\' && i+1 < len(s) {
			i++
			c = s[i]
			if !inClass && strings.ContainsRune("(){}", rune(c)) {
				b.WriteByte(c)
			} else {
				b.WriteByte('\\')
				b.WriteByte(c)
			}
			continue
		}
		if !inClass && strings.ContainsRune("()+?|{}", rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}
