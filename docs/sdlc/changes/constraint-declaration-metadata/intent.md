# UNIQUE and FOREIGN KEY declaration metadata

Issues #535 and #536 request native ADBC GetObjects coverage for SQL UNIQUE and
FOREIGN KEY declarations. Preserve accepted DDL and autocommit behavior while
making declaration metadata useful without claiming integrity enforcement.
Enforcement remains the separate #114 workstream.
