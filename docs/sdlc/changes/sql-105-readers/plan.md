# Plan

2026-11-03. [Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Retain the reproduced 25,000-row failure as a regression; verify every row,
   a refresh with fewer changed rows, and independent old/new reader results.
2. Add execution-owned generation leases, atomic registration/publication,
   immutable sequence bounds, and retryable retired-generation collection.
3. Cover multiple readers/generations, early release, cancellation, connection
   close, abandoned-reader recovery and lease renewal. Preserve existing SQL
   lifecycle/write errors and retained source validation.
4. Run focused integration/race tests, the full Software Go suite and native
   build. Review lease expiry, publication races, metadata slot safety, resource
   ownership, cancellation and recovery. Update the public guide.
5. Submit a draft PR with evidence and leave #105 open for the reader guarantee.
   Merging and closing the issue are outside this submission. dbt stays in its
   own linked issues.
