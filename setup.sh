#!/bin/sh
# Spark host setup. The Spark container copies this script into SparkRoot;
# run it once on the host, from there:
#
#   ~/Documents/Spark/setup.sh
#
# It does the two things the container can't, since it never touches host
# folders outside SparkRoot:
#   1. links ~/.claude/skills/spark to SparkRoot/Skill
#   2. adds a cron line that runs the collector every 15 minutes
# Both steps are skipped when already done, so rerunning it is safe.
set -eu

ROOT=$(cd "$(dirname "$0")" && pwd -P)
SKILL_LINK="$HOME/.claude/skills/spark"

if [ ! -f "$ROOT/collector.py" ] || [ ! -d "$ROOT/Skill" ]; then
  echo "error: $ROOT has no collector.py or Skill/. Start the Spark container with this folder mounted first." >&2
  exit 1
fi

PYTHON=$(command -v python3 || true)
if [ -z "$PYTHON" ]; then
  echo "error: python3 is not installed; the collector needs it." >&2
  exit 1
fi

# 1. Skill link. A real folder at that path is someone's own skill: leave it.
mkdir -p "$(dirname "$SKILL_LINK")"
if [ -L "$SKILL_LINK" ] || [ ! -e "$SKILL_LINK" ]; then
  ln -sfn "$ROOT/Skill" "$SKILL_LINK"
  echo "skill: $SKILL_LINK -> $ROOT/Skill"
else
  echo "skill: $SKILL_LINK exists and is not a link; left it alone. Move it away and rerun to link Spark's skill."
fi

# 2. Cron line. collector.log holds the last run's output only.
# Quoted, so a SparkRoot path with spaces still works. cron runs this with sh.
CRON_LINE="*/15 * * * * '$PYTHON' '$ROOT/collector.py' > '$ROOT/collector.log' 2>&1"
if ! command -v crontab >/dev/null 2>&1; then
  echo "cron: crontab not found. Schedule this yourself:"
  echo "  $CRON_LINE"
elif crontab -l 2>/dev/null | grep -Fq "$ROOT/collector.py"; then
  echo "cron: already scheduled"
else
  { crontab -l 2>/dev/null || true; echo "$CRON_LINE"; } | crontab -
  echo "cron: collector runs every 15 minutes, log in $ROOT/collector.log"
fi

echo
echo "Next: open the dashboard, go to Settings, and add the folders to scan."
