#!/usr/bin/env python3
"""Validate a real Hugo site at domain-root and subdirectory URLs."""
import json
from html.parser import HTMLParser
from pathlib import Path
import shutil
import subprocess
import tempfile
from urllib.parse import unquote, urlsplit


class Document(HTMLParser):
    def __init__(self, text):
        super().__init__()
        self.links = []
        self.search_index = None
        self.active = []
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        for key in ("href", "src"):
            if key in attrs:
                self.links.append((tag, attrs[key]))
        if attrs.get("id") == "search-input":
            self.search_index = attrs.get("data-index")
        if attrs.get("aria-current") == "page":
            self.active.append(attrs.get("href"))


def check_site(source, *, theme_repo=None):
    source = Path(source).resolve()
    with tempfile.TemporaryDirectory(prefix="dockleaf-check-") as directory:
        scratch = Path(directory)
        if theme_repo:
            installed = scratch / "site"
            shutil.copytree(source, installed, ignore=shutil.ignore_patterns("public", "resources", ".hugo_build.lock"))
            theme = installed / "themes/dockleaf"
            theme.mkdir(parents=True)
            for name in ("layouts", "static", "i18n", "hugo.toml", "theme.toml"):
                origin = theme_repo / name
                if origin.is_dir():
                    shutil.copytree(origin, theme / name)
                else:
                    shutil.copy2(origin, theme / name)
            source = installed
            overrides = source / "assets/css/custom.css"
            overrides.parent.mkdir(parents=True)
            overrides.write_text(":root { --brand: #34d399; }\n")
        for prefix in ("/", "/project/"):
            destination = scratch / ("root" if prefix == "/" else "subdirectory")
            command = ["hugo", "--source", str(source), "--destination", str(destination),
                       "--baseURL", "https://example.org" + prefix, "--panicOnWarning"]
            subprocess.run(command, check=True)
            home = Document((destination / "index.html").read_text())
            assert home.search_index == prefix + "index.json", home.search_index
            pages = json.loads((destination / "index.json").read_text())
            assert pages, "Search index is empty"
            assert len({page["href"] for page in pages}) == len(pages)
            for page in pages:
                assert page["href"].startswith(prefix), page
                assert isinstance(page["content"], str)
                target = destination / page["href"].removeprefix(prefix) / "index.html"
                assert target.is_file(), target
                doc = Document(target.read_text())
                assert page["href"] in doc.active, (target, doc.active)
            for html in destination.rglob("*.html"):
                doc = Document(html.read_text())
                for tag, link in doc.links:
                    url = urlsplit(link)
                    if url.scheme or url.netloc or not url.path:
                        continue
                    assert url.path.startswith(prefix), (html, link)
                    target = destination / unquote(url.path.removeprefix(prefix))
                    if url.path.endswith("/"):
                        target /= "index.html"
                    assert target.is_file(), (html, link, target)
            if theme_repo:
                assert "Searchbase" not in (destination / "index.html").read_text()
                assert (destination / "guide/advanced/deployment/index.html").is_file()
                assert prefix + "guide/advanced/deployment/" in [link for _, link in home.links]
                assert prefix + "css/custom.css" in [link for _, link in home.links]
            print(f"Validated {len(pages)} searchable pages at {prefix}")
    theme = theme_repo if theme_repo else source / "themes/dockleaf"
    if shutil.which("node"):
        for script in (theme / "static/js").glob("*.js"):
            subprocess.run(["node", "--check", str(script)], check=True)
    else:
        print("Node.js not installed; skipped optional JavaScript syntax checks")


if __name__ == "__main__":
    root = Path(__file__).resolve().parents[1]
    check_site(root / "docs")
