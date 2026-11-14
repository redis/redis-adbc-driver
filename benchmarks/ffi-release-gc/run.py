#!/usr/bin/env python3
"""Server-free attribution through real ADBC C release entrypoints.

Requires an experimental library built with probe.go.txt overlaid into pkg and
-tags driverlib,ffireleaseprobe. Never loads Redis or changes default driver code.
"""
import argparse
import ctypes as c
import hashlib
import json
from pathlib import Path
import statistics
import time

import pyarrow as pa


class Handle(c.Structure):
    _fields_ = [('private_data', c.c_void_p), ('private_driver', c.c_void_p)]


class Error(c.Structure):
    _fields_ = [('message', c.c_void_p), ('vendor_code', c.c_int32),
                ('sqlstate', c.c_char * 5), ('release', c.c_void_p),
                ('private_data', c.c_void_p), ('private_driver', c.c_void_p)]


class Schema(c.Structure):
    pass


class Array(c.Structure):
    pass


Schema._fields_ = [('format', c.c_char_p), ('name', c.c_char_p), ('metadata', c.c_void_p),
                  ('flags', c.c_int64), ('n_children', c.c_int64),
                  ('children', c.POINTER(c.POINTER(Schema))), ('dictionary', c.POINTER(Schema)),
                  ('release', c.c_void_p), ('private_data', c.c_void_p)]
Array._fields_ = [('length', c.c_int64), ('null_count', c.c_int64), ('offset', c.c_int64),
                 ('n_buffers', c.c_int64), ('n_children', c.c_int64),
                 ('buffers', c.POINTER(c.c_void_p)), ('children', c.POINTER(c.POINTER(Array))),
                 ('dictionary', c.POINTER(Array)), ('release', c.c_void_p), ('private_data', c.c_void_p)]


class Stream(c.Structure):
    _fields_ = [(n, c.c_void_p) for n in ('get_schema', 'get_next', 'get_last_error', 'release', 'private_data')]


def release(value):
    if value.release:
        c.CFUNCTYPE(None, c.POINTER(type(value)))(value.release)(c.byref(value))


class Probe:
    names = ('RedisDatabaseRelease', 'RedisConnectionRelease', 'RedisStatementRelease')

    def __init__(self, path):
        self.lib = c.CDLL(str(path.resolve()))
        self.lib.RedisFFIProbeNew.argtypes = [c.c_int, c.POINTER(Handle)]
        self.lib.RedisFFIProbeNew.restype = None
        self.lib.RedisFFIProbeStat.argtypes = [c.c_int]
        self.lib.RedisFFIProbeStat.restype = c.c_int64
        sigs = {n: [c.POINTER(Handle), c.POINTER(Error)] for n in self.names}
        sigs.update({
            'RedisStatementExecuteQuery': [c.POINTER(Handle), c.POINTER(Stream), c.POINTER(c.c_int64), c.POINTER(Error)],
            'RedisStatementBind': [c.POINTER(Handle), c.POINTER(Array), c.POINTER(Schema), c.POINTER(Error)],
            'RedisStatementBindStream': [c.POINTER(Handle), c.POINTER(Stream), c.POINTER(Error)],
            'RedisErrorFromArrayStream': [c.POINTER(Stream), c.POINTER(c.c_uint8)],
        })
        for name, args in sigs.items():
            f = getattr(self.lib, name)
            f.argtypes, f.restype = args, c.c_uint8
        self.lib.RedisErrorFromArrayStream.restype = c.POINTER(Error)

    def stat(self, kind):
        return self.lib.RedisFFIProbeStat(kind)

    def new(self, kind):
        h = Handle()
        self.lib.RedisFFIProbeNew(kind, c.byref(h))
        assert h.private_data
        return h

    def call(self, name, *args, expected=0):
        err = Error()
        status = getattr(self.lib, name)(*args, c.byref(err))
        message = c.string_at(err.message).decode() if err.message else ''
        sqlstate = bytes(err.sqlstate).decode()
        release(err)
        assert status == expected, (name, status, message, expected)
        return message, sqlstate

    def close(self, kind, h, expected=0):
        result = self.call(self.names[min(kind, 2)], c.byref(h), expected=expected)
        assert not h.private_data
        return result

    def execute(self, h):
        stream, count = Stream(), c.c_int64()
        self.call('RedisStatementExecuteQuery', c.byref(h), c.byref(stream), c.byref(count))
        assert stream.release
        return stream

    def stream(self):
        h = self.new(2)
        stream = self.execute(h)
        self.close(2, h)
        return stream

    def correctness(self):
        checks = []
        for kind in range(3):
            before = self.stat(kind)
            h = self.new(kind)
            self.close(kind, h)
            assert self.stat(kind) == before + 1
            self.close(kind, h, expected=6)
            assert self.stat(kind) == before + 1
            checks.append(f'kind_{kind}_explicit_close_once_invalid_repeat')
        # Exported array retains buffers independently of stream/statement.
        h = self.new(2)
        stream = self.execute(h)
        schema, arr = Schema(), Array()
        get_schema = c.CFUNCTYPE(c.c_int, c.POINTER(Stream), c.POINTER(Schema))(stream.get_schema)
        get_next = c.CFUNCTYPE(c.c_int, c.POINTER(Stream), c.POINTER(Array))(stream.get_next)
        assert get_schema(c.byref(stream), c.byref(schema)) == 0
        assert get_next(c.byref(stream), c.byref(arr)) == 0
        release(stream)
        assert not stream.release and not stream.private_data
        release(stream)  # Arrow stream release callback is idempotent by contract.
        self.close(2, h)
        batch = pa.RecordBatch._import_from_c(c.addressof(arr), c.addressof(schema))
        assert batch.to_pydict() == {'value': [11, None, 37]}
        del batch
        assert self.stat(3) == 0
        checks.append('retained_exported_array_survives_stream_and_statement')
        # Count producer releases at the actual foreign callback, synchronously.
        for as_stream in (False, True):
            h = self.new(2)
            batch = pa.record_batch([pa.array([7, None, 19], type=pa.int64())], names=['x'])
            callbacks = [0]
            if as_stream:
                value = Stream()
                reader = pa.RecordBatchReader.from_batches(batch.schema, [batch])
                reader._export_to_c(c.addressof(value))
                original = c.CFUNCTYPE(None, c.POINTER(Stream))(value.release)
                @c.CFUNCTYPE(None, c.POINTER(Stream))
                def callback(ptr):
                    callbacks[0] += 1
                    original(ptr)
                value.release = c.cast(callback, c.c_void_p).value
                self.call('RedisStatementBindStream', c.byref(h), c.byref(value))
            else:
                value, schema = Array(), Schema()
                batch._export_to_c(c.addressof(value), c.addressof(schema))
                original = c.CFUNCTYPE(None, c.POINTER(Array))(value.release)
                @c.CFUNCTYPE(None, c.POINTER(Array))
                def callback(ptr):
                    callbacks[0] += 1
                    original(ptr)
                value.release = c.cast(callback, c.c_void_p).value
                self.call('RedisStatementBind', c.byref(h), c.byref(value), c.byref(schema))
            assert callbacks == [0]
            out = self.execute(h)
            with pa.RecordBatchReader._import_from_c(c.addressof(out)) as output:
                table = output.read_all()
            assert table.to_pydict() == {'x': [7, None, 19]}
            self.close(2, h)
            # Stream can release at Close, but retained output tables keep arrays.
            if as_stream:
                assert callbacks == [1]
            del table
            assert callbacks == [1]
            checks.append('bound_stream_synchronous_producer_release' if as_stream else 'bound_array_synchronous_producer_release')
        h = self.new(3)
        stream = self.execute(h)
        arr = Array()
        get_next = c.CFUNCTYPE(c.c_int, c.POINTER(Stream), c.POINTER(Array))(stream.get_next)
        assert get_next(c.byref(stream), c.byref(arr)) == 0
        release(arr)
        assert get_next(c.byref(stream), c.byref(arr)) != 0
        status = c.c_uint8()
        error = self.lib.RedisErrorFromArrayStream(c.byref(stream), c.byref(status))
        assert status.value == 7 and error
        assert bytes(error.contents.sqlstate) == b'22000'
        assert 'intentional fixture stream error' in c.string_at(error.contents.message).decode()
        release(stream)  # Owns/frees its error; caller must not release borrowed error.
        message, state = self.close(3, h, expected=10)
        assert 'intentional fixture close error' in message and state == 'HY000'
        assert self.stat(3) == 0
        checks.append('stream_and_close_errors_preserve_sqlstate_and_release')
        return checks

    def measure(self, iterations, repeats):
        samples = []
        for case in ('database', 'connection', 'statement', 'stream'):
            kind = ('database', 'connection', 'statement', 'stream').index(case)
            for sample in range(repeats + 1):
                # Setup and NumGC/ReadMemStats are outside the timed section.
                handles = [self.stream() if kind == 3 else self.new(kind) for _ in range(iterations)]
                fn = release if kind == 3 else lambda h: self.close(kind, h)
                forced_before = self.stat(5)
                started = time.perf_counter_ns()
                for h in handles:
                    fn(h)
                elapsed = time.perf_counter_ns() - started
                forced_after = self.stat(5)
                assert self.stat(3) == 0
                samples.append(dict(case=case, sample=sample, warmup=sample == 0,
                                    iterations=iterations, elapsed_ns=elapsed,
                                    ns_per_release=elapsed / iterations,
                                    forced_gc_delta=forced_after - forced_before))
        return samples


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--library', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--iterations', type=int, default=128)
    p.add_argument('--repeats', type=int, default=6)
    p.add_argument('--correctness-only', action='store_true')
    args = p.parse_args()
    assert args.iterations > 0 and args.repeats > 0
    probe = Probe(args.library)
    checks = probe.correctness()
    samples = [] if args.correctness_only else probe.measure(args.iterations, args.repeats)
    result = dict(library_sha256=hashlib.sha256(args.library.read_bytes()).hexdigest(),
                  checks=checks, samples=samples, checked_allocator_bytes=probe.stat(3),
                  summary={case: statistics.median(s['ns_per_release'] for s in samples if s['case'] == case and not s['warmup'])
                           for case in {s['case'] for s in samples}})
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(checks=len(checks), samples=len(samples), summary=result['summary'])))


if __name__ == '__main__':
    main()
