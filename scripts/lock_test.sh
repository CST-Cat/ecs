#!/usr/bin/env bash
set -euo pipefail

# Deterministic contract test for tools/lock.json. The build/release scripts
# consume this file through common.sh; keep the validation close to the lock so
# a missing pin fails before a long tool build starts.

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repo_root/scripts/lib/common.sh"

die() {
	echo "tools-lock: $*" >&2
	exit 1
}

[[ "$ECS_LOCK_SCHEMA_VERSION" == "ecs.tools.lock/v1" ]] || die "unexpected schema"
[[ "${#ECS_TARGETS[@]}" -eq 7 ]] || die "expected seven architecture targets"
[[ "${#ECS_TOOL_NAMES[@]}" -eq 10 ]] || die "expected ten locked tools"

jq -e '
  (.architectures | length == 7) and
  ([.architectures[].package] | length == 7 and length == (unique | length)) and
  (.tools | length == 10) and
  ([.tools[].name] | length == 10 and length == (unique | length)) and
  (all(.tools[]; (.name | length > 0) and (.upstream | startswith("http")))) and
  (all(.tools[] | select(.repository != null); (.tag | length > 0) and (.commit | test("^[0-9a-f]{40}$")))) and
  (.corpus.name == "ecs-silesia-v1.corpus") and
  (.corpus.bytes == 211938580) and
  (.corpus.sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.source_sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.order | length == 12)
' "$ECS_LOCK_FILE" >/dev/null || die "lock contents failed the schema invariants"

if grep -Fq 'tools/lock.json' "$repo_root/run.sh"; then
	die "public run.sh must not depend on the repository tools lock"
fi

echo "tools-lock: lock schema and build facts are valid"
