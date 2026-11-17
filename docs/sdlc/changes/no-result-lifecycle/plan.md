# Plan

See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reuse owned Software fixture/principal management and public native DBAPI.
2. Add independent raw permission probes, six paired DML refusal cases and four
   synchronized parameter-producer cancellation/reuse/Close cases.
3. Run against owned verified Software; preserve failures and correct actual
   implementation/fixture defects without weakening assertions.
4. Add CI execution and sanitized evidence; review ownership, error parity and
   evidence limits. Leave the performance portion of #447 open.
