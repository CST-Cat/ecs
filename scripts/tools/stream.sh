#!/usr/bin/env bash

ecs_tool_build_stream() {
	echo "building official STREAM ${stream_revision}"
	stream_array_size=$ECS_STREAM_ARRAY_SIZE
	stream_ntimes=$ECS_STREAM_NTIMES
	stream_build_flags=("$cc_command" "${ECS_STREAM_COMPILE_FLAGS[@]}")
	stream_build_flags_json=$(printf '%s\n' "${stream_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
	ecs_stream_compile "$stream_src" "$stage/bin/stream" "$cc_command"
}

ecs_tool_smoke_stream() {
	stream_nt_threads=${STREAM_NT_THREADS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
	[[ "$stream_nt_threads" =~ ^[1-9][0-9]*$ ]] || die "invalid STREAM_NT_THREADS=$stream_nt_threads"
	for stream_threads in 1 "$stream_nt_threads"; do
		stream_context=nt
		[[ "$stream_threads" -eq 1 ]] && stream_context=1t
		OMP_NUM_THREADS="$stream_threads" run_target "$stream_bin" >"$work/stream-${stream_context}.txt"
	done
	cat "$work/stream-1t.txt" "$work/stream-nt.txt"
	for stream_context in 1t nt; do
		for kernel in Copy Scale Add Triad; do
			grep -q "${kernel}:" "$work/stream-${stream_context}.txt" || die "STREAM ${stream_context} output omitted ${kernel}"
		done
		grep -q 'Solution Validates' "$work/stream-${stream_context}.txt" || die "STREAM ${stream_context} validation failed"
	done
}
