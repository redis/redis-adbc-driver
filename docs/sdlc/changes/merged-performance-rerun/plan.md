# Execution plan

1. Pin merged source and verify input provenance. Build explicit-version native
   library and CLI outside timing.
2. Start an isolated Software fixture through `make -C go redis-up`; verify
   topology, effective 4x QPF, memory policy, TLS and RESP3.
3. Run the unchanged full `arrow_compare.py --suite all` matrix sequentially,
   keeping raw output and infrastructure metadata private.
4. Audit all 156 trial identities, validation/status, source/version and summary
   arithmetic. Compare against historical results with their original labels.
5. Write the result report and cleanup evidence. Remove owned server/network
   and generated credential/key files; preserve other resources.

See [intent](intent.md) and [specification](spec.md).

Status: complete. All 156 trials and the audit passed; owned resources and
generated credentials were cleaned. See [validation](validation.md).
