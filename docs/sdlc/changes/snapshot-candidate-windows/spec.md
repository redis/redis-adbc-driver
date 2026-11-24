# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`snapshotCandidateScan` reads each source's Search candidates in bounded row-ID windows
`(query) @__rowid:[(after hi]` with `hi = min(after + width, bound)`, starting at row ID 0:

- The initial width is 16 pages. A full page continues from its last row ID, and the next
  width becomes the row-ID span that page needed (at least one page). Each window then costs
  about one page's worth of rows.
- A short page proves the window `(after, hi]` complete; the scan resumes at `hi`. Windows with
  fewer than half a page double their width (capped at 2^40). Search drives a selective
  intersection from its smaller iterator, so sparse windows stay cheap while they widen. An
  empty window reads the next one before returning, so an empty head still means the source is
  finished for this page.
- `bound` is the table's row sequence, read once when the scan is created. Pinned published
  segments' row IDs were allocated before it, and are below 2^53, so windows stay exact numeric
  bounds. A sequence read failure, a nonpositive or out-of-range value, or a zero page keeps
  the original open-ended pages.
- Past `bound`, one open-ended tail page `(query) @__rowid:[(bound +inf]` completes the source.
  If a stale bound left later candidates, they are read in the original open-ended mode, so
  completeness never depends on the bound.

Candidate identity checks reject IDs at or below `after`, above the window end, or outside
exact-double range. Ordering, multi-segment newer-directory visibility (ZMSCORE), shadowed-HASH
identity checks, tombstones, partial-result warnings, `TIMEOUT 0`, page sizes and directory
verification are unchanged.

Acceptance: a structural regression fails before the change (0 bounded windows, 63 open-ended
pages for 4 sources) and passes after. It returns complete ordered rows across dense, sparse
and empty regions with UPDATE/DELETE visibility, keeps a selective predicate to a few bounded
windows plus one tail page per source, and stays complete with a stale bound. Existing
candidate/prefetch/visibility tests pass. SF1 single-page latency shows open-ended pages are the
bottleneck. A full SF1 third-path Q19 A/B is pending a full-memory RAM fixture.
