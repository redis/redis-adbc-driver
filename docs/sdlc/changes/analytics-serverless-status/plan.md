# Implementation and verification

See [intent](intent.md) and [specification](spec.md).

1. Read the live deployment with the owner's existing AWS credentials, read-only.
2. Rewrite the top of `analytics/deploy/README.md` for the serverless collector and
   move the EC2 instructions under a historical heading.
3. Correct the hosted-collector description in `analytics/README.md`.
4. Check links, anchors and the stated live facts.

## Live observations (2026-11-26, read-only)

All commands used read-only AWS API calls. The token was never printed; its check
sent it only to the dashboard API and recorded the HTTP status.

- DNS: the hostname is a Route 53 alias to an API Gateway custom domain. Its API
  (HTTP, tagged as Terraform-managed) has one Lambda proxy integration and six
  routes: `GET /`, `/dashboard.js`, `/dashboard.css`, `/health`, `/api/summary`
  and `POST /v1/usage`. The `$default` stage throttles at 10 requests/second with
  burst 20.
- Lambda: `api`, `refresh` and `downloads` functions, Python 3.12 on arm64, last
  deployed 2026-11-20 UTC. Memory/timeout/reserved concurrency: 256 MiB/10 s/10,
  2048 MiB/900 s/1 and 256 MiB/300 s/1. The `api` function's environment names
  the read-token SSM parameter; its value was not read from the environment.
- Schedules: EventBridge rules for `refresh` (15 minutes) and `downloads` (daily)
  are enabled. The refresh error alarm was `OK` with no alarm actions.
- Token: `/redis-adbc-analytics/read-token` is a SecureString, last modified
  2026-11-19 (Pacific).
- HTTP: `/health` and `/` returned 200; `/api/summary` returned 401 without a token
  and 200 with it. Its `generated_at` was 2026-11-26T20:32:54Z, seven minutes before
  the check.
- Storage: bucket versioning is enabled. Lifecycle rules expire `backups/` after
  90 days and noncurrent `serverless/generations/` objects after 7 days. The last
  legacy RDB backup is dated 2026-11-20 01:28 UTC; the 51 MB migration baseline
  `serverless/baseline.json.gz` exists.
- Legacy EC2: both recorded instances no longer exist. CloudTrail shows the
  2026-11-05 recovery host terminated that day, and the later collector host
  stopped (with its Elastic IP released) at 17:35 and terminated at 19:49 on
  2026-11-19 (Pacific). No detached project disk or tagged Elastic IP remains. The
  collector security group still exists.
- Terraform state (resource addresses only, values not read): it holds the
  serverless module and the root EC2 resources, including the terminated host as
  stopped. This checkout's Terraform has no serverless module.
- Source: the serverless source, packaging script, migration tool, Terraform module
  and the 2026-11-19 change record (`serverless-telemetry`) exist only in an
  unmerged local stash commit and a local backup of untracked files. They are not
  on any branch or PR.

## Validation

- Markdown links and the in-page anchor resolved against the working tree.
- No driver or analytics code changed, so no tests were run.
- Stated facts match the observations above. The serverless behavior section
  follows the unmerged `serverless-telemetry` specification, whose deployed
  configuration matches the live readings.
