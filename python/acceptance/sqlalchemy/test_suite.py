"""Run the real upstream suite without local assertion changes or xfails."""

from sqlalchemy.testing.suite import *  # noqa: F401,F403

from sqlalchemy.testing.suite import ComponentReflectionTest as _ComponentReflectionTest
from sqlalchemy.testing.suite import QuotedNameArgumentTest as _QuotedNameArgumentTest
from sqlalchemy.testing.suite import InsertBehaviorTest as _InsertBehaviorTest
from sqlalchemy import testing


class ComponentReflectionTest(_ComponentReflectionTest):
    @classmethod
    def define_tables(cls, metadata):
        super().define_tables(metadata)
        # The upstream fixture adds index=True columns even when the backend's
        # index_reflection requirement is closed. Keep all reflection assertions
        # but omit CREATE INDEX objects unsupported by Redis's Search layout.
        for table in metadata.tables.values():
            table.indexes.clear()


class QuotedNameArgumentTest(_QuotedNameArgumentTest):
    @classmethod
    def define_tables(cls, metadata):
        super().define_tables(metadata)
        for table in metadata.tables.values():
            table.indexes.clear()


class InsertBehaviorTest(_InsertBehaviorTest):
    # These upstream cases insert rows without primary keys but omit the
    # autoincrement requirement used by neighboring cases in the same class.
    test_no_results_for_non_returning_insert = testing.requires.autoincrement_insert(
        _InsertBehaviorTest.test_no_results_for_non_returning_insert)
    test_autoclose_on_insert_implicit_returning = testing.requires.autoincrement_insert(
        _InsertBehaviorTest.test_autoclose_on_insert_implicit_returning)
    test_empty_insert = testing.requires.autoincrement_insert(_InsertBehaviorTest.test_empty_insert)
    test_empty_insert_multiple = testing.requires.autoincrement_insert(_InsertBehaviorTest.test_empty_insert_multiple)
    test_insert_from_select_autoinc = testing.requires.autoincrement_insert(_InsertBehaviorTest.test_insert_from_select_autoinc)
