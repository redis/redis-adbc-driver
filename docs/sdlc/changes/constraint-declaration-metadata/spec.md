# Contract

* Persist optional UNIQUE/FK declaration names and ordered immutable column
  fields. Existing metadata without declarations continues to read unchanged.
* GetObjects reports UNIQUE/FK types and ordered local columns. UNIQUE usage is
  NULL; FK usage contains ordered logical catalog/schema/table/column identities
  only when every pair is resolvable against the reader's pinned metadata root.
* Resolve references at declaration time, preserving historical acceptance of
  missing referenced tables/columns/keys. An unresolved declaration remains
  unresolved until recreated. Never bind a reused name to a replacement object.
* Managed target OIDs and declaration epochs survive rename/rekey/type rewrite.
  Fresh descriptor constructors assign a new epoch even when Arrow replacement
  or import refresh preserves the public OID. Legacy epoch-less descriptors keep
  their stable OID identity through cloning and rewrites. Immutable column fields
  survive column rename; target drop or referenced-column drop suppresses usage.
  Application descriptors additionally require their registration identity;
  discovered foreign indexes have no durable identity and yield no FK usage.
* ALTER ADD/DROP CONSTRAINT, ADD COLUMN declarations, DROP COLUMN dependencies,
  rename/rekey, TRUNCATE and metadata clones preserve this contract.
* No UNIQUE/FK row checks, unique indexes, cascading actions, transactions or
  information_schema/pg_constraint expansion. Duplicates and orphan rows remain
  accepted. Old ignored declarations cannot be reconstructed automatically.
