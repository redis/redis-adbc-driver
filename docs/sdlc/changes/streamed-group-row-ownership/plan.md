# Plan

Date: 2026-11-24. Status: local implementation and verification.
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Add a coordinator-only request marker and wrap only proven star callbacks.
2. Reuse owned first-group maps after all inputs; preserve shared-memory cloning.
3. Test marker scope/restoration, nested requests, unsupported/retained/private
   paths, shared source preservation and actual parallel leaf pruning.
4. Reproduce callback-panic grant release before worker join with a gated native
   peer, then add deferred child cancellation and worker join.
5. Run race/unit checks and a fresh-leaf allocation control. Keep this optional
   change separate from the stable leaf-only allocation revision; no native
   query, library build, server operation or full benchmark run belongs here.
