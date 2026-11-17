#!/usr/bin/env python3
"""Server-free C/Arrow lifetime checks, separate from release timings.

The original probe stays available in run.py. These follow-up checks keep producer
callbacks alive explicitly and count both stream and imported-array releases.
"""
import argparse
import concurrent.futures
import ctypes as c
import gc
import hashlib
import json
from pathlib import Path
import resource
import time

import pyarrow as pa
from run import Array, Error, Handle, Probe, Schema, Stream, release


class Producer:
    def __init__(self, batches, stream):
        self.array_releases = 0
        self.stream_releases = 0
        self.array_callbacks = []
        self.stream = stream
        if stream:
            self.value = Stream()
            reader = pa.RecordBatchReader.from_batches(batches[0].schema, batches)
            reader._export_to_c(c.addressof(self.value))
            original_next = c.CFUNCTYPE(c.c_int, c.POINTER(Stream), c.POINTER(Array))(self.value.get_next)
            original_release = c.CFUNCTYPE(None, c.POINTER(Stream))(self.value.release)

            @c.CFUNCTYPE(c.c_int, c.POINTER(Stream), c.POINTER(Array))
            def get_next(ptr, array):
                result = original_next(ptr, array)
                if result == 0 and array.contents.release:
                    self.wrap_array(array.contents)
                return result

            @c.CFUNCTYPE(None, c.POINTER(Stream))
            def close(ptr):
                self.stream_releases += 1
                original_release(ptr)

            self.next_callback = get_next
            self.close_callback = close
            self.value.get_next = c.cast(get_next, c.c_void_p).value
            self.value.release = c.cast(close, c.c_void_p).value
        else:
            assert len(batches) == 1
            self.value, self.schema = Array(), Schema()
            batches[0]._export_to_c(c.addressof(self.value), c.addressof(self.schema))
            self.wrap_array(self.value)

    def wrap_array(self, value):
        original = c.CFUNCTYPE(None, c.POINTER(Array))(value.release)

        @c.CFUNCTYPE(None, c.POINTER(Array))
        def close(ptr):
            self.array_releases += 1
            original(ptr)

        self.array_callbacks.append(close)
        value.release = c.cast(close, c.c_void_p).value

    def bind(self, probe, statement):
        if self.stream:
            probe.call('RedisStatementBindStream', c.byref(statement), c.byref(self.value))
        else:
            probe.call('RedisStatementBind', c.byref(statement), c.byref(self.value), c.byref(self.schema))
        assert not self.value.release

    def assert_released(self, arrays):
        assert self.array_releases == arrays, (self.array_releases, arrays)
        assert self.stream_releases == int(self.stream), self.stream_releases


def batch(seed):
    # Explicit zero bytes, NULLs, exact int64, offsets and nested buffers.
    text = 'text' + chr(0) + 'é'
    binary = bytes([0, 255, seed % 256])
    assert 0 in text.encode() and 0 in binary
    source = pa.record_batch([
        pa.array([0, 9007199254740993 + seed, None, -9007199254740993], pa.int64()),
        pa.array(['prefix', text, None, ''], pa.large_string()),
        pa.array([b'prefix', binary, None, b''], pa.binary()),
        pa.array([[0], [seed, None], None, []], pa.list_(pa.int64())),
        pa.array([{'x': 0}, {'x': seed}, None, {'x': None}], pa.struct([('x', pa.int64())])),
    ], names=['id', 'text', 'binary', 'list', 'struct'])
    return source.slice(1, 3)


def retained_rebind(probe, stream, seed):
    statement = probe.new(2)
    first = Producer([batch(seed)], stream)
    second = Producer([batch(seed + 1)], stream)
    first.bind(probe, statement)
    output = probe.execute(statement)
    with pa.RecordBatchReader._import_from_c(c.addressof(output)) as reader:
        retained = reader.read_next_batch()
    second.bind(probe, statement)
    assert first.stream_releases == int(stream)
    assert first.array_releases == 0, 'retained consumer lost foreign buffers'
    probe.close(2, statement)
    second.assert_released(0 if stream else 1)
    expected = batch(seed)
    assert retained.equals(expected, check_metadata=False), (seed, stream, retained.to_pydict(), expected.to_pydict(), str(retained.schema), str(expected.schema))
    del retained
    first.assert_released(1)


def early_release(probe, stream):
    statement = probe.new(2)
    source = Producer([batch(0)], stream)
    source.bind(probe, statement)
    output = probe.execute(statement)
    release(output)  # No get_next; foreign stream input remains unconsumed.
    probe.close(2, statement)
    source.assert_released(0 if stream else 1)


def error_close(probe, stream):
    statement = probe.new(3)
    source = Producer([batch(0)], stream)
    source.bind(probe, statement)
    message, state = probe.close(3, statement, expected=10)
    assert message == '[redis] intentional fixture close error' and state == 'HY000'
    source.assert_released(0 if stream else 1)


def error_text(probe):
    fn = probe.lib.RedisFFIProbeError
    fn.argtypes = [c.c_int, c.POINTER(Error)]
    fn.restype = c.c_uint8
    for kind in range(4):
        err = Error()
        status = fn(kind, c.byref(err))
        assert status == (1 if kind == 0 else 7)
        assert c.string_at(err.message).decode() == '[redis] literal 100% complete: %s %d %!'
        if kind:
            assert bytes(err.sqlstate) == b'22000'
        if kind == 2:
            assert err.vendor_code == 27
        release(err)
        assert not err.release and not err.message


def poison_input(probe):
    libc = c.CDLL(None)
    libc.malloc.argtypes = [c.c_size_t]
    libc.malloc.restype = c.c_void_p
    libc.free.argtypes = [c.c_void_p]
    bitmap = libc.malloc(1)
    values = libc.malloc(3 * c.sizeof(c.c_int64))
    assert bitmap and values
    c.memset(bitmap, 5, 1)
    exact = (9007199254740993, 0, -9007199254740993)
    for i, value in enumerate(exact):
        c.cast(values, c.POINTER(c.c_int64))[i] = value
    counts = {'array': 0, 'schema': 0, 'child_array': 0, 'child_schema': 0}

    @c.CFUNCTYPE(None, c.POINTER(Array))
    def close_child_array(ptr):
        counts['child_array'] += 1
        assert counts['child_array'] == 1
        c.memset(values, 0xDD, 3 * c.sizeof(c.c_int64))
        c.memset(bitmap, 0xDD, 1)
        libc.free(values)
        libc.free(bitmap)
        ptr.contents.release = None

    @c.CFUNCTYPE(None, c.POINTER(Schema))
    def close_child_schema(ptr):
        counts['child_schema'] += 1
        ptr.contents.release = None

    buffers = (c.c_void_p * 2)(bitmap, values)
    child = Array(length=3, null_count=1, n_buffers=2, buffers=buffers,
                  release=c.cast(close_child_array, c.c_void_p).value)
    child_schema = Schema(format=b'l', name=b'poison', flags=2,
                          release=c.cast(close_child_schema, c.c_void_p).value)

    @c.CFUNCTYPE(None, c.POINTER(Array))
    def close_array(ptr):
        counts['array'] += 1
        release(child)
        ptr.contents.release = None

    @c.CFUNCTYPE(None, c.POINTER(Schema))
    def close_schema(ptr):
        counts['schema'] += 1
        release(child_schema)
        ptr.contents.release = None

    children = (c.POINTER(Array) * 1)(c.pointer(child))
    schemas = (c.POINTER(Schema) * 1)(c.pointer(child_schema))
    root_buffers = (c.c_void_p * 1)(None)
    source = Array(length=3, null_count=0, n_buffers=1, buffers=root_buffers,
                   n_children=1, children=children, release=c.cast(close_array, c.c_void_p).value)
    schema = Schema(format=b'+s', n_children=1, children=schemas,
                    release=c.cast(close_schema, c.c_void_p).value)
    statement = probe.new(2)
    probe.call('RedisStatementBind', c.byref(statement), c.byref(source), c.byref(schema))
    assert counts == {'array': 0, 'schema': 1, 'child_array': 0, 'child_schema': 1}
    output = probe.execute(statement)
    with pa.RecordBatchReader._import_from_c(c.addressof(output)) as reader:
        retained = reader.read_next_batch()
    probe.close(2, statement)
    assert counts['array'] == 0
    assert retained.to_pydict() == {'poison': [exact[0], None, exact[2]]}
    del retained
    assert counts == {'array': 1, 'schema': 1, 'child_array': 1, 'child_schema': 1}


def cancellation(probe):
    probe.lib.RedisStatementCancel.argtypes = [c.POINTER(Handle), c.POINTER(Error)]
    probe.lib.RedisStatementCancel.restype = c.c_uint8
    statement = probe.new(4)

    def execute():
        output, affected = Stream(), c.c_int64()
        result = probe.call('RedisStatementExecuteQuery', c.byref(statement), c.byref(output),
                            c.byref(affected), expected=11)
        assert not output.release and not output.private_data
        return result

    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        pending = pool.submit(execute)
        deadline = time.monotonic() + 10
        while probe.stat(6) == 0:
            if pending.done():
                pending.result()
                raise AssertionError('Blocking execution returned before cancellation')
            if time.monotonic() >= deadline:
                # Cancel once even on failed entry observation; never leave an
                # intentionally blocked callback parked during executor teardown.
                probe.call('RedisStatementCancel', c.byref(statement))
                raise TimeoutError('C-ABI execution did not enter the fixture')
            time.sleep(.001)
        probe.call('RedisStatementCancel', c.byref(statement))
        message, state = pending.result(timeout=10)
    assert message == '[redis] intentional fixture cancellation' and state == '57014'
    assert probe.stat(6) == 0
    probe.close(2, statement)
    probe.close(2, statement, expected=6)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--library', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--clients', type=int, default=4)
    parser.add_argument('--iterations', type=int, default=64)
    args = parser.parse_args()
    if not 1 <= args.clients <= 16 or not 1 <= args.iterations <= 10000:
        parser.error('Use 1..16 clients and 1..10000 iterations')
    probe = Probe(args.library)
    checks = probe.correctness()
    error_text(probe)
    checks.append('literal_percent_error_text_status_sqlstate_all_forwarding_branches')
    poison_input(probe)
    checks.append('retained_foreign_malloc_buffers_poisoned_freed_once_after_consumer')
    cancellation(probe)
    checks.append('blocking_c_abi_cancellation_status_sqlstate_and_close')
    for stream in (False, True):
        retained_rebind(probe, stream, 0)
        early_release(probe, stream)
        error_close(probe, stream)
        checks.append(('stream' if stream else 'array') + '_retained_rebind_early_error_release')
    before = [probe.stat(i) for i in range(4)]
    rss_before = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    started = time.monotonic()

    def client(worker):
        for iteration in range(args.iterations):
            retained_rebind(probe, bool(iteration % 2), worker * args.iterations + iteration)
        return args.iterations

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as pool:
        completed = sum(pool.map(client, range(args.clients)))
    # Collect Python wrappers only after all actual release-count assertions.
    gc.collect()
    after = [probe.stat(i) for i in range(4)]
    assert after[2] - before[2] == completed and after[3] == 0
    result = dict(schema_version=1, scope='Server-free stub C/ctypes + PyArrow ownership follow-up; no Redis or performance acceptance.',
                  library_sha256=hashlib.sha256(args.library.read_bytes()).hexdigest(),
                  harness_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  checks=checks, independent_clients=args.clients, iterations_per_client=args.iterations,
                  completed=completed, owner_close_deltas=[b-a for a,b in zip(before[:3],after[:3])],
                  checked_allocator_bytes=after[3], elapsed_seconds=time.monotonic()-started,
                  process_maxrss_before=rss_before, process_maxrss_after=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,
                  maxrss_unit='bytes on Darwin, KiB on Linux; lifetime maximum, not retained-heap accounting')
    args.output.write_text(json.dumps(result, indent=2)+'\n')
    print(json.dumps(dict(checks=len(checks), completed=completed, checked_allocator_bytes=after[3])))


if __name__ == '__main__':
    main()
