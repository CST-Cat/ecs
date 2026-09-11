#!/usr/bin/env bash
# FreeBSD Clang static build of zstd.

ecs_freebsd_c_build_zstd() {
  local work=$1 stage=$2 jobs=$3
  local repository tag commit
  repository=$(ecs_lock_tool_field zstd repository)
  tag=$(ecs_lock_tool_field zstd tag)
  commit=$(ecs_lock_tool_field zstd commit)
  local src="$work/src-zstd"
  ecs_freebsd_c_clone_tool "$repository" "$tag" "$commit" "$src"

  make -C "$src/programs" -j"$jobs" zstd-release \
    CC="$CC" \
    MOREFLAGS="$CFLAGS -static -DZSTD_NODICT -DZSTD_NOTRACE" \
    HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
  cp "$src/programs/zstd" "$stage/bin/zstd"
  ecs_freebsd_c_assert_static_freebsd_elf "$stage/bin/zstd" "$ecs_freebsd_elf_machine"
}
