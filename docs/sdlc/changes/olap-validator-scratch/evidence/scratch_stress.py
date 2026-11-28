"""Validator scratch pattern: write, close, reopen by path, read back.

Usage: python -I scratch_stress.py DIR SECONDS LABEL [unsynced|fsync|same_handle] [DELAY_SECONDS]
Each iteration writes two files of the Q72/Q78 scratch sizes in a fresh
directory, as one validate_files call does, then reads both back. A read that
differs is classified (empty, zeros, short, different) and immediately re-read.
"same_handle" reads through the still-open w+b handle instead of reopening.
DELAY_SECONDS waits between writing both files and reading them back.
"""
import hashlib, json, os, shutil, sys, tempfile, time

root, seconds, label = sys.argv[1], float(sys.argv[2]), sys.argv[3]
mode = sys.argv[4] if len(sys.argv) > 4 else "unsynced"
delay = float(sys.argv[5]) if len(sys.argv) > 5 else 0.0
data = os.urandom(20839)
other = os.urandom(20280)
counts = {"iterations": 0, "empty": 0, "zeros": 0, "short": 0, "different": 0}
examples = []
deadline = time.monotonic() + seconds
while time.monotonic() < deadline:
    tmp = tempfile.mkdtemp(dir=root)
    try:
        a, b = os.path.join(tmp, "a"), os.path.join(tmp, "b")
        os.mkdir(a); os.mkdir(b)
        handles = {}
        for path, payload in ((os.path.join(a, "sort-0"), data), (os.path.join(b, "sort-0"), other)):
            f = open(path, "w+b" if mode == "same_handle" else "wb")
            f.write(payload)
            f.flush()
            if mode == "fsync":
                os.fsync(f.fileno())
            if mode == "same_handle":
                handles[path] = f
            else:
                f.close()
        if delay:
            time.sleep(delay)
        for path, want in ((os.path.join(a, "sort-0"), data), (os.path.join(b, "sort-0"), other)):
            if mode == "same_handle":
                f = handles.pop(path)
                f.seek(0)
                got = f.read()
                f.close()
            else:
                with open(path, "rb") as f:
                    got = f.read()
            if got != want:
                kind = ("empty" if not got else "zeros" if got.count(0) == len(got)
                        else "short" if len(got) < len(want) else "different")
                counts[kind] += 1
                with open(path, "rb") as f:
                    again = f.read()
                if len(examples) < 60:
                    examples.append({"kind": kind, "got": len(got), "reread": len(again),
                                     "reread_zero": again.count(0) == len(again) and bool(again),
                                     "reread_ok": again == want, "at": counts["iterations"]})
    finally:
        shutil.rmtree(tmp)
    counts["iterations"] += 1
print(json.dumps({"label": label, **counts, "examples": examples}))
