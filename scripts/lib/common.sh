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

# Resolve caller-provided paths after scripts have changed to ECS_REPO_ROOT.
# A path which is passed to a compiler wrapper may later be used from an
# upstream source directory, so keeping it relative would make the wrapper
# resolve a different sysroot or dependency prefix there.
ecs_absolute_path() {
  local path=$1
  case "$path" in
    /*) printf '%s\n' "$path" ;;
    *) printf '%s/%s\n' "$ECS_REPO_ROOT" "$path" ;;
  esac
}

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
ECS_WINDOWS_TARGETS=()
while IFS=$'\t' read -r ecs_target ecs_goos ecs_goarch ecs_package; do
  [[ -n "$ecs_target" && -n "$ecs_package" ]] || continue
  case "$ecs_goos" in
    linux | freebsd)
      ECS_TARGETS+=("$ecs_target $ecs_goos $ecs_goarch $ecs_package")
      ;;
    windows)
      # Keep the Phase 6 builder target separate from the existing
      # Linux/FreeBSD package and release target arrays.
      ECS_WINDOWS_TARGETS+=("$ecs_target $ecs_goos $ecs_goarch $ecs_package")
      ;;
    *)
      echo "common: unsupported target OS in tools lock: $ecs_goos" >&2
      return 1
      ;;
  esac
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
ECS_WINDOWS_TARGET_IDS=()
ECS_LINUX_ARCHES=()
ECS_FREEBSD_ARCHES=()
ECS_WINDOWS_ARCHES=()
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
for ecs_target_record in "${ECS_WINDOWS_TARGETS[@]}"; do
  read -r ecs_target _ecs_goos _ecs_goarch ecs_arch <<<"$ecs_target_record"
  ECS_WINDOWS_TARGET_IDS+=("$ecs_target")
  ECS_WINDOWS_ARCHES+=("$ecs_arch")
done
ECS_ARCHES=("${ECS_LINUX_ARCHES[@]}")

# ECS_TARGETS intentionally remains the Unix-only set used by the existing
# Linux/FreeBSD builders and lock checks. Release packaging has one additional
# native target, so keep that contract explicit rather than silently changing
# every existing consumer of ECS_TARGETS.
ECS_RELEASE_TARGETS=("${ECS_TARGETS[@]}" "${ECS_WINDOWS_TARGETS[@]}")
ECS_RELEASE_TARGET_IDS=("${ECS_TARGET_IDS[@]}" "${ECS_WINDOWS_TARGET_IDS[@]}")

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

ecs_target_os() {
  local target=${1:-} goos
  [[ -n "$target" ]] || {
    echo "common: target is required" >&2
    return 1
  }
  if ! goos=$(ecs_lock_target_field "$target" goos); then
    echo "common: unsupported target: $target" >&2
    return 1
  fi
  printf '%s\n' "$goos"
}

# ecs_target_asset_name TARGET KIND [TOOL]
#
# This is the single naming contract for target-specific binaries and release
# assets. KIND is one of:
#
#   binary       cross.sh output (ecs_<target>[.exe])
#   main         ECS release archive (tar.gz on Unix, zip on Windows)
#   tools        tools bundle archive (tar.gz on Unix, zip on Windows)
#   main-member  executable name inside the ECS release archive
#   tool         executable name for a logical tool inside a tools bundle
#
# Keeping the platform suffix and archive format here prevents a second,
# subtly different "Windows branch" from appearing in each caller.
ecs_target_asset_name() {
  local target=${1:-} kind=${2:-} tool=${3:-} goos
  [[ -n "$target" && -n "$kind" ]] || {
    echo "common: target and asset kind are required" >&2
    return 1
  }
  goos=$(ecs_target_os "$target") || return 1

  case "$kind:$goos" in
    binary:linux | binary:freebsd)
      printf 'ecs_%s\n' "$target"
      ;;
    binary:windows)
      printf 'ecs_%s.exe\n' "$target"
      ;;
    main:linux | main:freebsd)
      printf 'ecs_%s.tar.gz\n' "$target"
      ;;
    main:windows)
      printf 'ecs_%s.zip\n' "$target"
      ;;
    tools:linux | tools:freebsd)
      printf 'ecs-tools_%s.tar.gz\n' "$target"
      ;;
    tools:windows)
      printf 'ecs-tools_%s.zip\n' "$target"
      ;;
    main-member:linux | main-member:freebsd)
      printf 'ecs\n'
      ;;
    main-member:windows)
      printf 'ecs.exe\n'
      ;;
    tool:linux | tool:freebsd)
      [[ -n "$tool" ]] || {
        echo "common: logical tool name is required" >&2
        return 1
      }
      printf '%s\n' "$tool"
      ;;
    tool:windows)
      [[ -n "$tool" ]] || {
        echo "common: logical tool name is required" >&2
        return 1
      }
      printf '%s.exe\n' "$tool"
      ;;
    *)
      echo "common: unsupported asset kind/target OS: $kind/$goos" >&2
      return 1
      ;;
  esac
}

ecs_target_tool_names() {
  local target=${1:-} goos
  goos=$(ecs_target_os "$target") || return 1
  case "$goos" in
    linux)
      jq -er '.tools[].name' "$ECS_LOCK_FILE"
      ;;
    freebsd)
      jq -er '.tools[].name | select(. != "ping" and . != "nexttrace-tiny")' "$ECS_LOCK_FILE"
      ;;
    windows)
      jq -er '.windows_tools[]' "$ECS_LOCK_FILE"
      ;;
    *)
      echo "common: unsupported target OS in tool set helper: $goos" >&2
      return 1
      ;;
  esac
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
# 从 dist 目录里解出全部主程序二进制，每行输出 "target<TAB>二进制路径"。
# 归档数不等于发布架构数时失败——少一个架构或多一个未知目标都必须挡住。
# 解包前还严格检查主程序归档的成员集合与路径，避免把目录穿越交给 tar/unzip。
ecs_release_binaries() {
  local dist=$1 out=$2
  local archive target target_id name directory member listing expected_listing actual_listing
  local -a archives expected_members members

  [[ -d "$dist" && -d "$out" ]] || {
    echo "release binaries: dist and output directories are required" >&2
    return 1
  }

  mapfile -t archives < <(find "$dist" -maxdepth 1 \( -type f -o -type l \) \
    \( -name 'ecs_*.tar.gz' -o -name 'ecs_*.zip' \) -print | sort)
  if [[ "${#archives[@]}" -ne "${#ECS_RELEASE_TARGET_IDS[@]}" ]]; then
    echo "主程序归档 = ${#archives[@]} 个，want ${#ECS_RELEASE_TARGET_IDS[@]}" >&2
    return 1
  fi

  for target in "${ECS_RELEASE_TARGET_IDS[@]}"; do
    archive="$dist/$(ecs_target_asset_name "$target" main)" || return 1
    [[ -s "$archive" ]] || {
      echo "release binaries: missing archive for target $target: $archive" >&2
      return 1
    }
  done
  for archive in "${archives[@]}"; do
    [[ -f "$archive" && ! -L "$archive" ]] || {
      echo "release binaries: archive is not a regular file: $archive" >&2
      return 1
    }
    name=$(basename "$archive")
    target=""
    for target_id in "${ECS_RELEASE_TARGET_IDS[@]}"; do
      [[ "$name" == "$(ecs_target_asset_name "$target_id" main)" ]] || continue
      target=$target_id
      break
    done
    [[ -n "$target" ]] || {
      echo "release binaries: unexpected main-program archive: $name" >&2
      return 1
    }
  done

  for target in "${ECS_RELEASE_TARGET_IDS[@]}"; do
    archive="$dist/$(ecs_target_asset_name "$target" main)"
    directory="$out/$target"
    member=$(ecs_target_asset_name "$target" main-member) || return 1
    mkdir -p "$directory"
    expected_members=("$member" LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md)

    case "$archive" in
      *.tar.gz)
        if ! listing=$(tar -tzf "$archive"); then
          echo "release binaries: cannot list $archive" >&2
          return 1
        fi
        ;;
      *.zip)
        if ! listing=$(unzip -Z1 "$archive"); then
          echo "release binaries: cannot list $archive" >&2
          return 1
        fi
        ;;
      *)
        echo "release binaries: unsupported archive suffix: $archive" >&2
        return 1
        ;;
    esac
    mapfile -t members <<<"$listing"
    for name in "${members[@]}"; do
      [[ "$name" != /* && "$name" != *'\\'* && "/$name/" != */../* ]] || {
        echo "release binaries: unsafe archive member in $archive: $name" >&2
        return 1
      }
    done
    expected_listing=$(printf '%s\n' "${expected_members[@]}" | LC_ALL=C sort)
    actual_listing=$(printf '%s\n' "${members[@]}" | LC_ALL=C sort)
    [[ "$actual_listing" == "$expected_listing" ]] || {
      echo "release binaries: unexpected member set in $archive" >&2
      echo "want:" >&2
      printf '%s\n' "$expected_listing" >&2
      echo "got:" >&2
      printf '%s\n' "$actual_listing" >&2
      return 1
    }

    case "$archive" in
      *.tar.gz) tar -xzf "$archive" -C "$directory" || return 1 ;;
      *.zip) unzip -qq "$archive" -d "$directory" || return 1 ;;
    esac
    [[ -f "$directory/$member" && ! -L "$directory/$member" ]] || {
      echo "release binaries: archive did not produce a regular $member for $target" >&2
      return 1
    }
    printf '%s\t%s\n' "$target" "$directory/$member"
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
