# Plan

1. Verify actual main ancestry for PR607/608/610/611/612/613, not only GitHub merged labels.
2. Apply public PR612 delta excluding its already-in-main shared HASH guard.
3. Add the independently reproduced visitor-panic join fix and native mock regression.
4. Run focused race and all unit-only packages with endpoints unset.
5. Publish a main-targeted PR; keep combined Q7 validation and remaining Q4 timeout attribution explicit.
