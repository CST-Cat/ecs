#!/usr/bin/env bash

# 官方 STREAM 的唯一来源与编译合同。
#
# Release 构建和 integration 都使用这里的定义：integration 只下载并编译这一
# 个官方 C 文件，不触发完整十工具构建；两条路径仍然共享同一个 SHA、数组大小
# 和迭代次数，避免测试与发布物悄悄使用不同口径。

ECS_STREAM_URL=$(ecs_lock_stream_field source_url)
ECS_STREAM_SOURCE_SHA256=$(ecs_lock_stream_field source_sha256)
ECS_STREAM_ARRAY_SIZE=$(ecs_lock_stream_field array_size)
ECS_STREAM_NTIMES=$(ecs_lock_stream_field ntimes)
ECS_STREAM_COMPILE_FLAGS=(
  -O3
  -fopenmp
  -static
  -static-libgcc
  "-DSTREAM_ARRAY_SIZE=$ECS_STREAM_ARRAY_SIZE"
  "-DNTIMES=$ECS_STREAM_NTIMES"
)

# ecs_stream_download OUTPUT 下载并校验 19.5 KiB 的官方 stream.c。
#
# URL 主机是 www.cs.virginia.edu（tools/lock.json，与 NASA 无关），源不动；
# 2026-09-13 起在原 3 次尝试上加 2 次短重试并整体限时（同批 NPB 官方渠道
# 500 的教训：长重试烧 CI），19.5 KiB 文件 60s 上限绰绰有余。
ecs_stream_download() {
  local output=$1 attempt actual
  mkdir -p "$(dirname "$output")"

  for attempt in 1 2 3 4 5; do
    if curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 --max-time 60 \
      "$ECS_STREAM_URL" -o "$output"; then
      actual=$(sha256sum "$output" | awk '{print $1}')
      if [[ "$actual" == "$ECS_STREAM_SOURCE_SHA256" ]]; then
        return 0
      fi
      echo "stream: SHA-256 mismatch on attempt $attempt/5: expected $ECS_STREAM_SOURCE_SHA256, got $actual" >&2
    else
      echo "stream: download failed on attempt $attempt/5" >&2
    fi
    rm -f -- "$output"
  done

  return 1
}

# ecs_stream_compile SOURCE OUTPUT [COMPILER] 用发布构建的同一组参数编译官方源码。
ecs_stream_compile() {
  local source=$1 output=$2 compiler=${3:-gcc}
  mkdir -p "$(dirname "$output")"
  "$compiler" "${ECS_STREAM_COMPILE_FLAGS[@]}" "$source" -o "$output"
  [[ -s "$output" && -x "$output" ]]
}
