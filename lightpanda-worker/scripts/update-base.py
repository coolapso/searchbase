#!/usr/bin/env python3
"""Pin the latest numbered Lightpanda release and its multi-arch image digest."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


CONTAINERFILE = Path(__file__).resolve().parents[1] / "Containerfile"
VERSION = re.compile(r"v?(\d+)\.(\d+)\.(\d+)\Z")
BASE = re.compile(
    r"^FROM lightpanda/browser:(v?\d+\.\d+\.\d+)@(sha256:[a-f0-9]{64})$",
    re.MULTILINE,
)
DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")


def run(*args: str) -> str:
    return subprocess.run(args, check=True, text=True, capture_output=True, timeout=120).stdout


def main() -> None:
    original = CONTAINERFILE.read_text()
    matches = list(BASE.finditer(original))
    if len(matches) != 1:
        raise ValueError("expected exactly one pinned Lightpanda release FROM line")
    current_tag, current_digest = matches[0].groups()
    current_version = tuple(map(int, VERSION.fullmatch(current_tag).groups()))

    # GitHub currently marks its `nightly` release as non-prerelease, so the
    # releases/latest endpoint is insufficient. Require a numbered tag too.
    releases = run(
        "gh", "api", "--paginate", "repos/lightpanda-io/browser/releases?per_page=100",
        "--jq", ".[] | select(.draft == false and .prerelease == false) | .tag_name",
    ).splitlines()
    stable = [(tuple(map(int, VERSION.fullmatch(tag).groups())), tag)
              for tag in releases if VERSION.fullmatch(tag)]
    if not stable:
        raise ValueError("no numbered stable Lightpanda release found")
    latest_version, latest_tag = max(stable)
    if latest_version < current_version:
        print(f"Current Lightpanda {current_tag} is newer than the latest release {latest_tag}; leaving pin unchanged")
        return

    manifest = json.loads(run(
        "docker", "buildx", "imagetools", "inspect", "--format", "{{json .Manifest}}",
        f"lightpanda/browser:{latest_tag}",
    ))
    digest = manifest.get("digest", "")
    platforms = {
        (item.get("platform", {}).get("os"), item.get("platform", {}).get("architecture"))
        for item in manifest.get("manifests", [])
    }
    if not DIGEST.fullmatch(digest) or not {("linux", "amd64"), ("linux", "arm64")} <= platforms:
        raise ValueError(f"Lightpanda {latest_tag} lacks a valid multi-architecture index digest")

    if latest_tag == current_tag and digest == current_digest:
        print(f"Lightpanda {current_tag} is already current ({digest})")
        return

    updated = original[:matches[0].start()] + f"FROM lightpanda/browser:{latest_tag}@{digest}" + original[matches[0].end():]
    with tempfile.NamedTemporaryFile(mode="w", dir=CONTAINERFILE.parent, delete=False) as file:
        temporary = Path(file.name)
        file.write(updated)
    try:
        os.chmod(temporary, CONTAINERFILE.stat().st_mode)
        os.replace(temporary, CONTAINERFILE)
    finally:
        temporary.unlink(missing_ok=True)
    print(f"Updated Lightpanda base: {current_tag}@{current_digest} -> {latest_tag}@{digest}")


if __name__ == "__main__":
    main()
