"""Offline admission checks for the owned Software recovery orchestrator."""

import io
import contextlib
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import snapshot_software_cluster as cluster


class RecoveryArtifactTests(unittest.TestCase):
    def test_private_attempt_archives_are_distinct_and_restrictive(self):
        with tempfile.TemporaryDirectory() as temporary:
            def copy(*args, **kwargs):
                destination = Path(args[-1])
                (destination / "nested").mkdir(mode=0o755)
                record = destination / "nested" / "metadata-proof-1.json"
                record.write_text('{"private_identity":"synthetic"}')
                record.chmod(0o644)
                return ""
            with patch.object(cluster, "docker", side_effect=copy):
                first = cluster.archive_recovery_artifacts(Path(temporary), "restart", "client", "/private-attempt")
                second = cluster.archive_recovery_artifacts(Path(temporary), "restart", "client", "/private-attempt")
            self.assertNotEqual(first, second)
            for archive in (first, second):
                self.assertEqual(archive.stat().st_mode & 0o777, 0o700)
                self.assertEqual((archive / "nested").stat().st_mode & 0o777, 0o700)
                self.assertEqual((archive / "nested" / "metadata-proof-1.json").stat().st_mode & 0o777, 0o600)

    def test_capture_rejects_symlinks_without_changing_target(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            outside = root / "outside"
            outside.write_text("synthetic")
            outside.chmod(0o640)
            def copy(*args, **kwargs):
                (Path(args[-1]) / "link").symlink_to(outside)
                return ""
            with patch.object(cluster, "docker", side_effect=copy):
                with self.assertRaisesRegex(ValueError, "unexpected symlink"):
                    cluster.archive_recovery_artifacts(root, "restart", "client", "/private-attempt")
            self.assertEqual(outside.stat().st_mode & 0o777, 0o640)

    def test_missing_artifacts_do_not_replace_original_failure_or_pass(self):
        with contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(SystemExit) as failure:
                cluster.finish_recovery("restart", 7, False)
            self.assertEqual(failure.exception.code, 7)
            with self.assertRaisesRegex(RuntimeError, "artifacts are unavailable"):
                cluster.finish_recovery("restart", 0, False)
            cluster.finish_recovery("restart", 0, True)


def checkpoint(second, **overrides):
    fields = dict(aof_enabled=0, rdb_bgsave_in_progress=0,
                  rdb_last_bgsave_status="ok", rdb_last_save_time=second)
    fields.update(overrides)
    return [{"redis_info": {"persistence": fields}}]


class RDBCheckpointTests(unittest.TestCase):
    def test_checkpoint_must_postdate_acknowledged_probe_writes(self):
        # A save at second 101 precedes the final write at 101.9, even though
        # it is newer than the start of this operation at 100.1.
        clock = [100.1]
        reported = [101]

        def now():
            value = clock[0]
            if value == 100.1:
                clock[0] = 101.1
            return value

        def writes(*args, **kwargs):
            clock[0] = 101.9
            return SimpleNamespace(returncode=0)

        def wait(check, *args, **kwargs):
            self.assertFalse(check(), "accepted a checkpoint preceding the writes")
            reported[0] = 102
            self.assertTrue(check(), "rejected a later completed checkpoint")

        with patch.object(cluster.time, "time", side_effect=now), \
                patch.object(cluster.subprocess, "run", side_effect=writes), \
                patch.object(cluster, "rdb_checkpoint_shards",
                             side_effect=lambda *args: checkpoint(reported[0])), \
                patch.object(cluster, "wait", side_effect=wait):
            cluster.checkpoint_rdb({}, None, ["docker", "exec", "client", "test"], io.StringIO())

    def test_clean_shards_get_progress_writes_without_replacing_probes(self):
        # A periodic save can finish in the probe second, then a clean shard
        # has no reason to produce the strictly later checkpoint we require.
        reported = [100]
        commands = []

        def writes(command, **kwargs):
            commands.append(command[-1])
            if command[-1] == "^TestSnapshotClusterRDBCheckpointProgress$":
                reported[0] = 102
            return SimpleNamespace(returncode=0)

        def wait(check, *args, **kwargs):
            self.assertFalse(check())
            self.assertTrue(check(), "clean shard never creates a later checkpoint")

        with patch.object(cluster.time, "time", side_effect=[100.5, 102.0]), \
                patch.object(cluster.subprocess, "run", side_effect=writes), \
                patch.object(cluster, "rdb_checkpoint_shards",
                             side_effect=lambda *args: checkpoint(reported[0])), \
                patch.object(cluster, "wait", side_effect=wait):
            cluster.checkpoint_rdb({}, None, ["docker", "exec", "client", "test"], io.StringIO())
        self.assertEqual(commands, ["^TestSnapshotClusterRDBCheckpointWrites$",
                                    "^TestSnapshotClusterRDBCheckpointProgress$"])

    def test_failed_progress_command_is_not_replayed_by_readiness_wait(self):
        commands = []

        def writes(command, **kwargs):
            commands.append(command[-1])
            return SimpleNamespace(returncode=int(len(commands) > 1))

        with patch.object(cluster.time, "time", side_effect=[100.5, 102.0]), \
                patch.object(cluster.time, "monotonic", side_effect=[0, 1, 100]), \
                patch.object(cluster.subprocess, "run", side_effect=writes), \
                patch.object(cluster, "rdb_checkpoint_shards", return_value=checkpoint(100)):
            with self.assertRaisesRegex(ValueError, "checkpoint progress writes failed"):
                cluster.checkpoint_rdb({}, None, ["docker", "exec", "client", "test"], io.StringIO())
        self.assertEqual(commands, ["^TestSnapshotClusterRDBCheckpointWrites$",
                                    "^TestSnapshotClusterRDBCheckpointProgress$"])

    def test_progress_timeout_does_not_expose_command_environment(self):
        commands = []

        def writes(command, **kwargs):
            commands.append(command[-1])
            if len(commands) > 1:
                raise cluster.subprocess.TimeoutExpired(command, 15)
            return SimpleNamespace(returncode=0)

        with patch.object(cluster.time, "time", side_effect=[100.5, 102.0]), \
                patch.object(cluster.time, "monotonic", side_effect=[0, 1, 100]), \
                patch.object(cluster.subprocess, "run", side_effect=writes), \
                patch.object(cluster, "rdb_checkpoint_shards", return_value=checkpoint(100)):
            with self.assertRaisesRegex(ValueError, "progress outcome unavailable") as caught:
                cluster.checkpoint_rdb({}, None, ["docker", "exec", "private-credential", "test"], io.StringIO())
        self.assertNotIn("private-credential", str(caught.exception))
        self.assertEqual(len(commands), 2)

    def test_only_completed_successful_rdb_without_aof_is_admitted(self):
        self.assertTrue(cluster.rdb_checkpoint_ready(checkpoint(102), 102))
        for shards in (None, [], checkpoint(101), checkpoint(102, aof_enabled=1),
                       checkpoint(102, rdb_bgsave_in_progress=1),
                       checkpoint(102, rdb_last_bgsave_status="err"),
                       [{"redis_info": {"persistence": {}}}]):
            with self.subTest(shards=shards):
                self.assertFalse(cluster.rdb_checkpoint_ready(shards, 102))


if __name__ == "__main__":
    unittest.main()
