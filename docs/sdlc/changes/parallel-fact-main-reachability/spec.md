# Specification

The original base main `f6e0bbbec1f9e1e6c973e7d7071b384ad174aaa8` already contains PR607, PR608, PR610, PR611 and PR613, but not the PR612 merge. Restore the public bounded parallel reader, exact count/byte admission helper and focused tests without changing main's 131,072 candidate /16 MiB estimated-key-byte limits or copying cohort production. The strict direct-Nil HASH guard is already inherited from main.

On every coordinator exit, including a visitor panic, cancel the child context and join all workers before releasing their worker grant. Preserve panic propagation after cleanup. Original source fences, exact ranges, residuals, duplicate/NULL semantics and source-error precedence remain in force.

Before publication integration, main advanced to `7af3a0fb` with PR615 fact demand and PR616 alias prefilter. The follow-up merges those public changes without alteration and resolves the single star hook overlap by computing/appending necessary fact demand before parallel admission. Both the parallel and serial fact paths therefore retain the same restriction.
