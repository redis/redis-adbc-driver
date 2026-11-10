"""Keep repository links usable without copying repository artifacts to Pages."""

import os
from pathlib import Path
from urllib.parse import quote, unquote, urlsplit, urlunsplit

from markdown.extensions import Extension
from markdown.treeprocessors import Treeprocessor

ROOT = Path(__file__).resolve().parents[1]
SOURCE_URL = "https://github.com/redis/redis-adbc-driver"
current_source = ""
published = set()


class RepositoryLinks(Treeprocessor):
    def run(self, root):
        for element in root.iter():
            for attribute in ("href", "src"):
                original = element.get(attribute)
                if not original:
                    continue
                url = urlsplit(original)
                if url.scheme or url.netloc or not url.path or url.path.startswith("/"):
                    continue
                path = (ROOT / Path(current_source).parent / unquote(url.path)).resolve()
                if not path.is_relative_to(ROOT):
                    raise ValueError(f"Link escapes repository: {current_source}: {original}")
                relative = path.relative_to(ROOT).as_posix()
                if relative in published:
                    continue  # MkDocs rewrites links to other published pages.
                if not path.exists():
                    raise ValueError(f"Missing link target: {current_source}: {original}")
                kind = "tree" if path.is_dir() else "blob"
                # Images need raw content; source and benchmark links need GitHub's viewer.
                base = (
                    "https://raw.githubusercontent.com/redis/redis-adbc-driver/main/"
                    if attribute == "src"
                    else f"{SOURCE_URL}/{kind}/main/"
                )
                element.set(attribute, urlunsplit((
                    "https", urlsplit(base).netloc,
                    urlsplit(base).path + quote(relative, safe="/"), url.query, url.fragment,
                )))


class RepositoryLinksExtension(Extension):
    def extendMarkdown(self, md):
        # After Markdown inline links, before MkDocs' relative-path processor.
        md.treeprocessors.register(RepositoryLinks(md), "repository_links", 5)


def on_config(config):
    global published
    published = {
        path.relative_to(config["docs_dir"]).as_posix()
        for path in Path(config["docs_dir"]).rglob("*.md")
    }
    config["markdown_extensions"].append(RepositoryLinksExtension())
    # GitHub Pages supplies this so custom domains and project prefixes stay correct.
    if os.environ.get("DOCS_SITE_URL"):
        config["site_url"] = os.environ["DOCS_SITE_URL"].rstrip("/") + "/"
    return config


def on_page_markdown(markdown, page, **kwargs):
    global current_source
    current_source = page.file.src_uri
    return markdown


def on_post_build(config):
    destination = Path(config["site_dir"])
    (destination / ".nojekyll").touch()
    (destination / "robots.txt").write_text(
        "User-agent: *\nAllow: /\n\nSitemap: " + config["site_url"] + "sitemap.xml\n"
    )
