"""Stage only the Markdown pages explicitly selected in mkdocs.yml."""

from pathlib import Path
import shutil

import yaml

ROOT = Path(__file__).resolve().parents[1]


def pages(nav):
    for item in nav:
        for value in item.values():
            if isinstance(value, list):
                yield from pages(value)
            else:
                yield value


def main():
    config = yaml.safe_load((ROOT / "mkdocs.yml").read_text())
    target = ROOT / config["docs_dir"]
    if target != ROOT / "build/docs-source":
        raise ValueError("Refusing to clear an unexpected staging directory")
    shutil.rmtree(target, ignore_errors=True)
    target.mkdir(parents=True)
    selected = list(pages(config["nav"]))
    for name in selected:
        source = (ROOT / name).resolve()
        if not source.is_relative_to(ROOT) or source.suffix != ".md":
            raise ValueError(f"Not a repository Markdown page: {name}")
        destination = target / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
    print(f"Staged {len(selected)} public documentation pages")


if __name__ == "__main__":
    main()
