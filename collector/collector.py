#!/usr/bin/env python3
"""Copy spark.md snapshots into SparkRoot/Projects/.

The script lives at the top of SparkRoot, so SparkRoot is the folder it sits
in. It reads scan roots from SparkRoot/Config/scan_roots.json, finds each
spark.md under them, and copies it to SparkRoot/Projects/ as
projectName__projectType.md, built from the snapshot's front matter.

It runs once and exits; schedule it with cron or similar. It never deletes
anything: to remove a card, delete its file from Projects/ by hand.

Python 3 standard library only.
"""

import json
import logging
import os
import re
import sys
import tempfile
from pathlib import Path

SPARK_ROOT = Path(__file__).resolve().parent
PROJECTS_DIR = SPARK_ROOT / "Projects"
SCAN_ROOTS_FILE = SPARK_ROOT / "Config" / "scan_roots.json"
SPARK_FILE = "spark.md"

# Never descended into while scanning, along with any dot-directory.
SKIP_DIRS = {"node_modules", "vendor", "build", "dist"}

log = logging.getLogger("collector")


def load_scan_roots():
    """Return the scan roots as absolute paths, with ~ expanded."""
    with open(SCAN_ROOTS_FILE, encoding="utf-8") as f:
        roots = json.load(f)
    if not isinstance(roots, list) or not all(isinstance(r, str) for r in roots):
        raise ValueError(f"{SCAN_ROOTS_FILE}: expected a JSON list of folder paths")
    return [Path(r.strip()).expanduser() for r in roots if r.strip()]


def scan(roots):
    """Return the path of every spark.md under the roots, sorted.

    A folder is not descended into once spark.md is found there. A root that
    cannot be read is logged and skipped.
    """
    found = []
    for root in roots:
        if not root.is_dir():
            log.warning("scan %s: not a readable directory", root)
            continue
        for dirpath, dirnames, filenames in os.walk(
            root, onerror=lambda e: log.warning("scan %s: %s", e.filename, e.strerror)
        ):
            if SPARK_FILE in filenames and (Path(dirpath) / SPARK_FILE).is_file():
                found.append(Path(dirpath) / SPARK_FILE)
                dirnames.clear()
                continue
            dirnames[:] = [d for d in dirnames if not d.startswith(".") and d not in SKIP_DIRS]
    return sorted(found)


def front_matter(text):
    """Parse the flat key: value front matter that skill/format.md defines."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError("no front matter")
    fields = {}
    for line in lines[1:]:
        if line.strip() == "---":
            return fields
        key, sep, value = line.partition(":")
        if sep:
            fields[key.strip()] = value.strip().strip("\"'")
    raise ValueError("front matter is not closed")


def camel_case(text):
    """Join the letter and digit runs of text: 'Spark / Web App' -> 'sparkWebApp'."""
    words = re.findall(r"[^\W_]+", text)
    if not words:
        return ""
    return words[0].lower() + "".join(w[:1].upper() + w[1:].lower() for w in words[1:])


def target_name(snapshot):
    fields = front_matter(snapshot.read_text(encoding="utf-8"))
    project = camel_case(fields.get("project", ""))
    project_type = camel_case(fields.get("project_type", ""))
    if not project or not project_type:
        raise ValueError("project and project_type must both be set")
    return f"{project}__{project_type}.md"


def put(name, data):
    """Write data to Projects/name without ever leaving a half-written file.

    The web app can read at any moment, so write a temp file and rename it.
    """
    fd, tmp = tempfile.mkstemp(dir=PROJECTS_DIR, prefix=f".{name}.", suffix=".tmp")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
        # mkstemp makes the file private; snapshots are meant to be read.
        os.chmod(tmp, 0o644)
        os.replace(tmp, PROJECTS_DIR / name)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def main():
    logging.basicConfig(format="%(asctime)s %(levelname)s %(message)s", level=logging.INFO)

    try:
        roots = load_scan_roots()
    except (OSError, ValueError) as e:
        log.error("scan roots: %s", e)
        return 1
    PROJECTS_DIR.mkdir(exist_ok=True)

    snapshots = scan(roots)
    log.info("found %d spark.md file(s) under %s", len(snapshots), ", ".join(map(str, roots)))

    failed = False
    # Lowercased, because some filesystems ignore case and would let two
    # names that differ only by case overwrite each other.
    claimed = {}
    for snapshot in snapshots:
        try:
            name = target_name(snapshot)
        except (OSError, UnicodeDecodeError, ValueError) as e:
            log.warning("skipping %s: %s", snapshot, e)
            continue

        first = claimed.get(name.lower())
        if first:
            log.warning("skipping %s: %s is already taken by %s", snapshot, name, first)
            continue
        claimed[name.lower()] = snapshot

        try:
            put(name, snapshot.read_bytes())
        except OSError as e:
            log.error("copy %s: %s", snapshot, e)
            failed = True
            continue
        log.info("copied %s -> %s", snapshot, name)

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
