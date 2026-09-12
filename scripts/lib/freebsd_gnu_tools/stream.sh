#!/usr/bin/env bash
# FreeBSD GNU/OpenMP build of the official STREAM benchmark (Stage 5).
#
# 源码 URL/SHA256、数组大小与迭代次数完全沿用 scripts/lib/stream.sh +
# tools/lock.json 的统一合同（与 Linux 发布构建、integration smoke 同一口径，
# 禁止第二套参数）；本文件只把编译器换成 Stage 4 GNU SDK 的 FreeBSD target
# gcc wrapper（-fopenmp 落在 libgomp 上，禁 libomp）。

# ecs_freebsd_gnu_build_stream WORK STAGE WRAPBIN FILE_MACHINE
ecs_freebsd_gnu_build_stream() {
  local work=$1 stage=$2 wrap_bin=$3 file_machine=$4
  local src="$work/src-stream.c"

  echo "freebsd-gnu-tools: downloading official STREAM source" >&2
  ecs_stream_download "$src" ||
    ecs_freebsd_gnu_die "could not download official STREAM source"

  # ECS_STREAM_COMPILE_FLAGS（scripts/lib/stream.sh）：-O3 -fopenmp -static
  # -static-libgcc -DSTREAM_ARRAY_SIZE=<locked> -DNTIMES=<locked>。
  # wrapper 负责 --sysroot，参数与 Linux 发布构建逐字相同。
  ecs_stream_compile "$src" "$stage/bin/stream" "$wrap_bin/gcc" ||
    ecs_freebsd_gnu_die "STREAM compile failed"

  ecs_freebsd_gnu_assert_static_freebsd_elf "$stage/bin/stream" "$file_machine"
  ecs_freebsd_gnu_assert_libgomp "$stage/bin/stream"

  {
    echo "STREAM 5.10, John D. McCalpin."
    echo "License terms are embedded in the stream.c source header"
    echo "($ECS_STREAM_URL)."
  } >"$stage/LICENSES/stream.LICENSE"
}
