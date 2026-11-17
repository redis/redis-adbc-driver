# Verification and remaining gates

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source: `75220f2f` plus the finite numeric candidate (unrelated to these paths).
Status: local checks complete; upstream fixes remain open.

[Intent](intent.md), [spec](spec.md), [plan](plan.md), [sanitized counts](results.json).

Owned Software with Redis 8.8.0/Search, three primaries, trusted TLS/RESP3,
noeviction, management-verified effective QPF 4x, six Search workers, mnp routing
and 32 connections. Started with `make -C go redis-up`; setup gates repeated
before native tests. Client: macOS arm64 through Docker published port. Raw
reports, connection details and generated trust remain private.

## R / adbi (#252)

A fresh isolated CRAN adbi **0.1.3** install reproduces all four failures:
one/two integer parameters, NULL text and mixed NULL/integer parameters, with
rebinding. Native unbound reads pass: **1 passed, 4 failed, 1 deselected** with
`--runxfail`. Every failure is the tracked named-params classification error;
tests first assert exact unnamed ADBC fields and parameter count.

The repository's `r/install_acceptance.R` installed its pinned adbi **0.1.4**
source plus explicit empty-name/TIME patches into a separate owned library.
The same native tests with `--runxfail`: **5 passed, zero failures/skips, 1
Quarto deselection**. These query fixtures do not use TIME parameters. R **4.6.2**.
This is patched-dependency evidence, not unmodified CRAN compatibility.

Existing upstream [issue #127](https://github.com/r-dbi/adbi/issues/127) and
[fix PR #128](https://github.com/r-dbi/adbi/pull/128) remain open. The supported
[pinned recipe](../../../../r/README.md) remains explicit; #252 stays open for
upstream acceptance/released-package qualification. Driver parameter metadata
and test assertions were not changed to obtain a pass.

## Hugging Face Datasets (#253)

Latest installed Datasets **5.2.0**, Python **3.14.10**, PyArrow **26.0.1**:
the independent local Arrow-table test with `--runxfail` yields **one nonempty
pass, one empty failure, three deselections**. Empty default save still writes
zero shards and reload raises IndexError. This reproduction uses no Redis,
ADBC handles, downloaded dataset or network model.

Native ADBC populated/default save and empty/explicit single-shard save both
pass: **2 passed, zero failures/skips**. Exact rows, features and filtered rows
are checked. The test's existing narrow 5.1.0 expected-failure recognition was
not widened to hide the newly observed 5.2.0 failure.

Existing upstream [issue #8912](https://github.com/huggingface/datasets/issues/8912)
and [fix PR #8913](https://github.com/huggingface/datasets/pull/8913) remain open.
Keep `num_shards=1` for empty datasets; #253 stays open until a qualified fixed
release or explicitly agreed compatibility policy. No duplicate issue/PR.

## Delivery and review

GitHub CLI upstream-comment permission was denied; the connected GitHub tool
also returned 403 for adbi. At the requestor's direction, the signed-in browser
successfully posted and visibly verified both evidence updates:
[adbi comment](https://github.com/r-dbi/adbi/issues/127#issuecomment-6963354137)
and [Datasets comment](https://github.com/huggingface/datasets/issues/8912#issuecomment-6963369158).
The read API independently confirmed each posted body and URL. The project
trackers link these updates. Existing upstream fix PRs still remain open.

Agent review checked unchanged dependency contracts, exact failure recognition,
full fetch/state assertions, separate patched/released environments and honest
failure/skip accounting. No shipping integration behavior changed. Human review,
hosted CI, dependency merges/releases and full upstream suites are not asserted.
