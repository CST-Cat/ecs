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
[[ "${#ECS_TARGETS[@]}" -eq 9 ]] || die "expected nine platform targets"
[[ "${#ECS_LINUX_TARGETS[@]}" -eq 7 ]] || die "expected seven Linux targets"
[[ "${#ECS_FREEBSD_TARGETS[@]}" -eq 2 ]] || die "expected two FreeBSD targets"
[[ "${#ECS_TOOL_NAMES[@]}" -eq 10 ]] || die "expected ten locked tools"

bundle_file="$repo_root/tools/BUNDLE"
[[ -f "$bundle_file" ]] || die "missing tools/BUNDLE"
[[ "$(wc -l < "$bundle_file")" -eq 1 ]] || die "tools/BUNDLE must contain exactly one line"
bundle_name=$(<"$bundle_file")
[[ "$bundle_name" =~ ^bundle-v[1-9][0-9]*$ ]] || die "invalid tools/BUNDLE"

jq -e '
  (.architectures | length == 9) and
  ([.architectures[].target] | length == 9 and length == (unique | length)) and
  (all(.architectures[]; (.target | test("^(linux|freebsd)_[a-z0-9]+$")) and (.goos | IN("linux", "freebsd")))) and
  ([.architectures[] | select(.goos == "linux")] | length == 7) and
  ([.architectures[] | select(.goos == "freebsd")] | length == 2) and
  ([.architectures[] | select(.goos == "freebsd") | .goarch] | sort == ["amd64", "arm64"]) and
  ([.architectures[] | select(.goos == "freebsd") | .openssl_target] | sort == ["BSD-aarch64", "BSD-x86_64"]) and
  ([.architectures[] | select(.goos == "linux") | .package] | length == 7 and length == (unique | length)) and
  (.tools | length == 10) and
  ([.tools[].name] | length == 10 and length == (unique | length)) and
  (all(.tools[]; (.name | length > 0) and (.upstream | startswith("http")))) and
  (all(.tools[] | select(.repository != null); (.tag | length > 0) and (.commit | test("^[0-9a-f]{40}$")))) and
  ((.tools[] | select(.name == "nexttrace-tiny") | .asset_sha256) as $digests |
    ($digests | type == "object") and
    (($digests | keys | sort) == ([.architectures[] | select(.goos == "linux") | .package] | sort)) and
    (all($digests[]; test("^[0-9a-f]{64}$")))) and
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
