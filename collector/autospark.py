#!/usr/bin/env python3
"""Run the Spark skill in every project whose snapshot is older than its code.

The script lives at the top of SparkRoot, next to collector.py, and reuses
its scan. A project is stale when its newest commit, not counting commits that
touch only spark.md, is newer than the snapshot's last_updated, or when it has
no spark.md yet. For each stale project it runs `claude -p "Spark, go"` there,
unattended, and the collector copies the new snapshot on its next run.

Which projects count comes from SparkRoot/Config/autospark.json:

    {"every_git_repo": true}

true (the default, also when the file is missing): every git repo under the
scan roots, plus every folder that already has a spark.md. A repo with no
spark.md at or under it gets its first one. false: only folders that already
have a spark.md.

Usage: autospark.py [CLAUDE], where CLAUDE is the claude binary (default: the
one on PATH). It runs once and exits; setup.sh schedules it nightly.

Python 3 standard library only.
"""

import fcntl
import json
import logging
import os
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path

import collector

SETTINGS_FILE = collector.SPARK_ROOT / "Config" / "autospark.json"
LOCK_FILE = collector.SPARK_ROOT / ".autospark.lock"
PROJECT_TYPES_FILE = collector.SPARK_ROOT / "Config" / "project_types.json"

# One project's run; a stuck run must not hold up the rest of the night.
RUN_TIMEOUT = 20 * 60

PROMPT = f"""Spark, go.

This is an unattended run started by autospark.py: nobody can answer
questions. Wherever the skill says to ask the user, make your best guess
instead; for project_type, pick the closest name in {PROJECT_TYPES_FILE}.
Write only spark.md."""

# Read anything, run only read-only commands, write only spark.md.
ALLOWED_TOOLS = [
    "Skill(spark)",
    "Read",
    "Glob",
    "Grep",
    "Edit(./spark.md)",
    "Bash(git log:*)",
    "Bash(git status:*)",
    "Bash(git diff:*)",
    "Bash(ls:*)",
    "Bash(date:*)",
    "Bash(realpath:*)",
]

log = logging.getLogger("autospark")


def every_git_repo():
    """Return the every_git_repo setting; true unless autospark.json says otherwise."""
    try:
        with open(SETTINGS_FILE, encoding="utf-8") as f:
            settings = json.load(f)
    except FileNotFoundError:
        return True
    value = settings.get("every_git_repo", True) if isinstance(settings, dict) else None
    if not isinstance(value, bool):
        raise ValueError(f"{SETTINGS_FILE}: expected {{\"every_git_repo\": true or false}}")
    return value


def git_repos(roots):
    """Return every git repo under the roots, sorted. Nested repos are not searched."""
    found = []
    for root in roots:
        if not root.is_dir():
            continue
        for dirpath, dirnames, filenames in os.walk(root, onerror=lambda e: None):
            if ".git" in dirnames or ".git" in filenames:
                found.append(Path(dirpath))
                dirnames.clear()
                continue
            dirnames[:] = [d for d in dirnames if not d.startswith(".") and d not in collector.SKIP_DIRS]
    return sorted(found)


def projects(roots, all_repos):
    """Return the folders to check: each spark.md's folder, plus repos without one.

    A repo that already has a spark.md anywhere under it is left as it is, so a
    new snapshot at its top doesn't hide the ones below from the collector.
    """
    folders = [s.parent for s in collector.scan(roots)]
    if all_repos:
        for repo in git_repos(roots):
            if not any(f == repo or repo in f.parents for f in folders):
                folders.append(repo)
    return sorted(folders)


def last_commit(folder):
    """Return the time of the newest commit under folder that touches more than spark.md.

    None when there is none, or folder is not in a git repo.
    """
    result = subprocess.run(
        ["git", "-C", str(folder), "log", "-1", "--format=%ct", "--", ".", f":(exclude){collector.SPARK_FILE}"],
        capture_output=True, text=True,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return None
    return int(result.stdout.strip())


def snapshot_time(folder):
    """Return the snapshot's last_updated as a timestamp, or None if it has none we can read."""
    try:
        fields = collector.front_matter((folder / collector.SPARK_FILE).read_text(encoding="utf-8"))
        return datetime.fromisoformat(fields["last_updated"]).timestamp()
    except (OSError, UnicodeDecodeError, ValueError, KeyError):
        return None


def run_skill(claude, folder):
    """Run the skill in folder; return True if spark.md has a new last_updated."""
    before = snapshot_time(folder)
    try:
        result = subprocess.run(
            [claude, "-p", PROMPT, "--allowedTools", *ALLOWED_TOOLS, "--no-session-persistence"],
            cwd=folder, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=RUN_TIMEOUT,
        )
    except subprocess.TimeoutExpired:
        log.error("%s: claude did not finish in %d minutes", folder, RUN_TIMEOUT // 60)
        return False
    after = snapshot_time(folder)
    if result.returncode != 0 or after is None or after == before:
        output = (result.stdout + result.stderr).strip()
        log.error("%s: no new spark.md (claude exited %d): %s", folder, result.returncode, output[-2000:])
        return False
    return True


def main():
    logging.basicConfig(format="%(asctime)s %(levelname)s %(message)s", level=logging.INFO)

    claude = sys.argv[1] if len(sys.argv) > 1 else shutil.which("claude")
    if not claude:
        log.error("claude not found; pass its path as the first argument")
        return 1

    # Two runs at once would run the skill twice in the same project.
    lock = open(LOCK_FILE, "w")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        log.info("another run is still going; nothing to do")
        return 0

    try:
        roots = collector.load_scan_roots()
        all_repos = every_git_repo()
    except (OSError, ValueError) as e:
        log.error("settings: %s", e)
        return 1

    failed = False
    for folder in projects(roots, all_repos):
        committed = last_commit(folder)
        if committed is None:
            continue
        snapshot = snapshot_time(folder)
        if snapshot is not None and committed <= snapshot:
            continue
        log.info("%s: %s, running the skill", folder, "no readable snapshot" if snapshot is None else "newer commits")
        if run_skill(claude, folder):
            log.info("%s: wrote spark.md", folder)
        else:
            failed = True

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
