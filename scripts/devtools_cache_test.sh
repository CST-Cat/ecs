#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repo_root/scripts/lib/common.sh"

test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-devtools-cache.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fake_binary="$test_root/staticcheck"
fake_lock="$test_root/staticcheck.lock"
printf '#!/bin/sh\nexit 0\n' >"$fake_binary"
chmod 0755 "$fake_binary"
printf '%s\n' "$(ecs_devtools_lock_state)" >"$fake_lock"

ecs_devtool_cache_valid staticcheck "$fake_binary" "$fake_lock" ||
  { echo "devtools-cache: matching module lock was rejected" >&2; exit 1; }

printf 'stale\n' >"$fake_lock"
if ecs_devtool_cache_valid staticcheck "$fake_binary" "$fake_lock"; then
  echo "devtools-cache: stale module lock was accepted" >&2
  exit 1
fi

echo "devtools-cache: module lock invalidation passed"
