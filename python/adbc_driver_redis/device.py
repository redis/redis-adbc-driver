"""Version 1 Arrow C Device adapter using explicit, synchronous host staging.

Importing this module requires neither PyArrow nor any CUDA Python package.
Manager handles are used only via their public ``bind`` method.
"""

import ctypes as c
import errno
import time
from concurrent.futures import CancelledError


class _Schema(c.Structure):
    pass


class _Array(c.Structure):
    pass


_Schema._fields_ = [
    ("format", c.c_char_p), ("name", c.c_char_p), ("metadata", c.c_void_p),
    ("flags", c.c_int64), ("n_children", c.c_int64),
    ("children", c.POINTER(c.POINTER(_Schema))), ("dictionary", c.POINTER(_Schema)),
    ("release", c.c_void_p), ("private_data", c.c_void_p),
]
_Array._fields_ = [
    ("length", c.c_int64), ("null_count", c.c_int64), ("offset", c.c_int64),
    ("n_buffers", c.c_int64), ("n_children", c.c_int64),
    ("buffers", c.POINTER(c.c_void_p)), ("children", c.POINTER(c.POINTER(_Array))),
    ("dictionary", c.POINTER(_Array)), ("release", c.c_void_p), ("private_data", c.c_void_p),
]


class _DeviceArray(c.Structure):
    _fields_ = [("array", _Array), ("device_id", c.c_int64), ("device_type", c.c_int32),
                ("sync_event", c.c_void_p), ("reserved", c.c_int64 * 3)]


_capsule_pointer = c.pythonapi.PyCapsule_GetPointer
_capsule_pointer.argtypes = [c.py_object, c.c_char_p]
_capsule_pointer.restype = c.c_void_p
_capsule_new = c.pythonapi.PyCapsule_New
_capsule_new.argtypes = [c.c_void_p, c.c_char_p, c.c_void_p]
_capsule_new.restype = c.py_object


def _release(value):
    if value.release:
        c.CFUNCTYPE(None, c.POINTER(type(value)))(value.release)(c.byref(value))


# A capsule owns its base descriptor, including after the producer is deleted.
# Consumers move release callbacks and set the source callback to NULL. Capsule
# destruction therefore releases only descriptors that were not consumed.
_exports = {}
_raw_pointer = c.PYFUNCTYPE(c.c_void_p, c.c_void_p, c.c_char_p)(
    ("PyCapsule_GetPointer", c.pythonapi))


@c.CFUNCTYPE(None, c.c_void_p)
def _schema_destructor(capsule, owners=_exports, pointer=_raw_pointer, release=_release):
    value = owners.pop(pointer(capsule, b"arrow_schema"), None)
    if value is not None:
        release(value[0])


@c.CFUNCTYPE(None, c.c_void_p)
def _device_destructor(capsule, owners=_exports, pointer=_raw_pointer, release=_release):
    value = owners.pop(pointer(capsule, b"arrow_device_array"), None)
    if value is not None:
        release(value[0].array)


def _own_capsule(value, name, destructor, library):
    address = c.addressof(value)
    _exports[address] = (value, library)
    try:
        return _capsule_new(address, name, c.cast(destructor, c.c_void_p))
    except BaseException:
        del _exports[address]
        raise


def _check_cancel(cancel):
    if cancel is not None and cancel.is_set():
        raise CancelledError("Arrow device staging cancelled")


class DeviceAdapterV1:
    """Copy Arrow device capsules to/from CPU Arrow for ordinary ADBC APIs.

    ``cancel`` arguments accept a threading.Event. Cancellation polls producer
    readiness and checks around synchronous copies; it cannot preempt a copy or
    cancel producer-owned work. Keep the producer alive until the call returns.
    """

    api_version = 1

    def __init__(self, library=None):
        from . import _driver_path
        try:
            import pyarrow as pa
        except ImportError as exc:
            raise ImportError("DeviceAdapterV1 needs PyArrow; install adbc-driver-redis[dbapi]") from exc

        from importlib.metadata import version as package_version
        manager_version = tuple(int(part) for part in package_version("adbc-driver-manager").split(".")[:2])
        arrow_version = int(pa.__version__.split(".")[0])
        if not ((1, 8) <= manager_version < (2, 0)) or not 14 <= arrow_version < 27:
            raise NotImplementedError("V1 adapter requires adbc-driver-manager 1.8..1.x and PyArrow 14..26; use ordinary CPU Arrow APIs")
        self._pa = pa
        self._lib = c.CDLL(library or _driver_path())
        try:
            version = self._lib.RedisDeviceInterfaceVersion
            version.argtypes, version.restype = [], c.c_int
            if version() != 1:
                raise NotImplementedError("unsupported Redis C Device interface version; use CPU Arrow staging")
            signatures = {
                "RedisDeviceArrayToHost": [c.POINTER(_DeviceArray), c.POINTER(_Schema), c.POINTER(_Array), c.c_char_p, c.c_size_t],
                "RedisDeviceArrayFromHost": [c.POINTER(_Array), c.POINTER(_Schema), c.c_int64, c.POINTER(_DeviceArray), c.c_char_p, c.c_size_t],
                "RedisDeviceSchemaBorrow": [c.POINTER(_Schema), c.POINTER(_Schema), c.c_char_p, c.c_size_t],
                "RedisDeviceArrayEventReady": [c.POINTER(_DeviceArray), c.c_char_p, c.c_size_t],
            }
            for name, args in signatures.items():
                function = getattr(self._lib, name)
                function.argtypes, function.restype = args, c.c_int
        except AttributeError as exc:
            raise NotImplementedError("Redis driver has no V1 C Device adapter; upgrade the library or use CPU Arrow APIs") from exc

    @staticmethod
    def _check(rc, message):
        if rc:
            raise OSError(rc, message.value.decode("utf-8", "replace") + "; use CPU Arrow APIs if CUDA is unavailable")

    def to_host(self, producer, *, device_type, device_id, cancel=None):
        """Borrow fresh Arrow device capsules and return an independent CPU array/batch.

        Device type must be 1 (CPU) or 2 (CUDA). Device ID is checked against the
        descriptor; CUDA IDs are ordinals 0..65535. No input capsule is consumed.
        """
        _check_cancel(cancel)
        if device_type not in (1, 2) or not isinstance(device_id, int):
            raise ValueError("specify device_type=1 (CPU) or 2 (CUDA) and an integer device_id")
        if device_type == 2 and not 0 <= device_id <= 65535:
            raise ValueError("CUDA device_id must be 0..65535")
        capsules = producer.__arrow_c_device_array__()
        if not isinstance(capsules, tuple) or len(capsules) != 2:
            raise TypeError("producer must return (arrow_schema, arrow_device_array) capsules")
        schema = c.cast(_capsule_pointer(capsules[0], b"arrow_schema"), c.POINTER(_Schema))
        device = c.cast(_capsule_pointer(capsules[1], b"arrow_device_array"), c.POINTER(_DeviceArray))
        if (device.contents.device_type, device.contents.device_id) != (device_type, device_id):
            raise ValueError("producer descriptor does not match the requested device type/ID")
        host, view, message = _Array(), _Schema(), c.create_string_buffer(512)
        try:
            self._check(self._lib.RedisDeviceSchemaBorrow(schema, c.byref(view), message, len(message)), message)
            while True:
                _check_cancel(cancel)
                rc = self._lib.RedisDeviceArrayEventReady(device, message, len(message))
                if rc != errno.EAGAIN:
                    self._check(rc, message)
                    break
                time.sleep(0.001)
            self._check(self._lib.RedisDeviceArrayToHost(device, schema, c.byref(host), message, len(message)), message)
            _check_cancel(cancel)
            importer = self._pa.RecordBatch if view.format == b"+s" else self._pa.Array
            return importer._import_from_c(c.addressof(host), c.addressof(view))
        finally:
            _release(host)
            _release(view)

    def bind(self, statement, producer, *, device_type, device_id, cancel=None, column_names=None):
        """Stage a record batch and bind via the manager's public CPU capsule API.

        The producer may be released immediately on return, before execution.
        Cancel execution itself using the manager statement's ``cancel()``.
        ``column_names`` supplies explicit names for producers (such as a raw
        pylibcudf.Table) whose capsule schema does not retain application names.
        """
        batch = self.to_host(producer, device_type=device_type, device_id=device_id, cancel=cancel)
        if not isinstance(batch, self._pa.RecordBatch):
            raise TypeError("ADBC bind requires a record batch struct producer")
        if column_names is not None:
            names = list(column_names)
            if (len(names) != batch.num_columns or
                    any(not isinstance(name, str) or not name for name in names) or
                    len(set(names)) != len(names)):
                raise ValueError("column_names must contain one distinct nonempty string per column")
            batch = batch.rename_columns(names)
        _check_cancel(cancel)
        schema, array = batch.__arrow_c_array__()
        statement.bind(array, schema)

    def to_device(self, host, *, device_id):
        """Return a reusable device capsule producer; each export copies to CUDA.

        The returned producer retains CPU input. Each exported capsule owns its
        CUDA allocations independently. Missing CUDA fails when exporting.
        """
        if not isinstance(device_id, int) or not 0 <= device_id <= 65535:
            raise ValueError("CUDA device_id must be 0..65535")
        if not isinstance(host, (self._pa.Array, self._pa.RecordBatch)):
            raise TypeError("to_device expects a PyArrow Array or RecordBatch")
        return _DeviceExport(self, host, device_id)


class _DeviceExport:
    def __init__(self, adapter, host, device_id):
        self._adapter, self._host, self._device_id = adapter, host, device_id

    def __arrow_c_device_array__(self, requested_schema=None):
        if requested_schema is not None:
            raise NotImplementedError("device adapter does not support requested schema conversion")
        array, schema, device = _Array(), _Schema(), _DeviceArray()
        message = c.create_string_buffer(512)
        self._host._export_to_c(c.addressof(array), c.addressof(schema))
        try:
            lib = self._adapter._lib
            self._adapter._check(lib.RedisDeviceArrayFromHost(c.byref(array), c.byref(schema), self._device_id,
                c.byref(device), message, len(message)), message)
            schema_capsule = _own_capsule(schema, b"arrow_schema", _schema_destructor, lib)
            try:
                device_capsule = _own_capsule(device, b"arrow_device_array", _device_destructor, lib)
            except BaseException:
                _release(device.array)
                raise
            return schema_capsule, device_capsule
        except BaseException:
            _release(schema)
            _release(device.array)
            raise
        finally:
            _release(array)
