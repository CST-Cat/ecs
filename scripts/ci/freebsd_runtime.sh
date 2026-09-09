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
# real binary and execute a local-only system report as an ordinary user before
# running the small set of platform-specific regressions below.
go build -o "$work/ecs" ./cmd/ecs
mkdir -p "$work/reports"
"$work/ecs" \
  --only system \
  --exposure local \
  --format json \
  --output "$work/reports" \
  --name system \
  --yes \
  --strict
[ -s "$work/reports/system.json" ] || {
  echo "freebsd-runtime: ecs --only system did not produce JSON" >&2
  exit 1
}

# Keep this deliberately narrow. Linux already owns the broad unit/race/parser
# suite. FreeBSD runtime CI validates native system collection, the shared
# command lifecycle regression that previously broke arm64, and the real base
# ping/traceroute/backtrace paths that define FreeBSD support.
go test -tags=integration ./internal/probe \
  -run '^(TestFreeBSDSystemResultUsesNativeMethodsAndUnavailableLinuxFacts|TestSpeedProducerBuildsStablePartialStatusDirectly|TestProbeCommandKillsProcessGroups|TestIntegrationPingLoopback|TestIntegrationFreeBSDTracerouteCanonicalRoute|TestIntegrationFreeBSDBacktraceCanonical)$' \
  -timeout 10m \
  -count=1 \
  -v
