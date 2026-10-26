"""Fail CI if required native partition cases were skipped or not collected."""
import sys
import xml.etree.ElementTree as ET


def check(path):
    root = ET.parse(path).getroot()
    cases = [case for case in root.iter("testcase")
             if "test_partitions" in case.get("classname", "")]
    if not cases:
        raise ValueError("native partition tests were not collected")
    required = {"test_partitions_cross_process", "test_partition_validation_and_empty",
                "test_partition_connection_cancel", "test_incremental_lifecycle",
                "test_partition_bound_expressions_cross_process",
                "test_balanced_partition_strategy", "test_pruned_partition_values",
                "test_partition_planner_exit_and_worker_death",
                "test_retained_partitions_native_replay_types_and_revoke",
                "test_retained_partitions_native_planner_exit_and_worker_death",
                "test_retained_partitions_native_byte_budget",
                "test_retained_partitions_native_global_sql_equivalence"}
    collected = {case.get("name", "").split("[")[0] for case in cases}
    if missing := required - collected:
        raise ValueError("required native partition cases missing: " + ", ".join(sorted(missing)))
    for case in cases:
        if any(case.find(tag) is not None for tag in ("skipped", "failure", "error")):
            raise ValueError("native partition test did not pass: " + case.get("name", ""))
    print(f"Required native partition tests: {len(cases)} passed")


if __name__ == "__main__":
    check(sys.argv[1])
