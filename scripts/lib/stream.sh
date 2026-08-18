#!/usr/bin/env bash

# 官方 STREAM 的唯一来源与编译合同。
#
# Release 构建和 integration 都使用这里的定义：integration 只下载并编译这一
# 个官方 C 文件，不触发完整十工具构建；两条路径仍然共享同一个 SHA、数组大小
# 和迭代次数，避免测试与发布物悄悄使用不同口径。

ECS_STREAM_URL='https://www.cs.virginia.edu/stream/FTP/Code/stream.c'
ECS_STREAM_SOURCE_SHA256='a52bae5e175bea3f7832112af9c085adab47117f7d2ce219165379849231692b'
ECS_STREAM_ARRAY_SIZE=10000000
ECS_STREAM_NTIMES=10
ECS_STREAM_COMPILE_FLAGS=(
  -O3
  -fopenmp
  -static
  -static-libgcc
  "-DSTREAM_ARRAY_SIZE=$ECS_STREAM_ARRAY_SIZE"
  "-DNTIMES=$ECS_STREAM_NTIMES"
)

# ecs_stream_download OUTPUT 下载并校验 19.5 KiB 的官方 stream.c。
ecs_stream_download() {
  local output=$1 attempt actual
  mkdir -p "$(dirname "$output")"

  for attempt in 1 2 3; do
    if curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 \
      "$ECS_STREAM_URL" -o "$output"; then
      actual=$(sha256sum "$output" | awk '{print $1}')
      if [[ "$actual" == "$ECS_STREAM_SOURCE_SHA256" ]]; then
        return 0
      fi
      echo "stream: SHA-256 mismatch on attempt $attempt/3: expected $ECS_STREAM_SOURCE_SHA256, got $actual" >&2
    else
      echo "stream: download failed on attempt $attempt/3" >&2
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
