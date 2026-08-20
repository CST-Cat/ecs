#!/usr/bin/env bash
# 全部构建、检查与发布脚本的共享定义。
#
# 所有脚本共用这一个 lib，避免发布目标、供应链校验与通用辅助逻辑
# 在多份库之间漂移。
#
# 每个脚本用一行把它引进来：
#
#   source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
#
# 引进来之后 $ECS_REPO_ROOT 可用，仓库根只在这里算一次。

# shellcheck shell=bash

# BASH_SOURCE 在 bash 之外（例如从交互式 zsh 里 source 本文件）是空的，
# 那时 dirname "" 会得到 "."，再往上两级就跑出仓库了。先问 git。
if [[ -n "${BASH_SOURCE[0]:-}" ]]; then
  ECS_REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
else
  ECS_REPO_ROOT=$(git rev-parse --show-toplevel)
fi

# ---- 发布目标 ----
#
# ecs 只面向 Linux VPS，发布目标随之收敛到 Linux 架构。任何需要遍历七架构的
# 脚本都用下面这几个数组，不要再抄一份列表——抄写过的列表迟早会漏掉一个架构，
# 而那种缺失只会在发布当天才暴露。
#
# ECS_TARGETS 的字段是 "GOOS GOARCH 包架构名"。包架构名与 GOARCH 不总是相同：
# GOARM=7 在发布物里表示为 armv7 而不是第二个 v7 后缀。
ECS_TARGETS=(
  "linux amd64 amd64"
  "linux arm64 arm64"
  "linux arm armv7"
  "linux 386 386"
  "linux s390x s390x"
  "linux riscv64 riscv64"
  "linux ppc64le ppc64le"
)

# Package architecture names are derived from the target table above; keeping
# only one literal architecture list prevents release/package loops drifting.
ECS_ARCHES=()
for ecs_target in "${ECS_TARGETS[@]}"; do
  read -r _ _ ecs_arch <<<"$ecs_target"
  ECS_ARCHES+=("$ecs_arch")
done

# 每个架构的工具包必须包含的十个二进制。
ECS_TOOL_NAMES=(sysbench zstd npb-ep npb-ft openssl stream fio iperf3 nexttrace-tiny ping)

# Silesia 语料的固定尺寸与摘要。语料是独立发布物，不进工具包。
ECS_CORPUS_BYTES=211938580
ECS_CORPUS_SHA256=8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0

ecs_step() {
  printf '\n==> %s\n' "$*" >&2
}

# ecs_retry COMMAND... 对网络操作重试三次，间隔递增。
#
# 每日任务里一次 GitHub 5xx 就让 workflow 变红是有害的：红灯常态化之后没人
# 再看它，而那正是这套流程想避免的。重试只用于下载这类可安全重复的操作，
# 不要用来包装有副作用的命令。
ecs_retry() {
  local attempt
  for attempt in 1 2 3; do
    if "$@"; then
      return 0
    fi
    if [[ "$attempt" -lt 3 ]]; then
      echo "retry: 第 $attempt 次失败，${attempt}0 秒后重试：$*" >&2
      sleep "${attempt}0"
    fi
  done
  echo "retry: 三次均失败：$*" >&2
  return 1
}

# ---- 发布制品 ----

# ecs_release_binaries DIST_DIR OUT_DIR
#
# 从 dist 目录里解出全部主程序二进制，每行输出 "归档名<TAB>二进制路径"。
# 归档数不等于发布架构数时失败——少一个架构就发布是这套流程最该挡住的事。
#
# verify 的归档校验需要统一解开全部七个主程序归档，所以该逻辑
# 作为共享辅助函数保留在这里。
ecs_release_binaries() {
  local dist=$1 out=$2
  local archive name directory
  local -a archives

  mapfile -t archives < <(find "$dist" -maxdepth 1 -type f -name 'ecs_linux_*.tar.gz' -print | sort)
  if [[ "${#archives[@]}" -ne "${#ECS_ARCHES[@]}" ]]; then
    echo "主程序归档 = ${#archives[@]} 个，want ${#ECS_ARCHES[@]}" >&2
    return 1
  fi

  for archive in "${archives[@]}"; do
    name=$(basename "$archive" .tar.gz)
    directory="$out/$name"
    mkdir -p "$directory"
    tar -xzf "$archive" -C "$directory" ecs || return 1
    printf '%s\t%s\n' "$name" "$directory/ecs"
  done
}

# ---- 分析工具 ----
#
# staticcheck 的版本由 devtools/go.mod + go.sum 固定，不在 workflow YAML
# 里再写一份。主模块 go.mod 保持零依赖：从源码构建 ecs 不需要
# 下载任何模块，那是发布物的一项属性，不该为了跑分析工具而放弃。

# ecs_devtool NAME 构建（若尚未构建）并回显分析工具的路径。
#
# 先构建再从仓库根运行，而不是 `go tool ...`：go tool 的工作目录与包模式
# `./...` 的解析基准会随调用位置漂移，构建出独立二进制则没有这种歧义。
ecs_devtool() {
  local name=$1 bin package
  bin="$ECS_REPO_ROOT/.devtools-bin/$name"

  case "$name" in
    staticcheck) package=honnef.co/go/tools/cmd/staticcheck ;;
    *)
      echo "ecs_devtool: unknown tool: $name" >&2
      return 1
      ;;
  esac

  if [[ ! -x "$bin" ]]; then
    mkdir -p "$ECS_REPO_ROOT/.devtools-bin"
    echo "devtools: building $name from devtools/go.mod" >&2
    (cd "$ECS_REPO_ROOT/devtools" && go build -o "$bin" "$package") || return 1
  fi
  printf '%s\n' "$bin"
}
