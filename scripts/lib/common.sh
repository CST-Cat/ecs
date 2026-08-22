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

# ---- locked build facts ----
#
# Architectures, tool identities, upstream pins and corpus facts are kept in
# one reviewed JSON lock. These scripts read the repository copy; the public
# bootstrap wrappers never download or depend on this file.
ECS_LOCK_FILE="$ECS_REPO_ROOT/tools/lock.json"

if [[ ! -s "$ECS_LOCK_FILE" ]]; then
  echo "common: missing tools lock: $ECS_LOCK_FILE" >&2
  return 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "common: jq is required to read $ECS_LOCK_FILE" >&2
  return 1
fi

ECS_LOCK_SCHEMA_VERSION=$(jq -er '.schema_version' "$ECS_LOCK_FILE") || return 1
[[ "$ECS_LOCK_SCHEMA_VERSION" == "ecs.tools.lock/v1" ]] || {
  echo "common: unsupported tools lock schema: $ECS_LOCK_SCHEMA_VERSION" >&2
  return 1
}

ECS_TARGETS=()
while IFS=$'\t' read -r ecs_goos ecs_goarch ecs_package; do
  [[ -n "$ecs_package" ]] || continue
  ECS_TARGETS+=("$ecs_goos $ecs_goarch $ecs_package")
done < <(jq -er '.architectures[] | [.goos, .goarch, .package] | @tsv' "$ECS_LOCK_FILE") || return 1

ECS_ARCHES=()
for ecs_target in "${ECS_TARGETS[@]}"; do
  read -r _ _ ecs_arch <<<"$ecs_target"
  ECS_ARCHES+=("$ecs_arch")
done

ECS_TOOL_NAMES=()
while IFS= read -r ecs_tool_name; do
  [[ -n "$ecs_tool_name" ]] || continue
  ECS_TOOL_NAMES+=("$ecs_tool_name")
done < <(jq -er '.tools[].name' "$ECS_LOCK_FILE") || return 1

ECS_CORPUS_BYTES=$(jq -er '.corpus.bytes' "$ECS_LOCK_FILE") || return 1
ECS_CORPUS_SHA256=$(jq -er '.corpus.sha256' "$ECS_LOCK_FILE") || return 1
ECS_CORPUS_NAME=$(jq -er '.corpus.name' "$ECS_LOCK_FILE") || return 1
ECS_CORPUS_ARCHIVE=$(jq -er '.corpus.archive' "$ECS_LOCK_FILE") || return 1

ecs_lock_tool_field() {
  local tool=$1 field=$2
  jq -er --arg tool "$tool" --arg field "$field" \
    '.tools[] | select(.name == $tool) | .[$field] // empty' "$ECS_LOCK_FILE"
}

ecs_lock_architecture_field() {
  local architecture=$1 field=$2
  jq -er --arg architecture "$architecture" --arg field "$field" \
    '.architectures[] | select(.package == $architecture) | .[$field] // empty' "$ECS_LOCK_FILE"
}

ecs_lock_corpus_field() {
  local field=$1
  jq -er --arg field "$field" '.corpus[$field] // empty' "$ECS_LOCK_FILE"
}

ecs_lock_corpus_order() {
  jq -er '.corpus.order[]' "$ECS_LOCK_FILE"
}

ecs_lock_stream_field() {
  local field=$1
  ecs_lock_tool_field stream "$field"
}

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
ecs_devtools_lock_hash() {
  local module_dir="$ECS_REPO_ROOT/devtools"
  [[ -f "$module_dir/go.mod" && -f "$module_dir/go.sum" ]] || return 1
  (cd "$module_dir" && sha256sum go.mod go.sum) | sha256sum | awk '{print $1}'
}

ecs_devtool_cache_valid() {
  local name=$1 bin=$2 lock_file=$3 expected
  [[ -x "$bin" && -s "$lock_file" ]] || return 1
  expected=$(ecs_devtools_lock_hash) || return 1
  [[ "$(<"$lock_file")" == "$expected" ]]
}

ecs_devtool() {
  local name=$1 bin package lock_file lock_hash
  bin="$ECS_REPO_ROOT/.devtools-bin/$name"
  lock_file="$ECS_REPO_ROOT/.devtools-bin/$name.lock"

  case "$name" in
    staticcheck) package=honnef.co/go/tools/cmd/staticcheck ;;
    govulncheck) package=golang.org/x/vuln/cmd/govulncheck ;;
    *)
      echo "ecs_devtool: unknown tool: $name" >&2
      return 1
      ;;
  esac

  if ! ecs_devtool_cache_valid "$name" "$bin" "$lock_file"; then
    mkdir -p "$ECS_REPO_ROOT/.devtools-bin"
    echo "devtools: building $name from devtools/go.mod + go.sum" >&2
    (cd "$ECS_REPO_ROOT/devtools" && go build -o "$bin" "$package") || return 1
    lock_hash=$(ecs_devtools_lock_hash) || return 1
    printf '%s\n' "$lock_hash" >"$lock_file"
  fi
  printf '%s\n' "$bin"
}
