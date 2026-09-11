#!/usr/bin/env bash
# Shared helpers for Linux-hosted FreeBSD C-tools builds (Clang + LLD).

set -euo pipefail

ecs_freebsd_c_die() {
  echo "build-tools-freebsd-c: $*" >&2
  exit 1
}

# Create clang/clang++ wrappers that always target the FreeBSD sysroot.
# Real compilers are invoked by absolute path: a bare `clang` would resolve
# back to this wrapper via PATH and recurse until ARG_MAX.
# The wrappers are probed with the same CC/CFLAGS/LDFLAGS/PKG_CONFIG_PATH
# environment that configure will later see.
ecs_freebsd_c_write_wrappers() {
  local work=$1 triple=$2 sysroot=$3
  local bin="$work/clang-wrap"
  local real_clang real_clangxx
  real_clang=$(command -v clang) || ecs_freebsd_c_die "clang not found before writing wrappers"
  real_clangxx=$(command -v clang++) || ecs_freebsd_c_die "clang++ not found before writing wrappers"
  mkdir -p "$bin"
  cat >"$bin/clang" <<EOF
#!/usr/bin/env bash
exec $real_clang --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  cat >"$bin/clang++" <<EOF
#!/usr/bin/env bash
exec $real_clangxx --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  cat >"$bin/cpp" <<EOF
#!/usr/bin/env bash
exec $real_clang -E --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  chmod +x "$bin/clang" "$bin/clang++" "$bin/cpp"
  echo "$bin"
}

# Probe must use the exact toolchain environment later configure will use.
ecs_freebsd_c_probe_wrappers() {
  local work=$1
  local probe="$work/wrapper-probe"
  mkdir -p "$probe"
  cat >"$probe/hello.c" <<'EOF'
#include <stdio.h>
int main(void) { puts("wrapper-ok"); return 0; }
EOF
  # Intentionally pass the same exported CC/CFLAGS/LDFLAGS used by builders.
  "$CC" $CFLAGS $CPPFLAGS -o "$probe/hello" "$probe/hello.c" $LDFLAGS ||
    ecs_freebsd_c_die "wrapper probe failed with CC=$CC CFLAGS=$CFLAGS LDFLAGS=$LDFLAGS"
  [[ -x "$probe/hello" ]] || ecs_freebsd_c_die "wrapper probe produced no binary"
  local file_out
  file_out=$(file "$probe/hello")
  echo "wrapper probe file: $file_out" >&2
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_c_die "wrapper probe is not a FreeBSD ELF: $file_out" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_c_die "wrapper probe is not static: $file_out" ;;
  esac
}

ecs_freebsd_c_assert_static_freebsd_elf() {
  local bin=$1 elf_machine=$2
  [[ -x "$bin" ]] || ecs_freebsd_c_die "missing executable: $bin"
  local file_out
  file_out=$(file "$bin")
  echo "verify $bin: $file_out" >&2
  case "$file_out" in
    *"$elf_machine"*) ;;
    *) ecs_freebsd_c_die "$bin architecture is not $elf_machine" ;;
  esac
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_c_die "$bin is not a FreeBSD ELF" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_c_die "$bin is not static" ;;
  esac
  if readelf -d "$bin" 2>/dev/null | grep -q NEEDED; then
    ecs_freebsd_c_die "$bin has dynamic NEEDED entries"
  fi
  if strings "$bin" 2>/dev/null | grep -q 'GLIBC_'; then
    ecs_freebsd_c_die "$bin contains glibc symbols"
  fi
}

ecs_freebsd_c_clone_tool() {
  local repository=$1 tag=$2 expected_commit=$3 dest=$4
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/${repository}.git" "$dest" >/dev/null
  local actual
  actual=$(git -C "$dest" rev-parse HEAD)
  [[ "$actual" == "$expected_commit" ]] ||
    ecs_freebsd_c_die "commit mismatch for $repository $tag: expected=$expected_commit actual=$actual"
}

ecs_freebsd_c_write_provenance() {
  local stage=$1 target=$2 triple=$3
  local bin="$stage/bin"
  {
    echo '{'
    echo "  \"target\": \"$target\","
    echo "  \"target_triple\": \"$triple\","
    echo "  \"build_host\": \"ubuntu-24.04-amd64\","
    echo "  \"toolchain_mode\": \"cross\","
    echo "  \"compiler_family\": \"clang\","
    echo "  \"compiler_version\": \"$(clang --version | head -n1 | sed 's/"/\\"/g')\","
    echo "  \"linker\": \"lld\","
    echo "  \"openmp_runtime\": null,"
    echo '  "tools": ['
    local first=1 name
    for name in sysbench zstd openssl fio iperf3; do
      [[ -x "$bin/$name" ]] || continue
      local sha
      sha=$(sha256sum "$bin/$name" | awk '{print $1}')
      if [[ $first -eq 0 ]]; then echo ','; fi
      first=0
      printf '    {"name":"%s","sha256":"%s","compiler_family":"clang"}' "$name" "$sha"
    done
    echo
    echo '  ]'
    echo '}'
  } >"$stage/provenance.json"
}
