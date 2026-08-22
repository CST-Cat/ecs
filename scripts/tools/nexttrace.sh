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
	cp "$nexttrace_download" "$stage/bin/nexttrace-tiny"
}
