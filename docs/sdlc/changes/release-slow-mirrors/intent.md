# Intent: prevent slow mirrors from exhausting release validation

Date: 2026-10-30. Base: `9564496`. Requested by the repository maintainer
through an Actions failure investigation. See [spec](spec.md), [plan](plan.md).

Release runs [41146601040](https://github.com/redis/redis-adbc-driver/actions/runs/41146601040)
and [40921355929](https://github.com/redis/redis-adbc-driver/actions/runs/40921355929)
failed on AlmaLinux 9 amd64 and arm64 respectively. DNF downloaded AppStream
metadata at 73–80 kB/s until the eight-minute validation deadline expired;
pytest never started. Make prerequisite installation reject slow mirrors
without changing test assertions, supported platforms or workflow deadlines.
