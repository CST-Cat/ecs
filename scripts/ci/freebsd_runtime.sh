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

artifacts="$repo_root/.ci/freebsd-runtime"
ecs="$artifacts/ecs"
probe_test="$artifacts/probe.test"
work="${TMPDIR:-/tmp}/ecs-freebsd-runtime"
command=${1:-}

require_artifacts() {
  [ -x "$ecs" ] || {
    echo "freebsd-runtime: BUILD did not provide executable $ecs" >&2
    return 1
  }
  [ -x "$probe_test" ] || {
    echo "freebsd-runtime: BUILD did not provide executable $probe_test" >&2
    return 1
  }
}

run_probe_test() {
  pattern=$1
  require_artifacts
  "$probe_test" \
    -test.run="$pattern" \
    -test.timeout=5m \
    -test.count=1 \
    -test.v
}

check_system() {
  require_artifacts
  rm -rf -- "$work"
  mkdir -p "$work/reports"

  freebsd-version
  uname -a

  "$ecs" \
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

  run_probe_test '^TestFreeBSDSystemResultUsesNativeMethodsAndUnavailableLinuxFacts$'
}

check_ping() {
  run_probe_test '^TestIntegrationPingLoopback$'
}

check_route() {
  run_probe_test '^TestIntegrationFreeBSDTracerouteCanonicalRoute$'
}

check_backtrace() {
  run_probe_test '^TestIntegrationFreeBSDBacktraceCanonical$'
}

case "$command" in
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
    echo "usage: $0 {system|ping|route|backtrace}" >&2
    exit 2
    ;;
esac
