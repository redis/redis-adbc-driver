# Specification

Date: 2026-11-04. Status: implemented; hardware validation outstanding.
See [intent](intent.md) and [plan](plan.md).

The [guide](../../../gpu-execution.md) defines the physical support table,
future stages, ownership, device identity, metadata, cancellation and limits.
Formats u/z use signed 32-bit offsets, U/Z signed 64-bit offsets. Validate and
normalize selected offsets and bitmap before copying selected bytes. Recursive
schemas/dictionaries stay unsupported. All allocations/copies share the bounded
batch budget. A descriptor cannot prove its buffer capacity; the normal Arrow
producer contract still applies.

V1 Python adapter discovers standalone symbols and uses public CPU manager
capsule binding. No manager internals or invented standard extension slots.
Returned device capsules own independent synchronous CUDA copies. Borrowed
producer events are polled with cancellation; cancellation does not stop producer
work. CPU import needs no CUDA package. Unsupported versions/extensions have
explicit CPU-path errors.
