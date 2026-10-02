#!/bin/sh
# Spark host setup. The Spark container copies this script into SparkRoot;
# run it once on the host, from there:
#
#   ~/Documents/Spark/setup.sh
#
# It does the things the container can't, since it never touches host
# folders outside SparkRoot:
#   1. links ~/.claude/skills/spark to SparkRoot/Skill, and
#      ~/.claude/skills/spark-handoff to SparkRoot/HandoffSkill
#   2. adds a cron line that runs the collector every 15 minutes
# Each step is skipped when already done, so rerunning it is safe.
set -eu

ROOT=$(cd "$(dirname "$0")" && pwd -P)
SKILLS="$HOME/.claude/skills"

if [ ! -f "$ROOT/collector.py" ] || [ ! -d "$ROOT/Skill" ] || [ ! -d "$ROOT/HandoffSkill" ]; then
  echo "error: $ROOT has no collector.py, Skill/ or HandoffSkill/. Start the Spark container with this folder mounted first." >&2
  exit 1
fi

PYTHON=$(command -v python3 || true)
if [ -z "$PYTHON" ]; then
  echo "error: python3 is not installed; the collector needs it." >&2
  exit 1
fi

# 1. Skill links. A real folder at that path is someone's own skill: leave it.
link_skill() { # link_skill <name> <SparkRoot folder>
  link="$SKILLS/$1"
  if [ -L "$link" ] || [ ! -e "$link" ]; then
    ln -sfn "$ROOT/$2" "$link"
    echo "skill: $link -> $ROOT/$2"
  else
    echo "skill: $link exists and is not a link; left it alone. Move it away and rerun to link it."
  fi
}
mkdir -p "$SKILLS"
link_skill spark Skill
link_skill spark-handoff HandoffSkill

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
