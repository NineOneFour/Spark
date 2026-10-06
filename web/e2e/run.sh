#!/bin/sh
# Spark's end-to-end tests, run on demand. From anywhere:
#
#   web/e2e/run.sh                     each Spark is a web process
#   web/e2e/run.sh -docker             each Spark is a container from the image
#   web/e2e/run.sh -test.run TestPushAndPull    (go test flags go last)
#
# Go builds in the golang:1.27.1 container, so the host needs only Docker and
# python3 (for the collector). Builds and the Go cache go in .e2e/ at the
# repo root; the image is tagged spark:e2e, so it never replaces spark.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd -P)
WEB=$(dirname "$HERE")
REPO=$(dirname "$WEB")
OUT="$REPO/.e2e"
mkdir -p "$OUT/bin" "$OUT/go"

use_docker=""
if [ "${1:-}" = "-docker" ]; then
  use_docker=1
  shift
fi

docker run --rm -u "$(id -u):$(id -g)" \
  -e GOCACHE=/cache/build -e GOPATH=/cache/path -e CGO_ENABLED=0 \
  -v "$WEB":/src -v "$OUT/go":/cache -v "$OUT/bin":/out -w /src \
  golang:1.27.1 sh -c 'go vet -tags e2e ./... && go build -o /out/web . && go test -c -tags e2e -o /out/e2e.test ./e2e'

if [ -n "$use_docker" ]; then
  docker build -q -t spark:e2e "$REPO" >/dev/null
  set -- -image spark:e2e "$@"
else
  set -- -web "$OUT/bin/web" "$@"
fi

# go test runs a package from its own folder; the tests find collector.py
# relative to it.
cd "$HERE"
exec "$OUT/bin/e2e.test" -test.v "$@"
