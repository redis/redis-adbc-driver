"""Capabilities excluded by the public Redis analytics dialect contract."""

from sqlalchemy.testing import exclusions
from sqlalchemy.testing.requirements import SuiteRequirements


class Requirements(SuiteRequirements):
    @property
    def autoincrement_insert(self):
        return exclusions.closed("Redis ADBC requires explicit primary-key values")

    @property
    def index_reflection(self):
        return exclusions.closed("Search indexes are physical table indexes, not CREATE INDEX objects")

    @property
    def foreign_keys(self):
        return exclusions.closed("Redis ADBC does not enforce FOREIGN KEY constraints")

    @property
    def foreign_key_constraint_reflection(self):
        return exclusions.closed("FOREIGN KEY declarations are not persisted")

    @property
    def unique_constraint_reflection(self):
        return exclusions.closed("UNIQUE declarations are not enforced or persisted")

    @property
    def duplicate_key_raises_integrity_error(self):
        return exclusions.closed("PRIMARY KEY metadata enforces NOT NULL, not uniqueness")

    @property
    def autoincrement_without_sequence(self):
        return self.autoincrement_insert

    @property
    def supports_distinct_on(self):
        return exclusions.open()

    @property
    def reflect_table_options(self):
        return exclusions.open()

    @property
    def reflects_pk_names(self):
        return exclusions.open()

    @property
    def temp_table_reflect_indexes(self):
        return self.index_reflection

    @property
    def temp_table_names(self):
        return exclusions.open()

    @property
    def has_temp_table(self):
        return exclusions.open()

    @property
    def views(self):
        return exclusions.open()

    @property
    def temporary_views(self):
        return exclusions.open()
