#!/usr/bin/env bash
# FreeBSD Clang build of sysbench against target LuaJIT + Concurrency Kit.

ecs_freebsd_c_build_sysbench() {
  local work=$1 stage=$2 deps_prefix=$3 jobs=$4
  local repository tag commit
  repository=$(ecs_lock_tool_field sysbench repository)
  tag=$(ecs_lock_tool_field sysbench tag)
  commit=$(ecs_lock_tool_field sysbench commit)
  local src="$work/src-sysbench"
  ecs_freebsd_c_clone_tool "$repository" "$tag" "$commit" "$src"

  # pkg-config metadata must come from the FreeBSD target prefix, never Ubuntu.
  # FreeBSD .pc files install with prefix=/usr/local; without SYSROOT_DIR,
  # pkg-config would emit host /usr/local paths instead of the extracted prefix.
  export PKG_CONFIG_SYSROOT_DIR="$deps_prefix"
  export PKG_CONFIG_PATH="$deps_prefix/usr/local/libdata/pkgconfig:$deps_prefix/usr/local/lib/pkgconfig"
  export PKG_CONFIG_LIBDIR="$deps_prefix/usr/local/libdata/pkgconfig:$deps_prefix/usr/local/lib/pkgconfig"
  command -v pkg-config >/dev/null 2>&1 || ecs_freebsd_c_die "pkg-config is required"
  local luajit_pc ck_pc
  luajit_pc=$(pkg-config --exists luajit && pkg-config --modversion luajit) ||
    ecs_freebsd_c_die "target LuaJIT pkg-config metadata missing"
  ck_pc=$(pkg-config --exists ck && pkg-config --modversion ck) ||
    ecs_freebsd_c_die "target Concurrency Kit pkg-config metadata missing"
  echo "sysbench deps: luajit=$luajit_pc ck=$ck_pc" >&2

  # Autoconf LDFLAGS must stay compiler/linker flags only. Libtool -all-static
  # is applied only at the final libtool link via --with-extra-ldflags.
  local host_triplet
  host_triplet=$(jq -er --arg t "$ecs_freebsd_target" \
    '.targets[$t].clang_target_triple' "$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json")
  local build_triplet
  build_triplet=$(gcc -dumpmachine)

  (
    cd "$src"
    ./autogen.sh
    ./configure \
      --build="$build_triplet" \
      --host="$host_triplet" \
      --prefix="$work/sysbench-prefix" \
      --with-system-luajit \
      --with-system-ck \
      --with-extra-ldflags='-all-static' \
      --without-mysql \
      --without-pgsql \
      --without-drizzle \
      --without-attachsql \
      --without-oracle
    make -j"$jobs"
  )
  cp "$src/src/sysbench" "$stage/bin/sysbench"
  ecs_freebsd_c_assert_static_freebsd_elf "$stage/bin/sysbench" "$ecs_freebsd_elf_machine"
}
