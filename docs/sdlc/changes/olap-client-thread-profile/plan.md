# Plan

1. Extract only the authorized isolation.py/run.py change from the dirty root into an isolated worktree.
2. Validate before opening a connection; record an explicit sanitized thread field.
3. Mock both DuckDB paths to prove 16 reaches setup, reject invalid/bool before open, preserve default 4 and third-path isolation.
4. Run the synthetic Python harness checks and document resource-profile limits; no live queries or full suites.
