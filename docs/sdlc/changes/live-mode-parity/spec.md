# Integration contract

An attached live query must reject buffered rows after its registration changes.
New queries and MERGE must use the replacement definition. Removing a public
column leaves its application field intact; subsequent MERGE updates preserve
that field and the source TTL. Evolution assumes its documented exclusive writer
quiescence; an old reader may remain open but must fail explicitly.

Individual feature contracts and limitations remain in their issue-specific
change records. See [intent](intent.md) and [plan](plan.md).
