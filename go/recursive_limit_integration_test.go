// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestSQLRecursiveConsumerLimit(t *testing.T) {
	h := newSQLHarness(t)
	for _, op := range []string{"UNION", "UNION ALL"} {
		prefix := `WITH RECURSIVE t(n) AS (VALUES(1) ` + op + ` SELECT n+1 FROM t) `
		h.expectRows(prefix+"SELECT * FROM t LIMIT 4 OFFSET 2", "3", "4", "5", "6")
		h.expectRows(prefix + "SELECT * FROM t LIMIT 0")
	}
	h.expectRows(`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT * FROM t ORDER BY n DESC LIMIT 2`, "8", "7")
	h.expectRows(`WITH RECURSIVE t(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM t WHERE n<8) SELECT (SELECT COUNT(*) FROM t),n FROM t LIMIT 1`, "8|1")
}
