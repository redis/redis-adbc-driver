# Intent

Reduce allocation when a freshly decoded direct native fact has exactly one match
in every prepared star dimension. This is a narrow execution allocation change;
no SQL eligibility, source scan, ordering, worker grant or candidate proof changes.
