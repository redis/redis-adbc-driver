# Validation — November 3, 2026

Documentation source: bb83527f305b75ea731696f55f78eb75121bbbc0 plus this update.
Immutable Flight evidence commit: e53c1f37fa99b6593d72d35088c1b403ac8b397d.

- Table shape/accounting: 50 tool rows; 43 qualifying tools; 37 native ADBC
  paths; 27 Flight paths; 24 overlap; exactly 27 dated Flight validation cells.
- Existing acceptance runner tests: `python3 -m unittest discover -s scripts
  -p test_acceptance_runner.py` — 13 passed. These are offline harness tests,
  including the unchanged 50-row acceptance-audit mapping.
- Existing distribution tests: `python3 -m unittest discover -s scripts
  -p test_distribution.py` — 7 passed, including installation inventory mapping.
- Evidence checks: all 13 immutable Flight targets exist; the 50 tool names/order
  and every native ADBC evidence cell remain unchanged.
- Git whitespace check: `git diff --check` — passed.

No live Redis, Software/QPF or Enterprise validation was performed in this
repository for the documentation update. The linked historical Flight record
contains 28 automated passes, two strict expected failures and three application
receipts on community Flight source c019afc with Redis OSS 8.6.2 and Search.
It does not establish current Enterprise TLS/authentication qualification.

Flight issue #57 tracks both empty-Datasets expected failures, which share the
upstream root cause reported in huggingface/datasets#8912. The explicit
one-shard workaround passes; expected failures are not counted as passes.
