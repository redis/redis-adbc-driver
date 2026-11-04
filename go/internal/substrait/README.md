# Pinned Substrait bindings

Source: [Substrait v0.108.0](https://github.com/substrait-io/substrait/tree/v0.108.0),
commit `a4d8e00342d027560e3534a564a7b55ddba4b8e4` (Apache-2.0).
`source/substrait/` and the two extension YAML files are unmodified upstream
sources. `SHA256SUMS.json` pins them, including the complete schema import set and `descriptor.pb`. Tests compare
every generated Substrait descriptor to this pinned set, with no descriptor option changes.
Google well-known types use the existing official protobuf runtime.

Generated locally with buf 1.66.0 and protoc-gen-go 1.36.6. This is a schema
adapter, not a separate Substrait executor. The generated package is internal;
no changes to existing public Go APIs or the C ABI. The Go runtime remains the version in go.mod.

The published protobuf Go module currently stops at v0.85.0. The missing current
release is tracked by [substrait-protobuf#7](https://github.com/substrait-io/substrait-protobuf/issues/7).
Generation avoids depending on older message definitions with a newer version
string. Replacing these bindings with an upstream module requires descriptor
and fixture equivalence checks.

Regenerate from this directory, with the pinned generators on PATH:

```sh
buf generate
buf build source --as-file-descriptor-set -o descriptor.pb
```

`buf.gen.yaml` uses protoc Go import mappings only; it does not use managed
mode or modify any protobuf descriptor options. Check source hashes and review
all generated changes. After external driverbase C wrapper regeneration, run
`python3 scripts/patch_substrait_boundary.py` from the repository root to restore
the admission guard before the C input copy. The external `_tmpl/driver.go.tmpl`
is not included in driverbase's published Go module or this repository.

The reproducible producer fixture lives in `../../testdata/substrait/`:

```sh
# From go/; a Go protobuf producer, with readable protobuf JSON alongside it.
UPDATE_SUBSTRAIT_FIXTURES=1 go test -run '^TestSubstraitFixtures$' .
```

The fixture generator uses the actual pinned generated protobuf messages and
deterministic protobuf serialization. JSON is a readable fixture description;
the public setter accepts binary protobuf only. Additional semantic fixtures
are constructed by the focused Go acceptance tests.
