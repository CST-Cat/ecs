#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
  echo "freebsd-runtime: integration must run as an ordinary user" >&2
  exit 1
fi
if [ "$(uname -s)" != FreeBSD ]; then
  echo "freebsd-runtime: expected FreeBSD" >&2
  exit 1
fi

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

freebsd-version
uname -a
go version

work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-freebsd-runtime.XXXXXX")
trap 'rm -rf -- "$work"' EXIT HUP INT TERM

# Runtime CI answers whether the FreeBSD product path actually works. Build the
# real binary and execute a local-only system report as an ordinary user. System
# inventory may legitimately be warning-level when optional hardware/cloud facts
# are unavailable, so this smoke checks successful execution and report creation;
# the native system test below asserts the required FreeBSD core facts.
go build -o "$work/ecs" ./cmd/ecs
mkdir -p "$work/reports"
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
  exit 1
}

# Keep this deliberately functional. Broad unit/race/parser regressions run on
# Linux already. FreeBSD runtime CI only gates the native system inventory and
# the real base-system ping/traceroute/backtrace paths that define FreeBSD
# support. Frozen benchmark binaries have their own native functional smoke in
# freebsd-tools.yml.
go test -tags=integration ./internal/probe \
  -run '^(TestFreeBSDSystemResultUsesNativeMethodsAndUnavailableLinuxFacts|TestIntegrationPingLoopback|TestIntegrationFreeBSDTracerouteCanonicalRoute|TestIntegrationFreeBSDBacktraceCanonical)$' \
  -timeout 10m \
  -count=1 \
  -v
