# Plan

1. Implement and locally verify the retained TLS checker and routing guards.
2. Record current runtime full Go/native/TLS checks and refresh scoped ClickBench.
3. Run installed wheels across three architectures, Python 3.13/3.14 and
   ADBC manager 1.8/1.12 through the existing release CI.
4. Validate Software and an owned Cloud Pro deployment with independent workers.
5. Review evidence and cleanup, update the follow-up PR after #430 and close #363 on merge only when
   all acceptance criteria have evidence. No automatic merge.

See [intent](intent.md), [spec](spec.md), [evidence](evidence.md).

All five steps are complete. Runtime/fixes merged in #430/#433; final evidence
records the successful hosted runs and closes #363 through its documentation PR.
