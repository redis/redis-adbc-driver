# Contract

Date: 2026-11-17. Source: `292ee254`. Status: implementation.
See [intent](intent.md) and [plan](plan.md).

Use GOTOOLCHAIN=auto in the private archive so the explicit base's module selects
its required toolchain. Keep historical default source unchanged. Record the
launcher version separately from the actual source-directory Go version and
preserve binary build info, hashes and all strict build flags.

For local Git archive extraction on Python 3.11, validate every member first:
only regular files/directories, with resolved paths inside the new destination.
Reject links and escaping names. Do not weaken extraction boundaries to bypass
an unsupported keyword argument. Keep build directories new and outside Git.

Validate current pinned dependencies, without Arrow overrides, GC omissions or
checkptr bypasses. Native server-free basic and expanded probes are correctness
checks, not Redis compatibility or performance acceptance. Preserve failures,
including nested/sliced dependency defects and sanitizer initialization failures.

The older-Python extraction requirements in this record are superseded by
[the Python 3.14 correction](../remove-python311-probe-compat/spec.md). Historical
validation data retains its original runtime labels.
