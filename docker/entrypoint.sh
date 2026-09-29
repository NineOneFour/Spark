#!/bin/sh
# Run the collector every COLLECT_INTERVAL seconds in the background, and the
# web app in the foreground. Set COLLECT_INTERVAL=0 to skip the collector,
# for example when snapshots arrive some other way.
set -e

if [ "${COLLECT_INTERVAL}" != "0" ]; then
  (
    while true; do
      collector || true
      sleep "${COLLECT_INTERVAL}"
    done
  ) &
fi

exec web
