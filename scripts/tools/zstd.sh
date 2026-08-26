#!/usr/bin/env bash

ecs_tool_build_zstd() {
	echo "building zstd ${zstd_tag} (${zstd_commit})"
	zstd_build_flags=(
		"$cc_command"
		'-O3'
		'-static'
		'-static-libgcc'
		'-DZSTD_NODICT'
		'-DZSTD_NOTRACE'
		'HAVE_ZLIB=0'
		'HAVE_LZMA=0'
		'HAVE_LZ4=0'
		'ZSTD_LEGACY_SUPPORT=0'
	)
	zstd_build_flags_json=$(printf '%s\n' "${zstd_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
	make -C "$zstd_src/programs" -j"$jobs" zstd-release \
		CC="$cc_command" \
		MOREFLAGS='-O3 -static -static-libgcc -DZSTD_NODICT -DZSTD_NOTRACE' \
		HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
	cp "$zstd_src/programs/zstd" "$stage/bin/zstd"
}

ecs_tool_smoke_zstd() {
	local expected_version expected_version_regex
	expected_version=$(ecs_lock_tool_field zstd version)
	expected_version_regex=${expected_version//./\\.}
	run_target "$zstd_bin" --version >"$work/zstd-version.txt" 2>&1
	grep -Eq "v${expected_version_regex}([^0-9]|$)" "$work/zstd-version.txt" || {
		cat "$work/zstd-version.txt" >&2
		die "zstd version smoke did not report v${expected_version}"
	}
	head -c 1048576 "$zstd_corpus_path" >"$work/zstd-smoke.corpus"
	run_target "$zstd_bin" -q -b3 -i1 -T1 "$work/zstd-smoke.corpus" >"$work/zstd-smoke.txt" 2>&1
	grep -Eq "bench ${expected_version_regex}.*input 1048576 bytes, 1 seconds" "$work/zstd-smoke.txt" || {
		cat "$work/zstd-smoke.txt" >&2
		die 'zstd benchmark smoke output was not recognized'
	}
	grep -Eq '^-3[[:space:]].*MB/s[[:space:]].*MB/s' "$work/zstd-smoke.txt" || {
		cat "$work/zstd-smoke.txt" >&2
		die 'zstd benchmark smoke omitted compression/decompression throughput'
	}
}
