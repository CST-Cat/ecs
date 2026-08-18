#!/usr/bin/env bash
set -euo pipefail

# 集成测试：以宿主上真实安装的基准工具运行 //go:build integration 测试。
#
# 用真实工具而不是脚本替身，是因为替身只能证明解析器认得它自己造出来的输出。
# sysbench 与 iperf3 的输出格式在版本间都变过，只有真实工具能证明解析器跟得上。
#
# 装包这一步只在 CI 或显式要求时做：开发机上的包管理器状态归开发者自己管，
# 一个测试脚本不该擅自 apt-get install。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
source "$(dirname "${BASH_SOURCE[0]}")/../lib/stream.sh"
cd "$ECS_REPO_ROOT"

packages=(fio sysbench iperf3 iputils-ping)

if [[ "${ECS_INSTALL_TOOLS:-${CI:-}}" == "" ]]; then
  ecs_step "跳过安装（设 ECS_INSTALL_TOOLS=1 可让本脚本装齐工具）"
else
  ecs_step "安装基准工具：${packages[*]}"
  sudo apt-get update
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${packages[@]}"
fi

# integration 只需要一个真实的官方 STREAM 二进制，不应为了这个测试触发完整
# 十工具构建。源码只有 19.5 KiB；下载后按 release 的固定 SHA 和编译参数构建，
# 放进本次 job 的临时 PATH，测试结束随 runner 临时目录一起回收。
stream_work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ecs-stream.XXXXXX")
trap 'rm -rf -- "$stream_work"' EXIT
stream_source="$stream_work/stream.c"
stream_binary="$stream_work/bin/stream"
ecs_step "准备官方 STREAM（${ECS_STREAM_ARRAY_SIZE} elements × ${ECS_STREAM_NTIMES} iterations）"
ecs_stream_download "$stream_source" || {
  echo "integration: 官方 STREAM 下载或 SHA-256 校验失败" >&2
  exit 1
}
ecs_stream_compile "$stream_source" "$stream_binary" || {
  echo "integration: 官方 STREAM 编译失败" >&2
  exit 1
}
export PATH="$(dirname "$stream_binary"):$PATH"

ecs_step "已安装的工具"
for tool in fio sysbench iperf3 ping stream; do
  printf '%-10s %s\n' "$tool" "$(command -v "$tool" || echo '未安装')"
done

ecs_step "go test -tags=integration"
go test -tags=integration ./... -timeout 30m
