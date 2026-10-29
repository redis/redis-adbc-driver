# Intent: easier installation and configuration

Date: 2026-10-29. Source: `95d6583` plus this working-tree change.
Requestor: user in this chat; implementation: Codex. Status: implemented locally,
not released or approved for merge. See [spec](spec.md), [plan](plan.md) and
[validation](validation.md).

## Problem and outcomes

Users must choose between Python wheels, native archives, manifests and multiple
tool-specific connection formats. Provide one setup entry point and actionable
diagnostics without exposing connection credentials. Strengthen native wheel
validation before publishing.

## Scope and constraints

Implement the first proposed batch: doctor, setup documentation and publishing
validation. Preserve the install-manifest CLI and wheel format. Native package
manager distribution, new connector implementations, extra supported platforms,
Docker images and actual publication are outside this batch.

The initially suspected macOS publishing failure was disproved: the wheel
builder deliberately stores Mach-O libraries under a .so filename. Preserve that
contract and check binary format/architecture instead of changing the suffix.
