# Plan

1. Read existing database/connection/statement lifecycle and query-worker options.
2. Add copied defaults and reuse one bounded parser/effective-default helper.
3. Verify public database option initialization, string/int dispatch, statement
   snapshot inheritance/override, invalid atomicity and unrelated typed routing.
4. Run focused/race and complete endpoint-free unit checks with bounded CPU/RAM.
5. Root review precedes publication; live independent readers remain a later gate.
