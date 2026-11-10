# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.

"""Run the shared-view contract against each selected native/release library."""

from shared_snapshot_validation import validate


def test_shared_snapshot_full_refresh(driver_path, db_kwargs):
    # driver_path selects the built library or the installed wheel in release
    # validation. Never resolve a different source library inside this test.
    validate(driver_path, db_kwargs)
