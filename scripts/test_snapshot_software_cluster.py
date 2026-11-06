"""Offline admission checks for the owned Software recovery orchestrator."""

import io
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import snapshot_software_cluster as cluster


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
