#!/usr/bin/env bash
# FreeBSD Clang static build of iperf3.

ecs_freebsd_c_build_iperf3() {
  local work=$1 stage=$2 jobs=$3
  local repository tag commit
  repository=$(ecs_lock_tool_field iperf3 repository)
  tag=$(ecs_lock_tool_field iperf3 tag)
  commit=$(ecs_lock_tool_field iperf3 commit)
  local src="$work/src-iperf3"
  ecs_freebsd_c_clone_tool "$repository" "$tag" "$commit" "$src"

  local host_triplet
  host_triplet=$(jq -er --arg t "$ecs_freebsd_target" \
    '.targets[$t].clang_target_triple' "$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json")
  local build_triplet
  build_triplet=$(gcc -dumpmachine)

  (
    cd "$src"
    ./configure \
      --build="$build_triplet" \
      --host="$host_triplet" \
      --prefix="$work/iperf3-prefix" \
      --enable-static-bin \
      --without-sctp \
      --without-openssl \
      --without-ldconfig
    make -j"$jobs"
  )
  cp "$src/src/iperf3" "$stage/bin/iperf3"
  ecs_freebsd_c_assert_static_freebsd_elf "$stage/bin/iperf3" "$ecs_freebsd_elf_machine"
}
