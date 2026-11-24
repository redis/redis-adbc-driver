# Intent

Allow generic ADBC consumers forwarding database options to request more than
the historical eight default source partitions without Redis-specific statement
logic. Reuse the existing partition_count option, preserving its bounds/default
and leaving consumer scheduling and internal query-worker budgets separate.
