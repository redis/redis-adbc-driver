"""Atomic, credential-free stage checkpoints survive a killed trial process."""
import json
from pathlib import Path
import resource
import threading
import time


def progress_path(output):
    return Path(output).with_suffix(".progress.json")


class TrialProgress:
    def __init__(self, output, started, cpu_before, interval=5):
        self.path = progress_path(output)
        self.started, self.cpu_before = started, cpu_before
        self.interval = interval
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.stage = "connect"
        self.stage_started = started
        self.completed = {}
        self.status = "running"
        self.thread = threading.Thread(target=self.sample, daemon=True)

    def begin(self):
        self.write()
        self.thread.start()

    def enter(self, stage, completed):
        with self.lock:
            self.stage, self.stage_started = stage, time.perf_counter()
            # Copy completed stage times; never read the ADBC connection from
            # the sampler while execute is active on that connection.
            self.completed = dict(completed)
        self.write()

    def write(self):
        try:
            with self.lock:
                usage = resource.getrusage(resource.RUSAGE_SELF)
                now = time.perf_counter()
                snapshot = {"status": self.status, "active_stage": self.stage,
                            "stage_seconds": self.completed,
                            "active_stage_elapsed_seconds": now - self.stage_started,
                            "trial_wall_seconds": now - self.started,
                            "process_user_cpu_seconds": usage.ru_utime - self.cpu_before.ru_utime,
                            "process_system_cpu_seconds": usage.ru_stime - self.cpu_before.ru_stime,
                            "process_peak_rss_bytes": usage.ru_maxrss * 1024}
                temp = self.path.with_suffix(self.path.suffix + ".tmp")
                temp.write_text(json.dumps(snapshot) + "\n")
                temp.replace(self.path)
        except OSError:
            # Optional diagnostics must not mask the original query outcome.
            pass

    def sample(self):
        while not self.stop.wait(self.interval):
            self.write()

    def close(self, status, completed):
        self.stop.set()
        self.thread.join()
        with self.lock:
            self.status = status
            self.completed = dict(completed)
        self.write()


def retained_progress(output):
    """Return diagnostics only; partial progress is never a successful result."""
    try:
        value = json.loads(progress_path(output).read_text())
        if value.get("active_stage") not in {"connect", "session_setup", "execute", "arrow_fetch", "output_fingerprint"}:
            return None
        fields = {"status", "active_stage", "stage_seconds", "active_stage_elapsed_seconds", "trial_wall_seconds",
                  "process_user_cpu_seconds", "process_system_cpu_seconds", "process_peak_rss_bytes"}
        return {key: value[key] for key in fields if key in value}
    except (OSError, ValueError, TypeError, AttributeError):
        return None


def annotate_failure(record, output):
    if record.get("error_type"):
        partial = retained_progress(output)
        if partial:
            record["partial_diagnostics"] = partial
    return record
