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

# apt 的网络等待必须有两个边界：Acquire 选项限制每个 HTTP(S) 请求，timeout
# 限制一次完整的 update/install（包括 sudo 和 apt 自己的重试）。默认值让一次
# 操作最多占用约五分钟，不会把 CI job 的三十分钟总超时当成安装器的超时机制。
ECS_APT_HTTP_TIMEOUT_DEFAULT=20
ECS_APT_RETRIES_DEFAULT=2
ECS_APT_OPERATION_TIMEOUT_DEFAULT=5m
ECS_APT_KILL_AFTER_DEFAULT=15s

# 这些命令入口默认指向宿主工具，也可显式指定真实可执行文件路径。
ECS_APT_TIMEOUT_COMMAND_DEFAULT=timeout
ECS_APT_SUDO_COMMAND_DEFAULT=sudo
ECS_APT_GET_COMMAND_DEFAULT=apt-get

# ecs_apt_run OPERATION [APT ARGS...]
ecs_apt_run() {
  local operation=$1
  shift
  local http_timeout="${ECS_APT_HTTP_TIMEOUT:-$ECS_APT_HTTP_TIMEOUT_DEFAULT}"
  local retries="${ECS_APT_RETRIES:-$ECS_APT_RETRIES_DEFAULT}"
  local operation_timeout="${ECS_APT_OPERATION_TIMEOUT:-$ECS_APT_OPERATION_TIMEOUT_DEFAULT}"
  local kill_after="${ECS_APT_KILL_AFTER:-$ECS_APT_KILL_AFTER_DEFAULT}"
  local timeout_command="${ECS_APT_TIMEOUT_COMMAND:-$ECS_APT_TIMEOUT_COMMAND_DEFAULT}"
  local sudo_command="${ECS_APT_SUDO_COMMAND:-$ECS_APT_SUDO_COMMAND_DEFAULT}"
  local apt_get_command="${ECS_APT_GET_COMMAND:-$ECS_APT_GET_COMMAND_DEFAULT}"
  # Make failed index fetches fail the update instead of reusing old lists.
  local -a apt_options=(
    -o "Acquire::http::Timeout=$http_timeout"
    -o "Acquire::https::Timeout=$http_timeout"
    -o "Acquire::Retries=$retries"
    -o APT::Update::Error-Mode=any
  )

  echo "integration: apt-get $operation：HTTP/HTTPS 超时 ${http_timeout}s，重试 ${retries} 次，单次硬超时 ${operation_timeout}（超时后 ${kill_after} 强制终止）" >&2
  "$timeout_command" \
    --signal=TERM \
    --kill-after="$kill_after" \
    "$operation_timeout" \
    "$sudo_command" \
    DEBIAN_FRONTEND=noninteractive \
    "$apt_get_command" \
    "${apt_options[@]}" \
    "$operation" \
    "$@"
}

ecs_install_tools() {
  local update_status install_status
  local -a install_packages=("$@")

  if ((${#install_packages[@]} == 0)); then
    echo "integration: 没有要安装的第三方工具包" >&2
    return 2
  fi

  if ecs_apt_run update; then
    :
  else
    update_status=$?
    echo "integration: apt-get update 失败（退出码 $update_status）" >&2
    return "$update_status"
  fi

  if ecs_apt_run install -y --no-install-recommends "${install_packages[@]}"; then
    return 0
  else
    install_status=$?
    echo "integration: apt-get install 失败（退出码 $install_status），不会把安装失败变成测试跳过" >&2
    return "$install_status"
  fi
}

ecs_install_tools_if_requested() {
  if [[ "${ECS_INSTALL_TOOLS:-${CI:-}}" == "" ]]; then
    ecs_step "跳过安装（设 ECS_INSTALL_TOOLS=1 可让本脚本装齐工具）"
    return 0
  fi

  ecs_step "安装基准工具：$*"
  ecs_install_tools "$@"
}

ecs_integration_main() {
  ecs_install_tools_if_requested "${packages[@]}"

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

# Production probes intentionally do not resolve benchmark tools from PATH.
# The CI-installed real tools are therefore exposed through an explicit,
# private staging directory, just as run.sh does for a user invocation. The
# symlinks preserve capabilities on tools such as ping while keeping the
# lookup contract separate from the host PATH used by integration fixtures.
integration_tool_bin="$stream_work/tool-bin"
mkdir -p "$integration_tool_bin"
for tool in fio sysbench iperf3 ping; do
  tool_path=$(command -v "$tool") || {
    echo "integration: $tool 未安装，无法建立显式工具 staging" >&2
    exit 1
  }
  ln -s "$tool_path" "$integration_tool_bin/$tool"
done
ln -s "$stream_binary" "$integration_tool_bin/stream"
export ECS_TOOL_BIN="$integration_tool_bin"

ecs_step "go test -tags=integration ./... -timeout 30m -count=1"
go test -tags=integration ./... -timeout 30m -count=1

ecs_step "locked NextTrace route/backtrace loopback runtime"
bash scripts/ci/linux_trace_runtime.sh
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  ecs_integration_main "$@"
fi
