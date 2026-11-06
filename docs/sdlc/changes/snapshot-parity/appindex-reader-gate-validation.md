# Application-index reader fixture admission

Date: 2026-11-04. Parent 7fee549. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh complete ordinary race run passed 81 test events with eight explicit
skips, then failed TestACLAppIndexReader (root 112.436 s). Its fixture expected
AppIndex=true although managed adoption intentionally publishes a private copy.
The fixture now requires Applied/ManagedCopy, AppIndex=false and the exact capture
SourceIndex. SQL does not reuse that application's live index.

The test checks its existing SETUSER principal-setup capability before producing
import data, using exactly the same reader commands/key grants and existing skip
condition. It does not add permissions, broaden skips, activate a reader lease
layout or bypass server fences. Its positive/negative reader assertions remain
behind this precondition; they still need native ownership/reader-protocol work
on a server where principal setup is available. The protocol-16 reader design is
not activated. A skipped fixture does not prove restricted-reader support.

Independent existing adoption families cover actual managed source selection,
private index policy and public SQL results without claiming reader-principal
compatibility. Mutating selections run sequentially on owned strict-TLS Redis
Software with noeviction and verified active effective 4x QPF. The two managed-copy families pass under the race detector; the reader fixture
explicitly skips because Software blocks ACL SETUSER. Zero failures, root
3.523 s. CUDA has no applicable selected tests and is skipped. Formatting and
diff whitespace checks pass. Restricted-principal, complete ordinary/platform,
recovery, resume, sustained compaction and comparative performance remain open. Author review is not independent maintainer approval.
