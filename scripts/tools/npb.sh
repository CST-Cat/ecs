#!/usr/bin/env bash

ecs_tool_configure_npb_class() {
	local benchmark=$1
	local class=$2
	local benchmark_lower=${benchmark,,}
	local params="$npb_src/$benchmark/npbparams.h"
	(
		cd "$npb_src/$benchmark"
		../sys/setparams "$benchmark_lower" "$class"
	)
	sed -i "s@parameter (compiletime='[^']*')@parameter (compiletime='$npb_compile_date')@" "$params"
	grep -F "parameter (compiletime='$npb_compile_date')" "$params" >/dev/null ||
		die "could not pin NPB compile date in $params"
}

ecs_tool_build_npb() {
	echo "building NPB ${npb_version} OpenMP EP + FT Class A"
	npb_flags='-O3 -fopenmp -static'
	npb_compile_date=$(date -u -d "@$SOURCE_DATE_EPOCH" '+%d %b %Y')
	npb_build_flags=(
		"$fc_command"
		'-O3'
		'-fopenmp'
		'-static'
		'CLASS=A'
		'RAND=randi8'
		'OMP'
	)
	npb_build_flags_json=$(printf '%s\n' "${npb_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
	npb_gfortran_version=$("$fc_command" --version | sed -n '1p')
	cat >"$npb_src/config/make.def" <<EOF
FC = $fc_command
FLINK = $fc_command
F_LIB =
F_INC =
FFLAGS = $npb_flags
FLINKFLAGS = $npb_flags
CC = $cc_command
CLINK = $cc_command
C_LIB = -lm
C_INC =
CFLAGS = $npb_flags
CLINKFLAGS = $npb_flags
UCC = gcc
BINDIR = ../bin
RAND = randi8
WTIME = wtime.c
EOF
	mkdir -p "$npb_src/bin"
	make -C "$npb_src/sys" all

	ecs_tool_configure_npb_class EP A
	ecs_tool_configure_npb_class FT A
	make -C "$npb_src" -j"$jobs" ep CLASS=A
	make -C "$npb_src" -j"$jobs" ft CLASS=A
	cp "$npb_src/bin/ep.A.x" "$stage/bin/npb-ep"
	cp "$npb_src/bin/ft.A.x" "$stage/bin/npb-ft"

	npb_smoke_class=A
	npb_ep_smoke_bin="$stage/bin/npb-ep"
	npb_ft_smoke_bin="$stage/bin/npb-ft"
	if [[ "$build_mode" == cross ]]; then
		npb_smoke_class=S
		ecs_tool_configure_npb_class EP S
		make -C "$npb_src" -j"$jobs" ep CLASS=S
		ecs_tool_configure_npb_class FT S
		make -C "$npb_src" -j"$jobs" ft CLASS=S
		npb_ep_smoke_bin="$work/npb-ep-class-s-smoke"
		npb_ft_smoke_bin="$work/npb-ft-class-s-smoke"
		cp "$npb_src/bin/ep.S.x" "$npb_ep_smoke_bin"
		cp "$npb_src/bin/ft.S.x" "$npb_ft_smoke_bin"
		"$strip_command" --strip-unneeded "$npb_ep_smoke_bin" "$npb_ft_smoke_bin"
	fi
}

ecs_tool_smoke_npb() {
	local expected_version expected_version_regex
	expected_version=$(ecs_lock_tool_field npb-ep version)
	expected_version_regex=${expected_version//./\\.}
	mkdir -p "$work/npb-smoke-run"
	for npb_benchmark in EP FT; do
		case "$npb_benchmark" in
			EP) npb_smoke_bin=$npb_ep_smoke_bin ;;
			FT) npb_smoke_bin=$npb_ft_smoke_bin ;;
		esac
		(
			cd "$work/npb-smoke-run"
			OMP_NUM_THREADS=1 \
				OMP_DYNAMIC=FALSE \
				OMP_PROC_BIND=close \
				OMP_PLACES=cores \
				OMP_SCHEDULE=static \
				OMP_DISPLAY_ENV=FALSE \
				NPB_TIMER_FLAG=0 \
				run_target "$npb_smoke_bin"
		) >"$work/npb-${npb_benchmark,,}-smoke.txt" 2>&1
		npb_smoke_output="$work/npb-${npb_benchmark,,}-smoke.txt"
		grep -Eq "NAS Parallel Benchmarks \\(NPB3\\.4-OMP\\) - ${npb_benchmark} Benchmark" "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke omitted the official header"
		grep -Eq "^[[:space:]]*Class[[:space:]]*=[[:space:]]*${npb_smoke_class}[[:space:]]*$" "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke did not run Class ${npb_smoke_class}"
		grep -Eq '^[[:space:]]*Total threads[[:space:]]*=[[:space:]]*1[[:space:]]*$' "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke did not use one OpenMP thread"
		grep -Eq '^[[:space:]]*Verification[[:space:]]*=[[:space:]]*SUCCESSFUL[[:space:]]*$' "$npb_smoke_output" || die "NPB ${npb_benchmark} Class ${npb_smoke_class} verification failed"
		grep -Eq "^[[:space:]]*Version[[:space:]]*=[[:space:]]*${expected_version_regex}[[:space:]]*$" "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke reported the wrong version"
		grep -Eq '^[[:space:]]*FC[[:space:]]*=.*gfortran[[:space:]]*$' "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke reported the wrong target compiler"
		grep -F 'FFLAGS       = -O3 -fopenmp -static' "$npb_smoke_output" >/dev/null || die "NPB ${npb_benchmark} smoke reported unexpected compiler flags"
		grep -Eq '^[[:space:]]*RAND[[:space:]]*=[[:space:]]*randi8[[:space:]]*$' "$npb_smoke_output" || die "NPB ${npb_benchmark} smoke reported the wrong random generator"
	done
}
