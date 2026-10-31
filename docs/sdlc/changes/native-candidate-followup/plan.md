# Additional native candidate plan and evidence

- Date: 2026-10-30
- Initial source: `91064e6`
- Final validated main base: `400e24dbd44d210ba72c6364d8e101ba4ac274c2`
- Status: implemented and validated; PR review pending
- [Intent](intent.md) · [Specification](spec.md)

## Work

1. Verify Redis Software/QPF and isolate compatible application environments.
2. Save and run Ibis/MLflow, Beam, Mage, and Kestra tests.
3. Add focused selectors and profiles to the existing rerunnable harness.
4. Record actual outcomes and retained compatibility gaps; update the matrix.
5. Review, commit, and open a follow-up PR because #248 is already merged.

## Early findings

- MLflow 3.17.2 requires `pyarrow>=4,<26`; Beam 2.77.0 requires
  `pyarrow>=14.0.1,<26`. Both resolve with PyArrow 25.0.1 in isolated profiles.
- [MLflow #27201](https://github.com/mlflow/mlflow/issues/27201) already requests
  PyArrow 26. Its fix, [#27216](https://github.com/mlflow/mlflow/pull/27216), merged
  on October 28; current main permits `<27`. Reuse that upstream request.
- Kestra 2.0.7's executable requires Java 25 (class version 69), despite the
  standalone installation page's Java 21 wording. Use a separate Java 25 runtime;
  the previous Spark profile continues to use Java 21.
- Opened the requested [Beam PyArrow 26 request #40919](https://github.com/apache/beam/issues/40919)
  with a verified package-resolution reproducer and labels at creation.
- Independently reproduced MLflow 3.17.2/pandas 3.1.0's automatic empty-dataframe
  digest failure, without Redis. Opened [MLflow #27358](https://github.com/mlflow/mlflow/issues/27358).
  Its public explicit-digest option works; a content/schema-derived digest is
  used for the empty workflow, and the exact default-path failure is retained
  as a strict expected failure. Other failures are not reclassified.
- Focused execution: Ibis 2 passed, MLflow 2 passed/1 strict expected failure,
  Beam 2 passed, Mage 2 passed, Kestra 2 passed. All native reads used the
  verified Software fixture and this checkout's native shared library.
- Harness corrections made before the final run: preserve Mage's runtime
  decorators, choose Beam's local FnApi subprocess runner explicitly, configure
  Kestra 2's required ephemeral authentication, avoid YAML aliases in task
  property maps, hand off files through Kestra storage, and retrieve task
  outputs through its current API. These were harness setup/API mistakes, not
  Redis driver failures.

## Final complete run

The unchanged saved launcher completed all thirteen profiles with exit 0:
**134 passed, one private-CA skip, two strict expected failures, zero failures or
errors**. The additional five entries contribute ten passing populated/empty
workflow cases and one exact MLflow default-digest expected failure. Earlier
application suites were rerun in the same command. [Complete validation](validation.md)
and [machine-readable outcomes](native-results.json).

The complete launcher passed again after merging the intervening main changes,
including its driver updates. Final tested source:
`6a311640dd71f4c36e26907f13eddb9ce120d7a5`, based on main `400e24d`.
The native library was rebuilt from that source. This PR has no driver
implementation diff against the final main base.

Shell/Python/TOML syntax, invalid/empty profile rejection, selected-profile
reporting, local documentation links, the result totals, and all 50 unchanged
Flight SQL cells were checked. `git diff --check` passed. Owned application
test processes exited; the verified Software fixture remains available.

The support matrix now records 37 entries with exercised native paths. The
thirteen remaining entries are explicitly identified as needing integration
work or having only an excluded Flight proposal in this comparison. No Flight
SQL path was tested.
