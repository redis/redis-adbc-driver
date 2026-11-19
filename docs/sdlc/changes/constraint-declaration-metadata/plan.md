# Implementation

1. Parse column/table UNIQUE and REFERENCES/FOREIGN KEY declarations, preserving
   ordered columns and reference arity.
2. Store immutable local/reference fields and stable target identity in optional
   metadata; resolve target metadata before the pure ALTER planning/replay path.
3. Render ADBC declarations and resolve current target names using only the
   reader's pinned metadata. Decode catalog/schema storage identities at output.
4. Preserve metadata through clone/ALTER/rename; remove local dependencies on
   DROP COLUMN and declaration names on DROP CONSTRAINT.
5. Enable the existing native validation fixtures/features and add parser,
   identity/name-reuse and Software lifecycle regressions; update scope docs.
