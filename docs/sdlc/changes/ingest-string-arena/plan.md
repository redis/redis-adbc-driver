# Plan

1. Review Clone/coercion and synchronous store ownership; freeze arena guard/cap.
2. Implement bounded owned strings only, independent of row-buffer reuse.
3. Differential tests include String/LargeString, NULL/empty/Unicode/NUL, slices,
   partial row prefixes, input mutation/release and one retained scalar, exact
   cap/oversize fallbacks, aliased/bounded targets and invalid metadata/error order.
4. Focused unit/race, complete-conversion allocation control, verified Software
   managed/unmanaged multi-record + atomic/partial/lost-reply/cancel checks.
5. Root grants every heavy slot. Build unique versioned clean binaries and run
   identical frozen native helper on baseline/candidate, serially. Record exact
   source/binary/helper/input hashes, every attempt and environment.
6. Evaluate original gate, peer review, publish accepted code or evidence only.
   Root owns final fixture/credential teardown. No merge without user instruction.

See [intent](intent.md) and [contract](spec.md).

Execution completed at clean `85cf660b`; quantitative gates passed. Final agent
artifact review and owner cleanup proof precede authorized PR publication.
