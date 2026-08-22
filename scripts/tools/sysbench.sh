#!/usr/bin/env bash

ecs_tool_build_sysbench() {
	echo "building sysbench ${sysbench_tag} (${sysbench_commit})"
	(
		cd "$sysbench_src"
		./autogen.sh
		./configure --help >"$work/sysbench-configure-help.txt"
	)

	sysbench_configure_args=(
		"${configure_cross_args[@]}"
		"--prefix=$work/sysbench-prefix"
		'--with-system-luajit'
		'--with-system-ck'
		'--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed'
	)
	sysbench_luajit_version=$(pkg-config --modversion luajit) ||
		die 'target container is missing the LuaJIT pkg-config metadata'
	sysbench_ck_version=$(pkg-config --modversion ck) ||
		die 'target container is missing the Concurrency Kit pkg-config metadata'
	sysbench_manifest_flags=(
		"${configure_cross_args[@]}"
		'--with-system-luajit'
		'--with-system-ck'
		'--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed'
	)
	sysbench_disabled_features=('database-drivers')
	for driver in mysql pgsql drizzle attachsql oracle; do
		option="--without-${driver}"
		if grep -Fq -- "$option" "$work/sysbench-configure-help.txt" ||
			grep -Eq "AC_ARG_WITH[[:space:]]*\\([[:space:]]*\\[?${driver}(\\]|,|[[:space:]])" \
				"$sysbench_src/configure.ac"; then
			sysbench_configure_args+=("$option")
			sysbench_manifest_flags+=("$option")
			sysbench_disabled_features+=("$driver")
		else
			sysbench_disabled_features+=("${driver}-driver-not-supported-by-upstream")
		fi
	done
	sysbench_build_flags_json=$(printf '%s\n' "${sysbench_manifest_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
	sysbench_disabled_features_json=$(printf '%s\n' "${sysbench_disabled_features[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')

	(
		cd "$sysbench_src"
		./configure "${sysbench_configure_args[@]}"
		make -j"$jobs"
	)
	cp "$sysbench_src/src/sysbench" "$stage/bin/sysbench"
}

ecs_tool_smoke_sysbench() {
	run_target "$sysbench_bin" --version
	run_target "$sysbench_bin" cpu --cpu-max-prime=1000 --threads=1 run >"$work/sysbench-smoke.txt"
	grep -Eq 'events per second|total time' "$work/sysbench-smoke.txt" || {
		cat "$work/sysbench-smoke.txt" >&2
		die 'sysbench CPU smoke output was not recognized'
	}
}
