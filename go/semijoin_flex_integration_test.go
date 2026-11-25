// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"strings"
	"testing"
)

// With adbc.redis.flex=true, snapshot reads traverse exact row-ID directories
// and ignore Search candidates, so a page probe's key union cannot narrow the
// inner side: every 256-key batch rescanned the whole inner table (#639,
// TPC-H Q22). A large Flex inner side must be read once by the whole
// decorrelated build, with unchanged results.
func TestSQLFlexSemiJoinBuildsInnerOnce(t *testing.T) {
	h := newSQLHarnessWithOptions(t, map[string]string{OptionStringFlex: "true"})
	drop := func() {
		h.exec("DROP TABLE IF EXISTS it_fsj_outer")
		h.exec("DROP TABLE IF EXISTS it_fsj_inner")
	}
	drop()
	t.Cleanup(drop)
	// 2,000 inner rows: above semiJoinEagerRows, so only the Flex rule can
	// select the whole build. Keys 1..2000, with v NULL on every 10th row.
	h.exec("CREATE TABLE it_fsj_inner (k BIGINT, v INTEGER)")
	var inner []string
	for k := 1; k <= 2000; k++ {
		v := fmt.Sprint(k % 7)
		if k%10 == 0 {
			v = "NULL"
		}
		inner = append(inner, fmt.Sprintf("(%d, %s)", k, v))
	}
	h.exec("INSERT INTO it_fsj_inner VALUES " + strings.Join(inner, ", "))
	// 2,500 distinct outer keys (about ten 256-key probe batches): NULL keys,
	// keys without inner rows, and keys that match.
	h.exec("CREATE TABLE it_fsj_outer (id INTEGER, k BIGINT, x INTEGER)")
	var outer []string
	for id := 1; id <= 2500; id++ {
		k := fmt.Sprint(id)
		switch {
		case id%250 == 0:
			k = "NULL"
		case id%7 == 0:
			k = fmt.Sprint(5000 + id) // no inner row
		}
		outer = append(outer, fmt.Sprintf("(%d, %s, %d)", id, k, id%7))
	}
	h.exec("INSERT INTO it_fsj_outer VALUES " + strings.Join(outer, ", "))

	exists, inMatch := 0, 0
	for id := 1; id <= 2500; id++ {
		if id <= 2000 && id%250 != 0 && id%7 != 0 {
			exists++
			// x = id % 7 IN (v of key id): v = id % 7 unless id%10 == 0 (NULL).
			if id%10 != 0 {
				inMatch++
			}
		}
	}
	w := h.expectWork(`SELECT COUNT(*) FROM it_fsj_outer o WHERE EXISTS (SELECT 1 FROM it_fsj_inner i WHERE i.k = o.k)`,
		fmt.Sprint(exists))
	if w.runs != 0 || w.semiJoins != 1 {
		t.Errorf("Flex EXISTS over a large inner side: %+v, want one whole build and no per-row runs or page probes", w)
	}
	w = h.expectWork(`SELECT COUNT(*) FROM it_fsj_outer o WHERE NOT EXISTS (SELECT 1 FROM it_fsj_inner i WHERE i.k = o.k)`,
		fmt.Sprint(2500-exists))
	if w.runs != 0 || w.semiJoins != 1 {
		t.Errorf("Flex NOT EXISTS over a large inner side: %+v, want one whole build", w)
	}
	w = h.expectWork(`SELECT COUNT(*) FROM it_fsj_outer o WHERE o.x IN (SELECT i.v FROM it_fsj_inner i WHERE i.k = o.k)`,
		fmt.Sprint(inMatch))
	if w.runs != 0 || w.semiJoins != 1 {
		t.Errorf("Flex correlated IN over a large inner side: %+v, want one whole build", w)
	}
	// Three-valued NOT IN: a matched key's only inner value is either x itself
	// (FALSE) or NULL (NULL, not true), so only rows without inner rows,
	// including NULL keys, satisfy NOT IN.
	h.expectRows(`SELECT COUNT(*) FROM it_fsj_outer o WHERE o.x NOT IN (SELECT i.v FROM it_fsj_inner i WHERE i.k = o.k)`,
		fmt.Sprint(2500-exists))
}
