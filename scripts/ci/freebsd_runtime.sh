#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
  echo "freebsd-runtime: checks must run as an ordinary user" >&2
  exit 1
fi
if [ "$(uname -s)" != FreeBSD ]; then
  echo "freebsd-runtime: expected FreeBSD" >&2
  exit 1
fi

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

work="${TMPDIR:-/tmp}/ecs-freebsd-runtime"
command=${1:-}

check_build() {
  rm -rf -- "$work"
  mkdir -p "$work"
  freebsd-version
  uname -a
  go version
  go build -o "$work/ecs" ./cmd/ecs
}

check_system() {
  [ -x "$work/ecs" ] || {
    echo "freebsd-runtime: BUILD did not produce $work/ecs" >&2
    return 1
  }

  mkdir -p "$work/reports"
  rm -f "$work/reports/system.json"
  "$work/ecs" \
    --only system \
    --exposure local \
    --format json \
    --output "$work/reports" \
    --name system \
    --yes \
    --no-color

  [ -s "$work/reports/system.json" ] || {
    echo "freebsd-runtime: ecs --only system did not produce JSON" >&2
    return 1
  }

  go test -tags=integration ./internal/probe \
    -run '^TestFreeBSDSystemResultUsesNativeMethodsAndUnavailableLinuxFacts$' \
    -timeout 5m \
    -count=1 \
    -v
}

check_ping() {
  go test -tags=integration ./internal/probe \
    -run '^TestIntegrationPingLoopback$' \
    -timeout 5m \
    -count=1 \
    -v
}

check_route() {
  go test -tags=integration ./internal/probe \
    -run '^TestIntegrationFreeBSDTracerouteCanonicalRoute$' \
    -timeout 5m \
    -count=1 \
    -v
}

check_backtrace() {
  go test -tags=integration ./internal/probe \
    -run '^TestIntegrationFreeBSDBacktraceCanonical$' \
    -timeout 5m \
    -count=1 \
    -v
}

case "$command" in
  build)
    check_build
    ;;
  system)
    check_system
    ;;
  ping)
    check_ping
    ;;
  route)
    check_route
    ;;
  backtrace)
    check_backtrace
    ;;
  *)
    echo "usage: $0 {build|system|ping|route|backtrace}" >&2
    exit 2
    ;;
esac
