Target: sqlalchemy/sqlalchemy — new GitHub Discussion, category "Usage Questions" (bug_report.yaml asks for a Discussion instead of a bug report; blank issues are disabled; "Ideas" is the other offered category)
Title: Third-party dialect suite: InsertBehaviorTest and reflection fixtures aren't gated by autoincrement_insert / index_reflection
Status: optional, low priority. The gaps are confirmed in the 2.1.6 source and on main 9c88919b; not a runnable MCVE.
---
We maintain an external ADBC-based dialect that runs `sqlalchemy.testing.suite`. Our backend has no autoincrement or sequences and no SQL `CREATE INDEX`, so our `Requirements` close `autoincrement_insert` and `index_reflection`. README.dialects.rst says suite tests "should be annotated with specific requirement decorators so that they can be fully controlled". We found two places that aren't gated, and we currently override them locally. Would gating them upstream be welcome?

SQLAlchemy 2.1.6; the same code is on main at `9c88919b`.

1. **`InsertBehaviorTest.test_no_results_for_non_returning_insert`** ([test_insert.py#L136-L161](https://github.com/sqlalchemy/sqlalchemy/blob/9c88919beb770ff0b14deab7e914ff7aaa5c4bd2/lib/sqlalchemy/testing/suite/test_insert.py#L136-L161)) inserts `{"data": …}` into `no_implicit_returning`.
   - That table's PK has `test_needs_autoincrement=True` ([L112-L119](https://github.com/sqlalchemy/sqlalchemy/blob/9c88919beb770ff0b14deab7e914ff7aaa5c4bd2/lib/sqlalchemy/testing/suite/test_insert.py#L112-L119)), but the test has no requirement decorator. With `autoincrement_insert` closed, the insert errors instead of skipping.
   - The neighbouring `test_autoclose_on_insert` does use `@requirements.autoincrement_insert`.
   - Less clearly, `test_empty_insert`, `test_empty_insert_multiple`, `test_insert_from_select_autoinc` and `test_autoclose_on_insert_implicit_returning` also insert into autoincrement tables under other requirements only.
2. **Unconditional reflection indexes.** `ComponentReflectionTest.define_reflected_tables` always declares `Column("email_address", …, index=True)` ([test_reflection.py#L744](https://github.com/sqlalchemy/sqlalchemy/blob/9c88919beb770ff0b14deab7e914ff7aaa5c4bd2/lib/sqlalchemy/testing/suite/test_reflection.py#L744)).
   - The method's explicit `Index` objects are gated by `testing.requires.index_reflection.enabled` ([L814](https://github.com/sqlalchemy/sqlalchemy/blob/9c88919beb770ff0b14deab7e914ff7aaa5c4bd2/lib/sqlalchemy/testing/suite/test_reflection.py#L814)).
   - `QuotedNameArgumentTest.define_tables` always adds `sa.Index("ix quote ' one", "name")` ([L483-L497](https://github.com/sqlalchemy/sqlalchemy/blob/9c88919beb770ff0b14deab7e914ff7aaa5c4bd2/lib/sqlalchemy/testing/suite/test_reflection.py#L483-L497)).
   - On a backend that rejects CREATE INDEX, table setup fails, so the whole class errors instead of just the index tests skipping.

Our current overrides clear `table.indexes` and add `requires.autoincrement_insert`: [test_suite.py#L10-L39](https://github.com/redis/redis-adbc-driver/blob/682fae1f73419b3efb4d5e011b9da0a4ca1d5588/python/acceptance/sqlalchemy/test_suite.py#L10-L39).

**Proposal:** add `@requirements.autoincrement_insert` to (1). For (2), gate `index=True` and the quoted-name index on `index_reflection`, or on a DDL-level requirement if you prefer. We can send a PR once it is marked open for PRs.

This is not a runnable MCVE: it needs a backend lacking these capabilities. We kept no public copy of the exact errors.
