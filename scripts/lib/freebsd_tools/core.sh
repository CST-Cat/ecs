# shellcheck shell=bash

phase_sysbench() {
  require_sources
  echo "building sysbench $sysbench_tag ($sysbench_commit)"
  (
    cd "$sysbench_src"
    ./autogen.sh
    # shellcheck disable=SC2086
    CC="$cc_command" CXX="$cxx_command" LDFLAGS=-static \
      ./configure \
      ${configure_cross_args[@]+"${configure_cross_args[@]}"} \
      --prefix="$work/sysbench-prefix" \
      --without-gcc-arch \
      --with-system-luajit \
      --with-system-ck \
      '--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed' \
      --without-mysql \
      --without-pgsql \
      --without-drizzle \
      --without-attachsql \
      --without-oracle
    gmake -j"$jobs"
  )
  cp "$sysbench_src/src/sysbench" "$stage/bin/sysbench"
  validate_binary sysbench

  # LuaJIT's runtime code generation reliably aborts under qemu-user
  # (SIGABRT / "uncaught target signal 6"), even for --version. ELF identity
  # and static linkage are already proven above. Functional smoke for the
  # cross path belongs on a real FreeBSD/arm64 guest, not the emulator.
  if [[ "$toolchain_mode" == cross ]]; then
    echo "sysbench: static FreeBSD/arm64 ELF validated; skipping qemu-user functional smoke (LuaJIT)"
    return 0
  fi

  local version short_commit expected
  version=$(lock_tool_field sysbench version)
  short_commit=$(git -C "$sysbench_src" rev-parse --short HEAD)
  set +e
  run_target "$stage/bin/sysbench" --version >"$work/sysbench-version.txt" 2>&1
  version_status=$?
  set -e
  if [[ "$version_status" -ne 0 ]]; then
    echo "sysbench --version exited $version_status under ${smoke_runner}:" >&2
    cat "$work/sysbench-version.txt" >&2 || true
    die "sysbench version smoke failed under the target runner"
  fi
  expected="sysbench $version-$short_commit"
  grep -Fx "$expected" "$work/sysbench-version.txt" >/dev/null || {
    cat "$work/sysbench-version.txt" >&2
    die "sysbench version smoke did not report $expected"
  }
  run_target "$stage/bin/sysbench" cpu --cpu-max-prime=1000 --threads=1 run >"$work/sysbench-smoke.txt" 2>&1
  grep -Eq 'events per second|total time' "$work/sysbench-smoke.txt" || {
    cat "$work/sysbench-smoke.txt" >&2
    die 'sysbench CPU smoke output was not recognized'
  }
}

phase_zstd() {
  require_sources
  echo "building zstd $zstd_tag ($zstd_commit)"
  gmake -C "$zstd_src/programs" -j"$jobs" zstd-release \
    CC="$cc_command" MOREFLAGS='-O3 -static -static-libgcc -DZSTD_NODICT -DZSTD_NOTRACE' \
    HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
  cp "$zstd_src/programs/zstd" "$stage/bin/zstd"
  validate_binary zstd

  local version
  printf '%s\n' 'ECS FreeBSD zstd smoke input' >"$work/zstd-input"
  version=$(lock_tool_field zstd version)
  run_target "$stage/bin/zstd" --version >"$work/zstd-version.txt" 2>&1
  grep -Eq "v${version//./\\.}([^0-9]|$)" "$work/zstd-version.txt" ||
    die "zstd version smoke did not report $version"
  run_target "$stage/bin/zstd" -q -f "$work/zstd-input" -o "$work/zstd-output.zst"
  run_target "$stage/bin/zstd" -q -d -f "$work/zstd-output.zst" -o "$work/zstd-roundtrip"
  cmp "$work/zstd-input" "$work/zstd-roundtrip" || die 'zstd round trip failed'
}

phase_npb() {
  require_sources
  local npb_flags npb_compile_date
  npb_flags='-O3 -fopenmp -static'
  npb_compile_date=$(date -u -r "$SOURCE_DATE_EPOCH" '+%d %b %Y')

  # Official libgfortran (cross SDK or FreeBSD/amd64 Ports gcc14) supplies
  # ieee_arithmetic.mod. Probe it before the Class A build.
  cat >"$work/npb-ieee-probe.f90" <<'PROBE'
program ecs_npb_ieee_probe
  use, intrinsic :: ieee_arithmetic, only : ieee_is_nan
  implicit none
  real(kind=kind(0.0d0)) :: value
  value = 0.0d0
  if (ieee_is_nan(value)) error stop 1
end program ecs_npb_ieee_probe
PROBE
  "$fc_command" -O3 -static "$work/npb-ieee-probe.f90" -o "$work/npb-ieee-probe"
  run_target "$work/npb-ieee-probe" ||
    die 'compiler ieee_arithmetic intrinsic module failed its probe'

  echo "building NPB $npb_version OpenMP EP + FT Class A"
  cat >"$npb_src/config/make.def" <<MAKEDEF
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
UCC = $cc_command
BINDIR = ../bin
RAND = randi8
WTIME = wtime.c
MAKEDEF
  mkdir -p "$npb_src/bin"
  gmake -C "$npb_src/sys" all
  local benchmark benchmark_lower params
  for benchmark in EP FT; do
    benchmark_lower=${benchmark,,}
    (
      cd "$npb_src/$benchmark"
      ../sys/setparams "$benchmark_lower" A
      params=../"$benchmark"/npbparams.h
      sed -i '' "s@parameter (compiletime='[^']*')@parameter (compiletime='$npb_compile_date')@" "$params"
    )
  done
  gmake -C "$npb_src" -j"$jobs" ep CLASS=A
  gmake -C "$npb_src" -j"$jobs" ft CLASS=A
  cp "$npb_src/bin/ep.A.x" "$stage/bin/npb-ep"
  cp "$npb_src/bin/ft.A.x" "$stage/bin/npb-ft"
  validate_binary npb-ep
  validate_binary npb-ft

  local benchmark_upper npb_smoke_output
  for benchmark in ep ft; do
    (
      cd "$work"
      OMP_NUM_THREADS=1 OMP_DYNAMIC=FALSE OMP_PROC_BIND=close OMP_PLACES=cores \
        OMP_SCHEDULE=static OMP_DISPLAY_ENV=FALSE NPB_TIMER_FLAG=0 \
        run_target "$stage/bin/npb-$benchmark"
    ) >"$work/npb-$benchmark-smoke.txt" 2>&1
    npb_smoke_output="$work/npb-$benchmark-smoke.txt"
    benchmark_upper=${benchmark^^}
    grep -Eq "NAS Parallel Benchmarks \\(NPB3\\.4-OMP\\) - ${benchmark_upper} Benchmark" "$npb_smoke_output" || {
      cat "$npb_smoke_output" >&2
      die "NPB $benchmark smoke omitted the official header"
    }
    grep -Eq '^[[:space:]]*Class[[:space:]]*=[[:space:]]*A[[:space:]]*$' "$npb_smoke_output" || {
      cat "$npb_smoke_output" >&2
      die "NPB $benchmark smoke did not run the release Class A binary"
    }
    grep -Eq '^[[:space:]]*Total threads[[:space:]]*=[[:space:]]*1[[:space:]]*$' "$npb_smoke_output" || {
      cat "$npb_smoke_output" >&2
      die "NPB $benchmark smoke did not use one OpenMP thread"
    }
    grep -Eq '^[[:space:]]*Verification[[:space:]]*=[[:space:]]*SUCCESSFUL[[:space:]]*$' "$npb_smoke_output" || {
      cat "$npb_smoke_output" >&2
      die "NPB $benchmark smoke verification failed"
    }
    grep -Eq "^[[:space:]]*Version[[:space:]]*=[[:space:]]*${npb_version//./\\.}[[:space:]]*$" \
      "$npb_smoke_output" || die "NPB $benchmark reported the wrong version"
    grep -F "FC           = $fc_command" "$npb_smoke_output" >/dev/null ||
      die "NPB $benchmark smoke reported the wrong compiler"
    grep -F 'FFLAGS       = -O3 -fopenmp -static' "$npb_smoke_output" >/dev/null ||
      die "NPB $benchmark smoke reported unexpected compiler flags"
    grep -Eq '^[[:space:]]*RAND[[:space:]]*=[[:space:]]*randi8[[:space:]]*$' "$npb_smoke_output" ||
      die "NPB $benchmark smoke reported the wrong random generator"
  done
}
