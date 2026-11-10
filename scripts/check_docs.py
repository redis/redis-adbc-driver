"""Verify the built site's links, indexing metadata, and publication boundary."""

import json
import os
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit
import xml.etree.ElementTree as ET

import yaml

from prepare_docs import ROOT, pages


class Page(HTMLParser):
    def __init__(self, text):
        super().__init__()
        self.links = []
        self.canonical = None
        self.noindex = False
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "link" and attrs.get("rel") == "canonical":
            self.canonical = attrs.get("href")
        if tag == "meta" and attrs.get("name") == "robots":
            self.noindex |= "noindex" in attrs.get("content", "").lower()
        for attribute in ("href", "src"):
            if attribute in attrs:
                self.links.append(attrs[attribute])


def main():
    config = yaml.safe_load((ROOT / "mkdocs.yml").read_text())
    site = ROOT / config["site_dir"]
    source = ROOT / config["docs_dir"]
    base = (os.environ.get("DOCS_SITE_URL") or config["site_url"]).rstrip("/") + "/"
    base_url = urlsplit(base)
    expected_sources = set(pages(config["nav"]))
    actual_sources = {p.relative_to(source).as_posix() for p in source.rglob("*") if p.is_file()}
    errors = []
    if actual_sources != expected_sources:
        errors.append("Staged files differ from the explicit Markdown publication list")
    expected_urls = set()
    for name in expected_sources:
        path = Path(name)
        relative = path.with_suffix("")
        if path.stem in {"README", "index"}:
            relative = path.parent
        uri = relative.as_posix().strip(".").strip("/")
        expected_urls.add(base + (uri + "/" if uri else ""))
    actual_urls = set()
    for file in site.rglob("*.html"):
        page = Page(file.read_text())
        relative = file.relative_to(site).as_posix()
        if relative == "404.html":
            continue
        if not page.canonical or page.noindex:
            errors.append(f"{relative}: missing canonical URL or blocked indexing")
        actual_urls.add(page.canonical)
        page_url = base + relative.removesuffix("index.html")
        for link in page.links:
            resolved = urlsplit(urljoin(page_url, link))
            if (resolved.scheme, resolved.netloc) != (base_url.scheme, base_url.netloc):
                continue
            if not resolved.path.startswith(base_url.path):
                errors.append(f"{relative}: link outside project prefix: {link}")
                continue
            target = site / unquote(resolved.path[len(base_url.path):])
            if target.is_dir():
                target /= "index.html"
            if not target.is_file():
                errors.append(f"{relative}: missing link or asset: {link}")
    if actual_urls != expected_urls:
        errors.append(f"Published HTML differs from selected pages: {actual_urls ^ expected_urls}")
    sitemap = ET.parse(site / "sitemap.xml")
    sitemap_urls = {node.text for node in sitemap.findall(".//{*}loc")}
    if sitemap_urls != expected_urls:
        errors.append("Sitemap does not list exactly the selected public pages")
    search = json.loads((site / "search/search_index.json").read_text())
    if not search.get("docs"):
        errors.append("Empty site search index")
    for name in expected_sources:
        uri = Path(name).with_suffix("").as_posix() + "/"
        if Path(name).stem == "README":
            uri = str(Path(name).parent).strip(".")
            uri = uri + "/" if uri else ""
        if not any(doc["location"].split("#")[0] == uri for doc in search["docs"]):
            errors.append(f"Missing from search index: {name}")
    if f"Sitemap: {base}sitemap.xml" not in (site / "robots.txt").read_text():
        errors.append("robots.txt points to the wrong sitemap")
    if not (site / ".nojekyll").exists():
        errors.append("Missing .nojekyll")
    # MkDocs' own theme/search assets are expected; repository files are not.
    forbidden = {".md", ".jsonl", ".docx", ".rdb", ".pem", ".key", ".log", ".yaml", ".yml"}
    for file in site.rglob("*"):
        if file.suffix in forbidden:
            errors.append(f"Unexpected repository artifact: {file.relative_to(site)}")
    if errors:
        raise SystemExit("\n".join(errors))
    print(f"Verified {len(expected_urls)} HTML pages, local links/assets, canonicals, sitemap, search, and publication scope")


if __name__ == "__main__":
    main()
