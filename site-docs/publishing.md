# Documentation website

This website is generated from the repository's Markdown files using MkDocs.
The public guides selected in `mkdocs.yml` become individual HTML pages with
navigation, site search, canonical URLs, a sitemap and a crawler-accessible
`robots.txt`. Other repository files are linked on GitHub.

## Preview and check locally

From the repository root:

```sh
python3 -m venv /tmp/redis-adbc-docs
/tmp/redis-adbc-docs/bin/pip install -r requirements-docs.txt
/tmp/redis-adbc-docs/bin/python scripts/prepare_docs.py
/tmp/redis-adbc-docs/bin/python -m mkdocs build --strict
/tmp/redis-adbc-docs/bin/python scripts/check_docs.py
/tmp/redis-adbc-docs/bin/python -m mkdocs serve
```

After editing the original Markdown, rerun `prepare_docs.py` to update the staged
sources. Add pages explicitly to `nav` in `mkdocs.yml` to publish them.
The staging step copies only those Markdown files. It does not copy development
records, logs, license files, deployment configuration or benchmark artifacts.
Review every new page for public suitability before adding it.

## Publish on GitHub Pages

1. In repository **Settings → Pages → Build and deployment**, select
   **GitHub Actions** as the source.
2. Merge the documentation workflow to `main`. It builds and checks the site,
   then deploys it to GitHub Pages. Subsequent documentation changes publish
   automatically after merging. Pull requests build and check without deploying.
3. The default URL is <https://redis.github.io/redis-adbc-driver/>. When Pages
   supplies a custom-domain URL, the workflow uses it for canonicals and sitemap.

The build job has read-only repository permissions. Only the deployment job
receives Pages and identity-token write permissions.

## Google discovery

Once the site is live, verify ownership in
[Google Search Console](https://search.google.com/search-console/about) and submit
the site's `sitemap.xml`. Use URL Inspection to request indexing of the homepage.
Ownership verification requires the maintainer's Google account or DNS access.
Public pages and a sitemap make the site discoverable; Google decides which pages
to index and when.

See [Google's indexing guidance](https://developers.google.com/search/docs/crawling-indexing/ask-google-to-recrawl)
and [GitHub's Pages workflow documentation](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages).
