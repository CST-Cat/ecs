#!/bin/sh
set -u

if [ "$(id -u)" -eq 0 ]; then
  echo "freebsd-runtime: integration must run as an ordinary user" >&2
  exit 1
fi
if [ "$(uname -s)" != FreeBSD ]; then
  echo "freebsd-runtime: expected FreeBSD" >&2
  exit 1
fi

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd) || exit 1
cd "$repo_root" || exit 1

freebsd-version
uname -a
go version

work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-freebsd-runtime.XXXXXX") || exit 1
trap 'rm -rf -- "$work"' EXIT HUP INT TERM

failures=0
results=""

run_check() {
  name=$1
  shift

  printf '\n===== %s =====\n' "$name"
  if "$@"; then
    printf '[PASS] %s\n' "$name"
    results="${results}${name}=PASS\n"
    return 0
  else
    status=$?
  fi

  printf '[FAIL] %s (exit %s)\n' "$name" "$status" >&2
  results="${results}${name}=FAIL\n"
  failures=$((failures + 1))
  return 0
}

check_build() {
  go build -o "$work/ecs" ./cmd/ecs
}

check_system() {
  mkdir -p "$work/reports" || return 1
  rm -f "$work/reports/system.json"

  "$work/ecs" \
    --only system \
    --exposure local \
    --format json \
    --output "$work/reports" \
    --name system \
    --yes \
    --no-color || return 1

  if [ ! -s "$work/reports/system.json" ]; then
    echo "freebsd-runtime: ecs --only system did not produce JSON" >&2
    return 1
  fi

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

# One VM, five product-facing failure domains. A failure in one domain must not
# hide the state of the others; collect every result and fail once at the end.
run_check BUILD check_build
run_check SYSTEM check_system
run_check PING check_ping
run_check ROUTE check_route
run_check BACKTRACE check_backtrace

printf '\n===== FreeBSD runtime summary =====\n'
printf '%b' "$results"

if [ "$failures" -ne 0 ]; then
  printf 'freebsd-runtime: %d functional check(s) failed\n' "$failures" >&2
  exit 1
fi

printf 'freebsd-runtime: all functional checks passed\n'
