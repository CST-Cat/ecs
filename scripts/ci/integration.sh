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
# update 最多占用约五分钟；首次失败后最多再进行一次同样有界的备用 update，
# 不会把 CI job 的三十分钟总超时当成安装器的超时机制。
ECS_APT_HTTP_TIMEOUT_DEFAULT=20
ECS_APT_RETRIES_DEFAULT=2
ECS_APT_OPERATION_TIMEOUT_DEFAULT=5m
ECS_APT_KILL_AFTER_DEFAULT=15s

# 这些命令入口默认指向宿主工具；回归测试注入 fake 命令，不触碰宿主 apt。
# 变量只接受一个可执行文件路径，避免把额外 shell 片段拼进命令行。
ECS_APT_TIMEOUT_COMMAND_DEFAULT=timeout
ECS_APT_SUDO_COMMAND_DEFAULT=sudo
ECS_APT_GET_COMMAND_DEFAULT=apt-get

ecs_apt_source_files() {
  local source_root source_list source_parts path
  source_root="${ECS_APT_SOURCE_ROOT:-/etc/apt}"
  source_list="${ECS_APT_SOURCE_LIST:-$source_root/sources.list}"
  source_parts="${ECS_APT_SOURCE_PARTS:-$source_root/sources.list.d}"

  if [[ -f "$source_list" ]]; then
    printf '%s\0' "$source_list"
  fi

  if [[ -d "$source_parts" ]]; then
    while IFS= read -r -d '' path; do
      printf '%s\0' "$path"
    done < <(
      find "$source_parts" -maxdepth 1 -type f \
        \( -name '*.list' -o -name '*.sources' \) -print0 | sort -z
    )
  fi
}

ecs_apt_has_azure_source() {
  local source_file
  while IFS= read -r -d '' source_file; do
    if grep -Eq -- '^[[:space:]]*(deb(-src)?|URIs:).*https?://azure\.archive\.ubuntu\.com([/:[:space:]]|$)' "$source_file"; then
      return 0
    fi
  done < <(ecs_apt_source_files)
  return 1
}

# 在临时目录里复制 apt 的主 sources.list 与 sources.list.d。只替换精确的
# Azure Ubuntu archive 主机名；第三方源的内容和文件仍然保留，宿主源文件不会
# 被 sed -i 改写。调用者在下一次 apt 操作后负责清理该临时目录。
ecs_apt_prepare_fallback_sources() {
  local source_root source_list source_parts fallback_dir source_file target

  source_root="${ECS_APT_SOURCE_ROOT:-/etc/apt}"
  source_list="${ECS_APT_SOURCE_LIST:-$source_root/sources.list}"
  source_parts="${ECS_APT_SOURCE_PARTS:-$source_root/sources.list.d}"
  fallback_dir=$(mktemp -d "${TMPDIR:-/tmp}/ecs-apt-sources.XXXXXX") || {
    echo "integration: 无法创建 apt 备用源临时目录" >&2
    return 1
  }
  mkdir -p "$fallback_dir/sources.list.d" || {
    echo "integration: 无法创建 apt 备用源临时目录结构" >&2
    rm -rf -- "$fallback_dir"
    return 1
  }

  while IFS= read -r -d '' source_file; do
    if [[ "$source_file" == "$source_list" ]]; then
      target="$fallback_dir/sources.list"
    else
      target="$fallback_dir/sources.list.d/$(basename "$source_file")"
    fi
    sed -E 's#(https?://)azure\.archive\.ubuntu\.com([/:]|$)#\1archive.ubuntu.com\2#g' \
      "$source_file" >"$target" || {
      echo "integration: 无法复制 apt 源文件 $source_file" >&2
      rm -rf -- "$fallback_dir"
      return 1
    }
  done < <(ecs_apt_source_files)

  # 某些 Ubuntu 镜像只使用 sources.list.d/*.sources；给 apt 一个明确的空主
  # 列表，避免它把默认 /etc/apt/sources.list 又混进备用配置。
  if [[ ! -e "$fallback_dir/sources.list" ]]; then
    : >"$fallback_dir/sources.list" || {
      echo "integration: 无法创建 apt 备用主源列表" >&2
      rm -rf -- "$fallback_dir"
      return 1
    }
  fi

  ECS_APT_FALLBACK_SOURCE_DIR=$fallback_dir
  ECS_APT_FALLBACK_SOURCE_LIST="$fallback_dir/sources.list"
  ECS_APT_FALLBACK_SOURCE_PARTS="$fallback_dir/sources.list.d"
}

ecs_apt_cleanup_fallback_sources() {
  if [[ -n "${ECS_APT_FALLBACK_SOURCE_DIR:-}" && -d "$ECS_APT_FALLBACK_SOURCE_DIR" ]]; then
    rm -rf -- "$ECS_APT_FALLBACK_SOURCE_DIR"
  fi
  ECS_APT_FALLBACK_SOURCE_DIR=
  ECS_APT_FALLBACK_SOURCE_LIST=
  ECS_APT_FALLBACK_SOURCE_PARTS=
}

# ecs_apt_run OPERATION SOURCE_MODE [APT ARGS...]
ecs_apt_run() {
  local operation=$1 source_mode=$2
  shift 2
  local http_timeout="${ECS_APT_HTTP_TIMEOUT:-$ECS_APT_HTTP_TIMEOUT_DEFAULT}"
  local retries="${ECS_APT_RETRIES:-$ECS_APT_RETRIES_DEFAULT}"
  local operation_timeout="${ECS_APT_OPERATION_TIMEOUT:-$ECS_APT_OPERATION_TIMEOUT_DEFAULT}"
  local kill_after="${ECS_APT_KILL_AFTER:-$ECS_APT_KILL_AFTER_DEFAULT}"
  local timeout_command="${ECS_APT_TIMEOUT_COMMAND:-$ECS_APT_TIMEOUT_COMMAND_DEFAULT}"
  local sudo_command="${ECS_APT_SUDO_COMMAND:-$ECS_APT_SUDO_COMMAND_DEFAULT}"
  local apt_get_command="${ECS_APT_GET_COMMAND:-$ECS_APT_GET_COMMAND_DEFAULT}"
  local source_description='默认 apt 源'
  local -a apt_options=(
    -o "Acquire::http::Timeout=$http_timeout"
    -o "Acquire::https::Timeout=$http_timeout"
    -o "Acquire::Retries=$retries"
  )

  if [[ "$source_mode" == fallback ]]; then
    [[ -n "${ECS_APT_FALLBACK_SOURCE_LIST:-}" && -n "${ECS_APT_FALLBACK_SOURCE_PARTS:-}" ]] || {
      echo "integration: apt 备用源配置未准备好" >&2
      return 1
    }
    apt_options+=(
      -o "Dir::Etc::sourcelist=$ECS_APT_FALLBACK_SOURCE_LIST"
      -o "Dir::Etc::sourceparts=$ECS_APT_FALLBACK_SOURCE_PARTS"
    )
    source_description='临时备用 apt 源（azure.archive.ubuntu.com → archive.ubuntu.com）'
  elif [[ "$source_mode" != default ]]; then
    echo "integration: 未知 apt 源模式：$source_mode" >&2
    return 1
  fi

  echo "integration: apt-get $operation：HTTP/HTTPS 超时 ${http_timeout}s，重试 ${retries} 次，单次硬超时 ${operation_timeout}（超时后 ${kill_after} 强制终止），源：$source_description" >&2
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
  local original_update_status fallback_update_status install_status
  local -a install_packages=("$@")

  if ((${#install_packages[@]} == 0)); then
    echo "integration: 没有要安装的基准工具包" >&2
    return 2
  fi

  ECS_APT_FALLBACK_SOURCE_DIR=
  ECS_APT_FALLBACK_SOURCE_LIST=
  ECS_APT_FALLBACK_SOURCE_PARTS=

  if ecs_apt_run update default; then
    if ecs_apt_run install default -y --no-install-recommends "${install_packages[@]}"; then
      return 0
    else
      install_status=$?
      echo "integration: apt-get install 失败（退出码 $install_status），不会把安装失败变成测试跳过" >&2
      return "$install_status"
    fi
  else
    original_update_status=$?
    echo "integration: 首次 apt-get update 失败（退出码 $original_update_status）" >&2
  fi

  if ! ecs_apt_has_azure_source; then
    echo "integration: 未发现 azure.archive.ubuntu.com，未切换其他源；apt 安装中止" >&2
    return "$original_update_status"
  fi

  echo "integration: 仅为 Debian/Ubuntu 源准备备用镜像；第三方 apt 源保持不变" >&2
  if ! ecs_apt_prepare_fallback_sources; then
    echo "integration: 备用 apt 源准备失败；原始 apt-get update 错误已保留在上方输出" >&2
    return 1
  fi

  if ecs_apt_run update fallback; then
    :
  else
    fallback_update_status=$?
    ecs_apt_cleanup_fallback_sources
    echo "integration: 备用 apt-get update 仍失败（退出码 $fallback_update_status；原始 update 退出码 $original_update_status）；原始与备用错误均已保留在上方输出" >&2
    return "$fallback_update_status"
  fi

  if ecs_apt_run install fallback -y --no-install-recommends "${install_packages[@]}"; then
    :
  else
    install_status=$?
    ecs_apt_cleanup_fallback_sources
    echo "integration: 备用 apt-get install 仍失败（退出码 $install_status；原始 update 退出码 $original_update_status）；原始与备用错误均已保留在上方输出" >&2
    return "$install_status"
  fi

  ecs_apt_cleanup_fallback_sources
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

ecs_step "go test -tags=integration ./... -timeout 30m -count=1"
go test -tags=integration ./... -timeout 30m -count=1
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  ecs_integration_main "$@"
fi
