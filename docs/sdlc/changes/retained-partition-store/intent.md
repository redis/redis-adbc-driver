# Intent

2026-10-21. Base `1282e4a`. Author: Codex; requestor: Albert Wong (runtime features required). Status: in progress. Refs #359/#360/#362/#363.

Implement the private storage foundation for opt-in replayable partition results (#362), subsequently used for global SQL (#359) and additional layouts/large IDs (#360). Existing live mode remains the default. This change does not enable unbudgeted query materialization or claim completion of those runtime features.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
