#!/bin/sh
# Fill SparkRoot from the image, then run the web app.
#
# collector.py, setup.sh, INSTALL.md and Skill/ are overwritten on every
# start, so pulling a new image updates them. Config/ and Projects/ are never
# touched here; the web app creates missing Config/ defaults itself.
set -eu

SHARE=/usr/local/share/spark
ROOT="${SPARK_ROOT:-/spark}"

if [ "$(id -u)" = "0" ]; then
  echo "warning: running as root, so SparkRoot files will be owned by root. Run with --user \"\$(id -u):\$(id -g)\"." >&2
fi
if ! [ -w "$ROOT" ]; then
  echo "error: $ROOT is not writable by uid $(id -u). Create the host folder yourself before starting the container (if Docker creates it, root owns it), and run with --user \"\$(id -u):\$(id -g)\"." >&2
  exit 1
fi

# Copy then rename, so the host never runs a half-written collector.py.
put() {
  tmp="$ROOT/$(dirname "$1")/.$(basename "$1").tmp"
  cp "$SHARE/$1" "$tmp"
  mv -f "$tmp" "$ROOT/$1"
}

put collector.py
put setup.sh
put INSTALL.md
# Every file in Skill/, including any in subfolders.
(cd "$SHARE" && find Skill -type d) | while read -r d; do mkdir -p "$ROOT/$d"; done
(cd "$SHARE" && find Skill -type f) | while read -r f; do put "$f"; done

exec web
