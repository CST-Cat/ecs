#!/usr/bin/env bash
# Shared helpers for the Linux-hosted FreeBSD GNU/OpenMP benchmark builds
# (Stage 5: NPB EP/FT + STREAM via the Stage 4 GNU SDK).
#
# common.sh is needed here (in addition to scripts/lib/common.sh) because the
# GNU stage has its own toolchain plumbing: it consumes a relocated Stage 4
# SDK artifact instead of a host clang, and the structural contract differs
# (libgomp instead of no-OpenMP, GNU target triple instead of clang wrappers).

set -euo pipefail

ecs_freebsd_gnu_die() {
  echo "build-tools-freebsd-gnu: $*" >&2
  exit 1
}

# upload-artifact v4 does not preserve file permissions: once the Stage 4 SDK
# artifact has been downloaded, the driver, cc1/f951/collect2 and the target
# binutils have lost their executable bit. Restore it before any probe.
ecs_freebsd_gnu_restore_sdk_exec_bits() {
  local sdk_prefix=$1 triple=$2 dir
  for dir in "$sdk_prefix/bin" "$sdk_prefix/libexec" "$sdk_prefix/$triple/bin"; do
    [[ -d "$dir" ]] || continue
    find "$dir" -type f -exec chmod +x {} +
  done
}

ecs_freebsd_gnu_validate_sdk() {
  local sdk_prefix=$1 triple=$2
  [[ -d "$sdk_prefix" ]] ||
    ecs_freebsd_gnu_die "GNU SDK prefix does not exist: $sdk_prefix"
  [[ -x "$sdk_prefix/bin/${triple}-gcc" ]] ||
    ecs_freebsd_gnu_die "target gcc missing in SDK prefix: $sdk_prefix/bin/${triple}-gcc"
  [[ -x "$sdk_prefix/bin/${triple}-gfortran" ]] ||
    ecs_freebsd_gnu_die "target gfortran missing in SDK prefix: $sdk_prefix/bin/${triple}-gfortran"
  local gcc_report
  gcc_report=$("$sdk_prefix/bin/${triple}-gcc" --version | head -n1)
  echo "sdk target gcc: $gcc_report" >&2
}

# Create gcc/gfortran wrappers that always pass the freshly installed FreeBSD
# sysroot. The SDK driver embeds the sysroot path of the machine it was built
# on; passing --sysroot explicitly makes the artifact relocation-safe.
# Real compilers are invoked by absolute path: a bare `gcc`/`gfortran` on PATH
# could resolve back to the wrapper and recurse until ARG_MAX (same lesson as
# the C-tools clang wrappers).
ecs_freebsd_gnu_write_wrappers() {
  local work=$1 sdk_prefix=$2 triple=$3 sysroot=$4
  local bin="$work/gnu-wrap"
  local real_gcc="$sdk_prefix/bin/${triple}-gcc"
  local real_gfortran="$sdk_prefix/bin/${triple}-gfortran"
  mkdir -p "$bin"
  cat >"$bin/gcc" <<EOF
#!/usr/bin/env bash
exec $real_gcc --sysroot=$sysroot "\$@"
EOF
  cat >"$bin/gfortran" <<EOF
#!/usr/bin/env bash
exec $real_gfortran --sysroot=$sysroot "\$@"
EOF
  chmod +x "$bin/gcc" "$bin/gfortran"
  echo "$bin"
}

# Probe the relocated SDK with the exact environment shape the benchmarks
# will use: static C and Fortran OpenMP hellos against the fresh sysroot.
ecs_freebsd_gnu_probe_wrappers() {
  local work=$1 file_machine=$2
  local probe="$work/wrapper-probe"
  mkdir -p "$probe"

  cat >"$probe/omp.c" <<'EOF'
#include <stdio.h>
int main(void) {
    int n = 0;
#pragma omp parallel
    {
#pragma omp atomic
        n += 1;
    }
    printf("gnu-c-openmp-threads=%d\n", n);
    return n > 0 ? 0 : 1;
}
EOF
  "$work/gnu-wrap/gcc" -O2 -fopenmp -static -o "$probe/omp-c" "$probe/omp.c" ||
    ecs_freebsd_gnu_die "wrapper probe failed: static C OpenMP hello did not compile"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$probe/omp-c" "$file_machine"

  cat >"$probe/omp.f90" <<'EOF'
program ompf
  use omp_lib
  integer :: n
  n = 0
!$omp parallel
!$omp atomic
  n = n + 1
!$omp end parallel
  print *, 'gnu-fortran-openmp-threads=', n
end program ompf
EOF
  "$work/gnu-wrap/gfortran" -O2 -fopenmp -static -o "$probe/omp-f" "$probe/omp.f90" ||
    ecs_freebsd_gnu_die "wrapper probe failed: static Fortran OpenMP hello did not compile"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$probe/omp-f" "$file_machine"

  echo "wrapper probe ok: static FreeBSD OpenMP C + Fortran hellos" >&2
}

# file_machine is the token `file` prints for the target CPU: x86-64 for
# freebsd_amd64, aarch64 for freebsd_arm64 (empirically fixed by the Stage 4
# probe logs). The FreeBSD identity comes either from the ELF OS/ABI byte
# (amd64: "version 1 (FreeBSD)") or from the FreeBSD ABI note (arm64 GNU ld
# keeps OS/ABI at 0 and `file` prints "for FreeBSD <rel>, FreeBSD-style").
ecs_freebsd_gnu_assert_static_freebsd_elf() {
  local bin=$1 file_machine=$2
  [[ -x "$bin" ]] || ecs_freebsd_gnu_die "missing executable: $bin"
  local file_out
  file_out=$(file "$bin")
  echo "verify $bin: $file_out" >&2
  case "$file_out" in
    *"$file_machine"*) ;;
    *) ecs_freebsd_gnu_die "$bin architecture is not $file_machine" ;;
  esac
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_gnu_die "$bin is not a FreeBSD ELF" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_gnu_die "$bin is not static" ;;
  esac
  if readelf -d "$bin" 2>/dev/null | grep -q NEEDED; then
    ecs_freebsd_gnu_die "$bin has dynamic NEEDED entries"
  fi
  if strings "$bin" 2>/dev/null | grep -q 'GLIBC_'; then
    ecs_freebsd_gnu_die "$bin contains glibc symbols"
  fi
}

# Structural libgomp proof: GNU libgomp symbols are statically linked into the
# binary; LLVM libomp's __kmpc_* runtime symbols must be absent.
ecs_freebsd_gnu_assert_libgomp() {
  local bin=$1 gomp kmpc
  gomp=$(nm "$bin" 2>/dev/null | grep -c ' GOMP_' || true)
  kmpc=$(nm "$bin" 2>/dev/null | grep -c '__kmpc_' || true)
  [[ "$gomp" -gt 0 ]] ||
    ecs_freebsd_gnu_die "$bin has no GOMP_ symbols (libgomp runtime missing)"
  [[ "$kmpc" -eq 0 ]] ||
    ecs_freebsd_gnu_die "$bin contains LLVM libomp (__kmpc_) symbols"
}

# Per-tool provenance records. Every tool carries the full Stage 5 contract:
# gcc/14.2.0 cross on ubuntu-24.04 with libgomp, plus its pinned source URL
# and SHA-256 (NPB for npb-ep/npb-ft, STREAM for stream).
ecs_freebsd_gnu_write_provenance() {
  local stage=$1 target=$2 triple=$3 gcc_version=$4
  local npb_url=$5 npb_sha=$6 stream_url=$7 stream_sha=$8
  local bin="$stage/bin"
  local -a records=()
  local name sha source_json
  for name in npb-ep npb-ft stream; do
    [[ -x "$bin/$name" ]] || ecs_freebsd_gnu_die "provenance: missing binary $name"
    sha=$(sha256sum "$bin/$name" | awk '{print $1}')
    case "$name" in
      npb-ep | npb-ft)
        source_json=$(jq -cn --arg url "$npb_url" --arg sha "$npb_sha" \
          '{name:"NPB",version:"3.4.4",url:$url,sha256:$sha}')
        ;;
      stream)
        source_json=$(jq -cn --arg url "$stream_url" --arg sha "$stream_sha" \
          '{name:"STREAM",url:$url,sha256:$sha}')
        ;;
    esac
    records+=("$(jq -cn \
      --arg name "$name" --arg sha "$sha" --arg triple "$triple" \
      --arg gcc "$gcc_version" --argjson source "$source_json" \
      '{name:$name,sha256:$sha,compiler_family:"gcc",compiler_version:$gcc,target_triple:$triple,build_host:"ubuntu-24.04",openmp_runtime:"libgomp",source:$source}')")
  done
  jq -n \
    --arg target "$target" --arg triple "$triple" --arg gcc "$gcc_version" \
    --argjson tools "$(printf '%s\n' "${records[@]}" | jq -s .)" \
    '{target:$target,target_triple:$triple,build_host:"ubuntu-24.04",toolchain_mode:"cross",compiler_family:"gcc",compiler_version:$gcc,openmp_runtime:"libgomp",tools:$tools}' \
    >"$stage/provenance.json"
}
