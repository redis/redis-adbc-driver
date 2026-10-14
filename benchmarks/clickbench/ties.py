"""Independent DuckDB membership and rank-band validation for ClickBench LIMITs."""
import json
import re

import duckdb


def quote(name):
    return '"' + name.replace('"', '""') + '"'


def check_ties(con, sql, actual, same_value):
    """Return valid/mismatch/needs_tie_validation, with no execution-only passes.

    The oracle must expose the same independently typed source as references.py.
    Supports the official single-key ORDER BY and unordered LIMIT shapes. Unknown
    syntax stays unresolved. OFFSET admits partial ties at both rank boundaries.
    """
    suffix = re.search(r'\s+LIMIT\s+(\d+)(?:\s+OFFSET\s+(\d+))?\s*;?\s*$', sql, re.I)
    if not suffix:
        return "needs_tie_validation"
    limit, offset = int(suffix[1]), int(suffix[2] or 0)
    base = sql[:suffix.start()]
    order = re.search(r'\s+ORDER\s+BY\s+(.+)$', base, re.I)
    key = None
    if order:
        expression = order[1].strip()
        base = base[:order.start()]
        match = re.fullmatch(r'([A-Za-z_]\w*)(?:\s+(ASC|DESC))?', expression, re.I)
        if not match:
            return "needs_tie_validation"
        key, direction = match[1], (match[2] or "ASC").upper()
        # Q25 orders by an unprojected source column. Keep it in the oracle,
        # but compare only the selected SearchPhrase returned by the driver.
        if re.match(r'^SELECT\s+SearchPhrase\s+FROM\s+', base, re.I) and key.lower() == "eventtime":
            base = re.sub(r'^SELECT\s+SearchPhrase\s+FROM\s+', 'SELECT SearchPhrase, EventTime FROM ', base, flags=re.I)
            hidden = True
        else:
            hidden = False
    description = con.execute("DESCRIBE " + base).fetchall()
    columns = [row[0] for row in description]
    visible = columns[:-1] if key and hidden else columns
    if any(not isinstance(row, (list, tuple)) or len(row) != len(visible) for row in actual):
        return "mismatch"
    total = con.execute("SELECT COUNT(*) FROM (" + base + ") AS universe").fetchone()[0]
    if len(actual) != min(limit, max(0, total - offset)):
        return "mismatch"
    if not actual:
        return "valid"
    if key:
        matches = [name for name in columns if name.lower() == key.lower()]
        if len(matches) != 1:
            return "needs_tie_validation"
        key = quote(matches[0])
        ranked = ("WITH universe AS (" + base + "), ranked AS (SELECT *, "
                  + f"RANK() OVER (ORDER BY {key} {direction}) AS __tie_rank, "
                  + f"COUNT(*) OVER (PARTITION BY {key}) AS __tie_size FROM universe) "
                  + "SELECT " + ",".join(quote(c) for c in visible)
                  + ",__tie_rank,__tie_size FROM ranked WHERE __tie_rank <= ? "
                  + "AND __tie_rank + __tie_size > ? ORDER BY __tie_rank")
        # Refuse to grant correctness when the evidence exceeds this bound.
        cursor = con.execute(ranked, [offset + limit, offset])
        candidates = json.loads(json.dumps(cursor.fetchmany(1_000_001), default=str))
        if len(candidates) > 1_000_000:
            return "needs_tie_validation"
        position, start = 0, 0
        while start < len(candidates):
            rank, size = candidates[start][-2:]
            end = start
            while end < len(candidates) and candidates[end][-2] == rank:
                end += 1
            required = min(rank + size - 1, offset + limit) - max(rank, offset + 1) + 1
            remaining = [list(row[:-2]) for row in candidates[start:end]]
            for row in actual[position:position + required]:
                for index, candidate in enumerate(remaining):
                    if all(same_value(a, b) for a, b in zip(row, candidate)):
                        remaining.pop(index)
                        break
                else:
                    return "mismatch"
            position += required
            start = end
        return "valid" if position == len(actual) else "mismatch"
    # Unordered LIMIT: each returned group and every aggregate must occur in
    # the full independent result, with multiplicity. No arbitrary reference
    # LIMIT sample can establish membership for a different set of groups.
    placeholders = " UNION ALL ".join("SELECT " + ",".join(
        f"CAST(? AS {description[i][1]}) AS {quote(c)}" for i, c in enumerate(visible)) for _ in actual)
    values = [value for row in actual for value in row]
    # Casting must not silently round an invalid count or truncate a value.
    try:
        converted = con.execute(placeholders, values).fetchall()
    except duckdb.ConversionException:
        return "mismatch"
    converted = json.loads(json.dumps(converted, default=str))
    if any(not all(same_value(a, b) for a, b in zip(row, cast_row))
           for row, cast_row in zip(actual, converted)):
        return "mismatch"
    missing = con.execute("SELECT COUNT(*) FROM ((" + placeholders + ") EXCEPT ALL (" + base + ")) AS missing", values).fetchone()[0]
    return "valid" if missing == 0 else "mismatch"
