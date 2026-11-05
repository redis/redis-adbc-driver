# Binary plan fixture

`read.bin` is a deterministic binary Substrait 0.108.0 plan produced by the Go
protobuf fixture generator in `TestSubstraitFixtures`; `read.json` describes the
same message. It reads `it_substrait(id: i64 required, value: string nullable)`.
The execution behavior is per-plan. There are no external dependencies or URNs.

Regenerate from go/ with
`UPDATE_SUBSTRAIT_FIXTURES=1 go test -run '^TestSubstraitFixtures$' .`.
The source/version/generator hashes are documented in
[the binding record](../../internal/substrait/README.md).
