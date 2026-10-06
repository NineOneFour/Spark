#!/bin/sh
# Spark setup and upgrade, for a person's own Spark (local mode). Run it from
# the repo:
#
#   ./setup.sh [--root DIR] [--port PORT] [--image IMAGE]
#
# It:
#   1. creates SparkRoot (default ~/Documents/Spark), so root doesn't own it
#   2. pulls the image (default nineonefour/spark:latest) and starts the
#      `spark` container on 127.0.0.1:PORT (default 9140). SPARK_USERNAME and
#      SPARK_PASSWORD in the environment turn login on. --image takes another
#      image, such as one built here with `docker build -t spark .`; a name
#      without a registry path is never pulled.
#   3. links ~/.claude/skills/spark to SparkRoot/Skill, and
#      ~/.claude/skills/spark-handoff to SparkRoot/HandoffSkill
#   4. asks which folder holds your projects, when no scan root is set yet
#   5. adds a cron line that runs the collector every 15 minutes, and runs it
#      once now so the cards are there straight away
#
# Rerun it to upgrade: it pulls the latest image. Options come
# from the existing container, so a plain rerun keeps them and flags change
# them. The container is recreated only when the image is newer or an option
# changed; SparkRoot holds all of Spark's data, so nothing is lost. The
# container also copies this script into SparkRoot, so it can be rerun from
# there. Every other step is skipped when already done.
#
# With --host-only it does only steps 3 to 5, for the SparkRoot it sits in
# (or --root): for a container started another way, such as Compose, or
# Spark without Docker.
set -eu

DEFAULT_IMAGE=nineonefour/spark:latest
NAME=spark
SKILLS="$HOME/.claude/skills"

usage() {
  echo "usage: $0 [--root DIR] [--port PORT] [--image IMAGE] | --host-only [--root DIR]" >&2
  exit 2
}
root= port= image= host_only=false
while [ $# -gt 0 ]; do
  case "$1" in
    --root) [ $# -ge 2 ] || usage; root=$2; shift 2 ;;
    --root=*) root=${1#*=}; shift ;;
    --port) [ $# -ge 2 ] || usage; port=$2; shift 2 ;;
    --port=*) port=${1#*=}; shift ;;
    --image) [ $# -ge 2 ] || usage; image=$2; shift 2 ;;
    --image=*) image=${1#*=}; shift ;;
    --host-only) host_only=true; shift ;;
    *) usage ;;
  esac
done
case "$port" in
  '') ;;
  *[!0-9]*) echo "error: --port must be a number" >&2; exit 2 ;;
  *) if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then echo "error: --port must be 1-65535" >&2; exit 2; fi ;;
esac

PYTHON=$(command -v python3 || true)
if [ -z "$PYTHON" ]; then
  echo "error: python3 is not installed; the collector needs it." >&2
  exit 1
fi

# Steps 3 to 5, on the host: the container never touches folders outside
# SparkRoot.
# A real folder at the link's path is someone's own skill: leave it.
link_skill() { # link_skill <name> <SparkRoot folder>
  link="$SKILLS/$1"
  if [ -L "$link" ] || [ ! -e "$link" ]; then
    ln -sfn "$root/$2" "$link"
    echo "skill: $link -> $root/$2"
  else
    echo "skill: $link exists and is not a link; left it alone. Move it away and rerun to link it."
  fi
}

# Asks for the folder that holds your projects and writes it as the one scan
# root. It is stored as typed (~ included), like the Settings page does; the
# collector expands ~ on the host.
ask_scan_root() {
  default=
  [ -d "$HOME/Projects" ] && default="~/Projects"
  while :; do
    if [ -n "$default" ]; then
      printf 'Which folder holds your projects? Spark scans it for spark.md files. [%s] ' "$default"
    else
      printf 'Which folder holds your projects? Spark scans it for spark.md files (Enter to skip): '
    fi
    read -r dir || dir=
    [ -n "$dir" ] || dir=$default
    if [ -z "$dir" ]; then
      echo "scan roots: skipped; add your project folders under Settings"
      return
    fi
    case "$dir" in
      */) [ "$dir" = / ] || dir=${dir%/} ;;
    esac
    case "$dir" in
      '~') path=$HOME ;;
      '~/'*) path="$HOME/${dir#\~/}" ;;
      /*) path=$dir ;;
      *) echo "  Give a full path, starting with / or ~/."; continue ;;
    esac
    if [ ! -d "$path" ]; then
      echo "  $path is not a folder."
      continue
    fi
    break
  done
  "$PYTHON" - "$roots_file" "$dir" <<'EOF'
import json, os, sys, tempfile
path, root = sys.argv[1], sys.argv[2]
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix=".scan_roots.json.", suffix=".tmp")
with os.fdopen(fd, "w") as f:
    json.dump([root], f, indent=2)
    f.write("\n")
os.replace(tmp, path)
EOF
  echo "scan roots: $dir"
}

host_steps() {
  # 3. Skill links.
  mkdir -p "$SKILLS"
  link_skill spark Skill
  link_skill spark-handoff HandoffSkill

  # 4. Scan root. Asked only on a terminal and only while none is set, so a
  # rerun never asks again; more are added under Settings.
  roots_file="$root/Config/scan_roots.json"
  if [ ! -f "$roots_file" ]; then
    echo "scan roots: $roots_file not found; add your project folders under Settings"
  elif "$PYTHON" -c 'import json, sys; sys.exit(0 if json.load(open(sys.argv[1])) else 1)' "$roots_file"; then
    echo "scan roots: already set; change them under Settings"
  elif [ ! -t 0 ]; then
    echo "scan roots: none set; add your project folders under Settings"
  else
    ask_scan_root
  fi

  # 5. Cron line. collector.log holds the last run's output only.
  # Quoted, so a SparkRoot path with spaces still works. cron runs this with sh.
  # A Spark line for another SparkRoot or python3 is replaced, not added to.
  CRON_LINE="*/15 * * * * '$PYTHON' '$root/collector.py' > '$root/collector.log' 2>&1"
  SPARK_LINE="/collector\.py' > '.*/collector\.log' 2>&1\$"
  if ! command -v crontab >/dev/null 2>&1; then
    echo "cron: crontab not found. Schedule this yourself:"
    echo "  $CRON_LINE"
  else
    current=$(crontab -l 2>/dev/null || true)
    if printf '%s\n' "$current" | grep -qxF "$CRON_LINE"; then
      echo "cron: already scheduled"
    else
      old=$(printf '%s\n' "$current" | grep -e "$SPARK_LINE" || true)
      { if [ -n "$current" ]; then printf '%s\n' "$current" | grep -v -e "$SPARK_LINE" || true; fi; echo "$CRON_LINE"; } | crontab -
      if [ -n "$old" ]; then
        echo "cron: replaced the old collector line:"
        echo "  $old"
      fi
      echo "cron: collector runs every 15 minutes, log in $root/collector.log"
    fi
  fi

  # The first run, so the cards don't wait for cron.
  if "$PYTHON" "$root/collector.py" > "$root/collector.log" 2>&1; then
    echo "collector: $(ls "$root/Projects" 2>/dev/null | grep -c '\.md$') project(s) in $root/Projects"
  else
    echo "collector: the first run failed; see $root/collector.log"
  fi
}

# A copy of this script inside SparkRoot sits next to collector.py.
here=$(cd "$(dirname "$0")" && pwd -P)

if $host_only; then
  if [ -n "$port$image" ]; then
    echo "error: --port and --image need the container; leave out --host-only" >&2
    exit 2
  fi
  root=${root:-$here}
  if [ ! -f "$root/collector.py" ] || [ ! -d "$root/Skill" ] || [ ! -d "$root/HandoffSkill" ]; then
    echo "error: $root has no collector.py, Skill/ or HandoffSkill/. Start the Spark container with this folder mounted first, or pass --root." >&2
    exit 1
  fi
  root=$(cd "$root" && pwd -P)
  host_steps
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is not installed." >&2
  exit 1
fi
# The existing container's options are the defaults for this run.
exists=false old_root= old_ip= old_port= old_image= old_env= old_name=
if docker container inspect "$NAME" >/dev/null 2>&1; then
  exists=true
  old_root=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/spark"}}{{.Source}}{{end}}{{end}}' "$NAME")
  binding=$(docker inspect -f '{{range $p, $b := .HostConfig.PortBindings}}{{if eq $p "9140/tcp"}}{{with index $b 0}}{{.HostIp}} {{.HostPort}}{{end}}{{end}}{{end}}' "$NAME")
  old_ip=${binding% *}
  old_port=${binding#* }
  old_image=$(docker inspect -f '{{.Image}}' "$NAME")
  old_name=$(docker inspect -f '{{.Config.Image}}' "$NAME")
  # Only the env set on the container, not the image's own ENV lines.
  image_env=$(docker image inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$old_image")
  old_env=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$NAME" | grep -vxF "$image_env" || true)
fi

if [ -z "$root" ]; then
  if [ -n "$old_root" ]; then
    root=$old_root
  elif [ -f "$here/collector.py" ]; then
    root=$here
  else
    root="$HOME/Documents/Spark"
  fi
fi
[ -n "$port" ] || port=${old_port:-9140}
ip=${old_ip:-127.0.0.1}
[ -n "$image" ] || image=${old_name:-$DEFAULT_IMAGE}

# A name without a registry path is a local build: docker would resolve
# `spark` to docker.io/library/spark, someone else's image.
case "$image" in
  */*)
    echo "image: pulling $image"
    if ! docker pull -q "$image" >/dev/null; then
      if docker image inspect "$image" >/dev/null 2>&1; then
        echo "image: pull failed; using the copy already here"
      else
        echo "error: could not pull $image" >&2
        exit 1
      fi
    fi ;;
  *)
    if ! docker image inspect "$image" >/dev/null 2>&1; then
      echo "error: no local image '$image'. Build it in the repo (docker build -t $image .) or leave out --image." >&2
      exit 1
    fi ;;
esac

# 1. SparkRoot. If Docker created it, root would own it.
mkdir -p "$root"
root=$(cd "$root" && pwd -P)

# 2. Container. Login comes from this shell when set, else from the old container.
login_env=
for key in SPARK_USERNAME SPARK_PASSWORD; do
  eval "val=\${$key-}"
  if [ -n "$val" ]; then
    login_env="$login_env$key=$val
"
  fi
done
want_env=$old_env
if [ -n "$login_env" ]; then
  want_env=$(printf '%s\n' "$old_env" | grep -v -e '^SPARK_USERNAME=' -e '^SPARK_PASSWORD=' || true)
  want_env="$want_env
$login_env"
fi
sorted() { printf '%s\n' "$1" | grep -v '^$' | sort; }

reason=
if ! $exists; then
  reason=new
else
  old_root_real=$old_root
  if [ -n "$old_root" ] && [ -d "$old_root" ]; then old_root_real=$(cd "$old_root" && pwd -P); fi
  if [ "$old_name" != "$image" ]; then
    reason="the image changed from $old_name"
  elif [ "$old_image" != "$(docker image inspect -f '{{.Id}}' "$image")" ]; then
    reason="$image is newer"
  elif [ "$old_root_real" != "$root" ]; then
    reason="SparkRoot changed from $old_root"
  elif [ "$old_port" != "$port" ]; then
    reason="port changed from $old_port"
  elif [ "$(sorted "$old_env")" != "$(sorted "$want_env")" ]; then
    reason="login changed"
  fi
fi

if [ -n "$reason" ]; then
  if $exists; then
    echo "container: recreating, since $reason"
    docker rm -f "$NAME" >/dev/null
  fi
  # Each value goes in through this shell's environment, so a password never
  # shows on docker's command line.
  set --
  while IFS= read -r line; do
    key=${line%%=*}
    case "$key" in
      '' | [0-9]* | *[!A-Za-z0-9_]*) continue ;;
    esac
    export "$line"
    set -- "$@" -e "$key"
  done <<EOF
$want_env
EOF
  case "$ip" in *:*) bind="[$ip]" ;; *) bind=$ip ;; esac
  docker run -d --name "$NAME" --restart unless-stopped \
    --user "$(id -u):$(id -g)" \
    -p "$bind:$port:9140" \
    -v "$root:/spark" \
    "$@" "$image" >/dev/null
  echo "container: $NAME running, SparkRoot $root"
  if $exists && [ "${old_root_real:-$root}" != "$root" ]; then
    echo "container: the old SparkRoot $old_root was left as it was"
  fi
elif [ "$(docker inspect -f '{{.State.Running}}' "$NAME")" != true ]; then
  docker start "$NAME" >/dev/null
  echo "container: started $NAME"
else
  echo "container: $NAME already up to date"
fi

# The web app answers only after the container has filled SparkRoot.
case "$ip" in ''|0.0.0.0|::) probe=127.0.0.1 ;; *:*) probe="[$ip]" ;; *) probe=$ip ;; esac
url="http://$probe:$port/login"
if ! "$PYTHON" - "$url" <<'EOF'
import sys, time, urllib.error, urllib.request
for _ in range(60):
    try:
        urllib.request.urlopen(sys.argv[1], timeout=2)
        sys.exit(0)
    except urllib.error.HTTPError:
        sys.exit(0)
    except Exception:
        time.sleep(0.5)
sys.exit(1)
EOF
then
  echo "error: the dashboard did not answer at $url within 30 seconds. Container log:" >&2
  docker logs --tail 20 "$NAME" >&2
  exit 1
fi

host_steps
echo
echo "Spark is at http://localhost:$port."
