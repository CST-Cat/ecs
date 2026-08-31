#!/usr/bin/env bash

ecs_tool_smoke_nexttrace() {
	run_target "$nexttrace_bin" --help >"$work/nexttrace-help.txt" 2>&1 || {
		cat "$work/nexttrace-help.txt" >&2
		die 'NextTrace help failed'
	}
	grep -Eq -- '--(json|raw)' "$work/nexttrace-help.txt" || {
		cat "$work/nexttrace-help.txt" >&2
		die 'NextTrace Tiny help omitted the JSON/raw flag'
	}
}

ecs_tool_build_nexttrace() {
	local expected_sha
	expected_sha=$(jq -er --arg arch "$arch" '
		.tools[] | select(.name == "nexttrace-tiny") | .asset_sha256[$arch] // empty
	' "$ECS_LOCK_FILE") || die "tools lock has no NextTrace Tiny digest for $arch"
	[[ "$nexttrace_sha" == "$expected_sha" ]] ||
		die "NextTrace Tiny $arch digest disagrees with tools lock: expected $expected_sha, got $nexttrace_sha"
	cp "$nexttrace_download" "$stage/bin/nexttrace-tiny"
}
