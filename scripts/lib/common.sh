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
while IFS=$'\t' read -r ecs_target ecs_goos ecs_goarch ecs_package; do
  [[ -n "$ecs_target" && -n "$ecs_package" ]] || continue
  ECS_TARGETS+=("$ecs_target $ecs_goos $ecs_goarch $ecs_package")
done < <(jq -er '.architectures[] | [.target, .goos, .goarch, .package] | @tsv' "$ECS_LOCK_FILE") || return 1

# ECS_ARCHES is intentionally the seven Linux package architecture labels used
# by the existing Linux-only tool builder and release checks.  A package label
# such as amd64 is not a target identity once FreeBSD is present; consumers
# that need to select a complete platform target use ECS_TARGETS or one of the
# platform-specific arrays below.
ECS_LINUX_TARGETS=()
ECS_FREEBSD_TARGETS=()
ECS_LINUX_TARGET_IDS=()
ECS_FREEBSD_TARGET_IDS=()
ECS_LINUX_ARCHES=()
ECS_FREEBSD_ARCHES=()
ECS_TARGET_IDS=()
for ecs_target_record in "${ECS_TARGETS[@]}"; do
  read -r ecs_target ecs_goos ecs_goarch ecs_arch <<<"$ecs_target_record"
  ECS_TARGET_IDS+=("$ecs_target")
  case "$ecs_goos" in
    linux)
      ECS_LINUX_TARGETS+=("$ecs_target_record")
      ECS_LINUX_TARGET_IDS+=("$ecs_target")
      ECS_LINUX_ARCHES+=("$ecs_arch")
      ;;
    freebsd)
      ECS_FREEBSD_TARGETS+=("$ecs_target_record")
      ECS_FREEBSD_TARGET_IDS+=("$ecs_target")
      ECS_FREEBSD_ARCHES+=("$ecs_arch")
      ;;
    *)
      echo "common: unsupported target OS in tools lock: $ecs_goos" >&2
      return 1
      ;;
  esac
done
ECS_ARCHES=("${ECS_LINUX_ARCHES[@]}")

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

ecs_lock_target_field() {
  local target=$1 field=$2
  jq -er --arg target "$target" --arg field "$field" \
    '.architectures[] | select(.target == $target) | .[$field] // empty' "$ECS_LOCK_FILE"
}

ecs_target_tool_names() {
  local target=$1 goos
  goos=$(ecs_lock_target_field "$target" goos) || return 1
  if [[ "$goos" == "freebsd" ]]; then
    jq -er '.tools[].name | select(. != "ping" and . != "nexttrace-tiny")' "$ECS_LOCK_FILE"
  else
    jq -er '.tools[].name' "$ECS_LOCK_FILE"
  fi
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
# verify 的归档校验需要统一解开全部九个平台目标的主程序归档，所以该逻辑
# 作为共享辅助函数保留在这里。
ecs_release_binaries() {
  local dist=$1 out=$2
  local archive name directory
  local -a archives

  mapfile -t archives < <(find "$dist" -maxdepth 1 -type f -name 'ecs_*.tar.gz' -print | sort)
  if [[ "${#archives[@]}" -ne "${#ECS_TARGETS[@]}" ]]; then
    echo "主程序归档 = ${#archives[@]} 个，want ${#ECS_TARGETS[@]}" >&2
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
ecs_devtools_lock_state() {
  local module_dir="$ECS_REPO_ROOT/devtools"
  [[ -f "$module_dir/go.mod" && -f "$module_dir/go.sum" ]] || return 1
  printf '%s\n' 'go.mod:'
  cat "$module_dir/go.mod"
  printf '%s\n' 'go.sum:'
  cat "$module_dir/go.sum"
}

ecs_devtool_cache_valid() {
  local name=$1 bin=$2 lock_file=$3 expected
  [[ -x "$bin" && -s "$lock_file" ]] || return 1
  expected=$(ecs_devtools_lock_state) || return 1
  [[ "$(<"$lock_file")" == "$expected" ]]
}

ecs_devtool() {
  local name=$1 bin package lock_file lock_state
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
    lock_state=$(ecs_devtools_lock_state) || return 1
    printf '%s\n' "$lock_state" >"$lock_file"
  fi
  printf '%s\n' "$bin"
}

# ecs_download_sha256 URL EXPECTED_SHA OUTPUT LABEL
#
# 带重试的下载 + 摘要校验：这是跨越信任边界的那一次校验，所有从互联网取回的
# 第三方源码与二进制都走这里。摘要不符时删除产物重试，三次都失败才失败。
ecs_download_sha256() {
  local url=$1 expected_sha=$2 output=$3 label=$4
  local attempt actual_sha actual_bytes

  for attempt in 1 2 3; do
    if curl -fsSL --connect-timeout 30 --speed-limit 1024 --speed-time 30 \
      --max-time 900 "$url" -o "$output"; then
      actual_sha=$(sha256sum "$output" | awk '{print $1}')
      if [[ "$actual_sha" == "$expected_sha" ]]; then
        return 0
      fi
      actual_bytes=$(stat -c %s "$output")
      echo "ecs: $label SHA-256 mismatch on attempt $attempt/3: expected $expected_sha, got $actual_sha ($actual_bytes bytes)" >&2
    else
      echo "ecs: $label download failed on attempt $attempt/3" >&2
    fi
    rm -f -- "$output"
  done

  echo "ecs: $label did not match its pinned SHA-256 after 3 attempts" >&2
  return 1
}
