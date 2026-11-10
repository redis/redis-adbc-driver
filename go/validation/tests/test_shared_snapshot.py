# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.

"""Run the shared-view contract against each selected native/release library."""

from uuid import uuid4

from shared_snapshot_validation import validate


def test_shared_snapshot_full_refresh(driver_path, db_kwargs):
    # driver_path selects the built library or the installed wheel in release
    # validation. Never resolve a different source library inside this test.
    validate(driver_path, db_kwargs)


def test_shared_snapshot_full_refresh_with_multiple_schemas(driver_path, db_kwargs, conn):
    # Reproduce concurrent GetObjects schema enumeration: metadata order may
    # change without changing the pinned catalog. Keep complete field checks.
    schemas = ["shared_metadata_" + uuid4().hex for _ in range(8)]
    try:
        with conn.cursor() as cursor:
            for schema in schemas:
                cursor.execute(f'CREATE SCHEMA "{schema}"')
        validate(driver_path, db_kwargs)
    finally:
        with conn.cursor() as cursor:
            for schema in schemas:
                cursor.execute(f'DROP SCHEMA IF EXISTS "{schema}"')
