# Layout contracts and acceptance

See [intent](intent.md), [verification](plan.md), and the
[public contract](../../../arrow-interoperability.md#layout-assessment-and-decoded-input-limits).

- Reject Map/dense Union/sparse Union uniformly before schema creation, including
  valid, selected-child NULL, empty and sliced inputs. Do not infer mutable
  storage semantics or stringify unsupported data.
- Normalize FixedSizeList, ListView/LargeListView and signed 16/32/64-bit run-end
  input recursively. Preserve logical order, exact canonical schemas, parent
  NULL/empty/NULL-child distinctions and nested shape. Validate dimensions and
  run boundaries before indexing data. C run-end parents can have zero buffers.
- For a batch containing those layouts, preflight every input column against
  2^20 decoded values and 64 MiB decoded string/binary payload. Count repeated
  logical occurrences and nested values, not merely physical buffer sizes.
  Reject with InvalidArgument before row allocation/writes from that batch;
  preserve autocommit/earlier-batch behavior. This is not a process-memory cap.
- Verify supported data through C Bind/BindStream, create/append, released input
  buffers, separate-connection metadata reload, IPC stream/file, live partition
  replay and independent retained replay after truncating the source.
- Cover safe malformed offsets/sizes/run boundaries and compact valid inputs
  whose logical expansion exceeds the limits, without huge allocations.
- Native input layout preservation, Map/Union storage and extension support
  are not added. Existing selected-result-layout partition guards remain.
