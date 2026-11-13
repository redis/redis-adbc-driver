# Specification and audit: rejected first-insert index experiment

Date: 2026-11-13. Status: evidence only; candidate not adopted.
See [intent](intent.md), [plan](plan.md), [results](results.json).

## Candidate scope and proof

The unapplied [patch](candidate.patch) is the exact diff from baseline attribution
commit `c2776bda91e24c6b8d62d45a16ebb59a78f48e2e` to candidate
`f81b7a048741e181895f0f35ee557953cf849c3d`. It requires that baseline to apply;
its historical documentation describes the experimental candidate, not current
production behavior. Candidate source remains available on its separate branch.

Only ordinary managed Arrow with a fresh owned deferred output was eligible.
Flex, imports/provenance, retained operations and maintenance stayed deferred.
After checking its first nonempty conversion slice, the writer created the
private index before complete first-insert HASH batches. SQL/input admission,
row/byte budgets, metadata/source fences and one publication per call were
unchanged. Empty input retained the deferred path.

Audit findings and protections:

- Late TAG/NaN facts affect query-planning metadata, not FT.CREATE's schema.
  Seal compared exact index name and creation arguments; late value facts were
  tested, including truncating TAG values and NaN.
- Insert batches reject both existing HASHes and directory members before
  mutation. Replace/import batches and legacy private mutations invalidate the
  runtime proof; seal rebuilds and retires the early index. No runtime flag is
  serialized, and retained prepare/resume remains deferred.
- CREATE uses the existing top-level Software registry command bracketed by
  lease/ownership gates. A constant additional Lua gate derives the data ID
  from FT.CREATE's existing arguments and checks the declared directory is
  empty at both gates. Distinct outputs use identical script bodies. There is
  no per-output compiled-script growth.
- Existing registered data ownership, index names and orphan-index cleanup
  retain canceled/unknown CREATE outputs for reclamation. A lost CREATE aborts
  without replay or an alternative index attempt; a regression verified GC.
- Final prefix/identity, indexing-error/readiness and exact directory/native
  indexed-row proofs remain. Only typed query-ACL refusal selects the existing
  private initial-scan fallback; unknown replies and warnings abort. Actual
  write-only and EVAL-only Software principals were exercised.

Correctness acceptance passed, including race tests and independent full typed
fetches. Performance acceptance failed: every measured default-call candidate
sample exceeded its baseline range. Removing notification calls did not remove
the corresponding overall cost. No specific server-internal cause is proven by
client-hook attribution. Do not retune/reintroduce this candidate without a new
concrete hypothesis and matched evidence.
