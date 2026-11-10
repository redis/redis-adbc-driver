# Specification: public documentation website

Date: 2026-11-10. Source: `73eb9642`. Status: implementation.
Requestor: repository user. Issue/PR: pending.

Use MkDocs 1.6.1 and GitHub Pages. Explicit `nav` entries in `mkdocs.yml` select
the public Markdown pages; stage only those files. Preserve relative links
between published pages and direct other repository links to GitHub. Do not
copy linked artifacts. Fail the build for missing repository link targets.

Generate crawlable HTML, canonical URLs, a complete sitemap, robots.txt and
local search. Default to `https://redis.github.io/redis-adbc-driver/`; use the
Pages URL in deployment to accommodate project prefixes and custom domains.

Pull requests build/check with read-only access. Only main-branch pushes or
manual runs deploy, with Pages and OIDC permissions confined to deployment.
Google indexing remains controlled by Google; Search Console verification
requires the maintainer's account. Pages must be enabled in repository settings.

Acceptance: strict build succeeds; generated-site checks verify page selection,
links/assets, canonical URLs, sitemap, search coverage and artifact exclusions.

See [intent](intent.md) and [plan](plan.md).
