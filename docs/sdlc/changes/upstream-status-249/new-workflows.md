# Additional workflow scope and dedicated guides

Date: 2026-10-30. Author/reviewer: OpenAI Codex. User correction: the original
review omitted new integration workflows; the user additionally requested
dedicated guides for each integration.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).
Issue: https://github.com/redis/redis-adbc-driver/issues/249 .

## Scope and acceptance

Extend the status page and guide index to include NumPy, scikit-learn, Jupyter,
Hugging Face Datasets, Streamlit and SciPy. Provide one dedicated guide per
workflow with installation, a complete example, environment configuration,
recorded coverage and explicit limits. Source evidence: PR #248 head
`a99c882b4088aaff32e1abdad449f6e1b575b41a`, open and unmerged when checked.
The examples use the public DB-API and Arrow APIs; the saved test runner needs
a checkout containing #248. Preserve distinct Software/OSS test provenance.

## Implementation and verification

Reviewed PR #248's complete file list, workflow tests and validation records,
and each upstream project's documentation structure. Added six guides with
read-only literal queries or isolated unique demo tables cleaned up in finally
blocks. Streamlit uses bound values and Jupyter describes kernel-side setup.
Dataset persistence explicitly uses one shard for empty-result compatibility.

Compiled all six Python examples, checked all new relative links/anchors against
the changed files and current main tree, checked index coverage and retained
upstream PR URLs, and checked whitespace. Live guide examples were subsequently run against isolated Redis Software with
Search and verified 4x QPF at the user's request; see [validation](validation.md).
No full documentation-site build was run. Historical runtime results remain
linked and qualified.

The original twelve contributions remain tracked; the six new workflows do not
inherit their submission/acceptance status. All eighteen integration paths now
have a guide entry (dbt now has a tested local starter guide).
Upstream submissions for the six additions and maintainer review remain
outstanding; no new upstream submission is claimed. No human approval is inferred.
