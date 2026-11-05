#!/usr/bin/env python3
"""Reapply Redis's plan admission guard after regenerating go/pkg/driver.go.

The upstream driverbase _tmpl/driver.go.tmpl is not shipped in this repository.
Run this after that external generation step; fail if its copy site changes.
"""
from pathlib import Path

path = Path(__file__).resolve().parents[1] / "go/pkg/driver.go"
text = path.read_text()
marker = '\tvar safeLen int\n'
start = text.index('func RedisStatementSetSubstraitPlan(')
end = text.index('\n//export ', start)
block = text[start:end]
guard = '''\t// Redis-specific admission must run before narrowing or copying C input.
\tif length > C.size_t(redis.SubstraitMaxPlanBytes) || (length != 0 && plan == nil) {
\t\tsetErr(err, "Substrait: invalid plan pointer or plan_bytes budget exceeded")
\t\treturn C.ADBC_STATUS_INVALID_ARGUMENT
\t}

'''
copy_site = "e := st.stmt.SetSubstraitPlan(st.NewContext(), C.GoBytes(unsafe.Pointer(plan), C.int(safeLen)))"
clear_sql = '\n\tif e == nil {\n\t\tst.sqlQuery = ""\n\t}'
if copy_site not in block:
    raise SystemExit("Substrait C copy site changed; review admission guard")
if copy_site + clear_sql not in block:
    block = block.replace(copy_site, copy_site + clear_sql, 1)
if guard not in block:
    if block.count(marker) != 1:
        raise SystemExit("Substrait C copy site changed; review admission guard")
    block = block.replace(marker, guard + marker, 1)
path.write_text(text[:start] + block + text[end:])
