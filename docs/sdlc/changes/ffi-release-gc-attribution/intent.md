# FFI release collection attribution

Date: 2026-11-13. Status: experiment prepared; measurements pending. Requestor:
repository user, asking for the four next performance investigations with
subagents. Base: `24f654b76b98f3d8960af94a3ca0230185d467a1`.

The C boundary forces a whole Go garbage collection after every stream,
statement, connection and database release. Attribute that cost using actual C
entrypoints, establish which foreign callbacks run synchronously through explicit
ownership, and measure a full-read control. Preserve the default behavior until
ownership and sanitizer evidence justify a separate production proposal.

See [specification](spec.md), [plan](plan.md), and [validation](validation.md).
