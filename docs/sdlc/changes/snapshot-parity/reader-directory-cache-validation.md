# Reader directory hints

Status: isolated candidate; validated and measured, not integrated.

Admission and reclamation currently read the complete bounded reader namespace
directory in a separate read-only script before sending the control command.
Stable membership repeats this round trip on every non-point control operation.
The candidate keeps an immutable, atomic process-local list per store. It is a
routing hint: the server must still prove exact directory cardinality, every
canonical namespace's membership and uniqueness, and lease HASH resource limits
before mutation. The server also checks the directory's byte bound on every
admission, including with a warm hint. Equal cardinality alone cannot authorize
a namespace replacement.

Only a direct native `reader namespace changed before mutation` reply permits
refresh and another attempt, within the existing eight-attempt bound. Plain,
wrapped, joined and unknown transport errors stop; cancellation stops refresh.
Cold discovery errors cannot send an admission. Unknown acquisition outcomes
continue to resolve the original token read-only. Point validation, renewal and
release retain their direct paths. Domain registration discovery is unchanged.
No limits, storage format, lease lifetime or authorization scope change.

Focused unit checks pass with the race detector, including stable administrator
and restricted-reader admission, refusal classification, cancellation, failed
discovery and bounded churn. New Software regressions cover repeated stable
admission, equal-cardinality replacement, removal, partial-pin exclusion, and
directory/HASH byte limits after warming the hint. The native race selection
passes all five test events, no skips/failures, root 1.805 seconds, on owned
Software 8.8 with strict TLS, noeviction and API-verified effective 4x QPF.
A first private preflight was invoked from the wrong directory; the complete
rerun verifies configuration before testing and passes. No failed preflight is
counted as policy verification.

The complete actual Software SQL RBAC race selection also passes: 21 test
events, no skips/failures, root 24.833 seconds. It exercises shared-role boundaries, stable grants, captured
payload denial, poisoning, mixed-producer global capacity, stale-directory
refusals, original-token lost-reply resolution and cancellation. It does not
broaden the existing role/ACL trust boundary or raise retention quotas.

At candidate source `c306768`, the versioned library with SHA-256
`2460b91dee76f7e7d85e823bd779268890ac72a27d52554da67bf86e2ea78653`
passes complete native ADBC validation: **565 tests and 3 subtests**, **8
capability skips**, **81.50 seconds**. The environment is Python 3.14.10,
PyArrow 26.0.1, manager 1.13.0, pytest 9.2.0 and official validation revision
`47a5ddf3f2a617ac88da3891848e000c74d55b0e`. There is no selection override.
The complete snapshot race selection then passes **528 test events**, **29
explicit skips**, root **374.564 seconds**; all six test-bearing packages pass,
with no failures or data races. QPF/TLS/noeviction are reverified before each
phase. Suite durations are validation evidence, not performance comparisons.

The same candidate library independently passes **19 native TLS partition and
lifecycle checks plus 6 metadata/statistics identities**, using real separate
Software credentials. This includes cross-process workers, direct-HASH Search
independence, denied writes, negative certificates, cancellation, old/new
producer visibility, TRUNCATE, release and retained replay. Owned roles are
removed, the original aggregate cap is restored, and effective QPF/noeviction
are reverified. Full operational reports and fixture identities remain private.

The preserved comparison reference is production source `4f77d9c`, library
SHA-256 `74ec9ffe708e1feeeab4adff9481733a39d4ab4568baf9c0160269949b42c76c`.
Production differences from that reference are limited to the three cache
implementation files. The older native cap-response correction is present in
both libraries; test/workflow repairs remain separate from the candidate.

## Matched native comparison

Six independent processes compare reference `4f77d9c`, candidate `c306768`,
mutable `f4d5b78`, then reverse order, separately for reads and writes. Every
process verifies strict Software 8.8 TLS, noeviction and effective 4x QPF.
Local builds, tests, compilers and pytest are absent before and throughout each
process; the 500 ms census reports zero violations. The other owned Software
fixture is stopped before the timing window. Apple M5 Max, 18 logical CPUs,
36 GiB host memory; Docker has 18 CPUs and 25,421,033,472 bytes available.
This is synthetic load, not ClickBench.

Reads use 20,000 input rows plus replacement/deletion, 32 unrelated tables,
eight query shapes, one warmup and five samples per process. Timing includes
execution, full Arrow fetch and independently checked values; the full implicit
read also checks schema. All processes pass, but the full-read median does not
improve:

| Build | Full-read median ms | p95 ms |
|---|---:|---:|
| Reference | 773.598 | 1069.837 |
| Directory hints | 781.742 | 1015.355 |
| Mutable | 121.527 | 149.287 |

The candidate median is **1.05% higher** than the reference and **6.433x**
mutable. Some smaller cases improve, but this comparison does not establish a
full-read speedup. [All eight cases and samples](reader-directory-cache-read-comparison.json)
retain the negative result.

Writes alternate 64 UPDATE RETURNING statements between two 20,000-row tables
after a warmup on each table, with zero caller retries. Timing includes atomic
publication and complete RETURNING fetch. Setup and independent final checks
of all 40,000 rows/columns are outside timing. All six processes pass:

| Build | Median ms | p95 ms | Updates/s |
|---|---:|---:|---:|
| Reference | 35.514 | 72.136 | 23.478 |
| Directory hints | 31.948 | 66.836 | 25.892 |
| Mutable | 5.634 | 12.525 | 148.829 |

Throughput is **10.28% higher**, while mutable throughput remains **5.748x**
the candidate. [All write samples and source/library hashes](reader-directory-cache-write-comparison.json)
remain reported. Two processes per source do not establish statistical or
all-workload parity. The full feature and performance objective remains open.
The candidate is kept separate from the stable repair head during its hosted
compatibility checks.

Self-review checks the immutable-slice ownership and atomic pointer, exact
server membership/resource proof before mutation, direct-native-only retry,
bounded churn, point-operation independence and unchanged original-token
outcome resolution. No actionable finding remains from that review. Actual
Cluster API/platform proof and independent maintainer review remain separate
requirements; these local proxy/Software checks do not establish them.
