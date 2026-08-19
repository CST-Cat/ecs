#!/usr/bin/env bash
set -euo pipefail

# 重新审查**已经发布的**二进制。
#
# 发布完成不代表安全生命周期结束：漏洞库每天都在更新，昨天干净的二进制今天
# 可能已经不干净了。用户手上跑的正是这些文件，所以复审的对象必须是 Release
# 上的实际归档，而不是今天的源码——今天的源码里可能已经修好了，但那份修复
# 还没有变成用户能下载到的东西。
#
# 扫描复用 scripts/release/security.sh：发布前门禁与发布后监控用同一份实现，
# 差别只在扫描对象是候选制品还是已发布制品。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"

usage() {
  echo "usage: scripts/security/scan_released.sh --work-dir DIR [--tag TAG]" >&2
}

die() {
  echo "scan-released: $*" >&2
  exit 1
}

# read_release_build_go BINARY
#
# `go version -m` prints the toolchain on its first line for current Go releases:
#
#   /path/to/ecs: go1.26.5
#
# Newer releases also expose the same value as a `build GoVersion=...` setting.
# Accept either representation, but require exactly one distinct, parseable
# version.  In particular, `go version -m` exits zero for a non-Go executable
# after printing "could not read Go build info"; checking the parsed value is
# therefore essential and cannot be replaced by the security runner's Go.
read_release_build_go() {
  local binary=$1 metadata
  local -a versions

  [[ -f "$binary" && -r "$binary" && -x "$binary" ]] ||
    die "Release binary is not a readable executable: $binary"

  if ! metadata=$(go version -m "$binary" 2>&1); then
    die "go version -m failed for $binary"
  fi

  mapfile -t versions < <(
    {
      # The first line is the stable format emitted by Go 1.18+.
      sed -nE '1s/^.*: (go[0-9]+\.[0-9]+(\.[0-9]+)?([[:alnum:]_.+-]*)?)$/\1/p' <<<"$metadata"
      # Keep compatibility with the explicit build setting emitted by newer Go.
      awk '$1 == "build" && $2 == "GoVersion" { print $3 }' <<<"$metadata"
    } | sed '/^$/d' | sort -u
  )

  if [[ "${#versions[@]}" -ne 1 || ! "${versions[0]}" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?([[:alnum:]_.+-]*)?$ ]]; then
    die "Release binary has no valid Go build metadata: $binary"
  fi
  printf '%s\n' "${versions[0]}"
}

# verify_release_build_go DIST EXTRACTED
#
# Extract each downloaded main-program archive and inspect the actual ecs binary
# inside it.  The archive set must cover the shared seven-architecture list, and
# all binaries must report exactly the same build toolchain.  The caller receives
# that one authoritative version on stdout; diagnostics stay on stderr so the
# result remains safe to append to GITHUB_OUTPUT.
verify_release_build_go() {
  local dist=$1 extracted=$2
  local listing name binary arch version expected=""
  local -A seen=()

  mkdir -p "$extracted"
  if ! listing=$(ecs_release_binaries "$dist" "$extracted"); then
    die "无法解出 Release 主程序归档"
  fi
  [[ -n "$listing" ]] || die "Release 中未找到主程序二进制"

  while IFS=$'\t' read -r name binary; do
    [[ -n "$name" && -n "$binary" ]] || die "Release 主程序清单包含空行"
    arch=${name#ecs_linux_}
    case " ${ECS_ARCHES[*]} " in
      *" $arch "*) ;;
      *) die "Release 主程序包含未知架构：$name" ;;
    esac
    [[ "${seen[$arch]:-0}" -eq 0 ]] || die "Release 主程序重复架构：$arch"
    seen[$arch]=1

    version=$(read_release_build_go "$binary")
    echo "scan-released: $name build Go $version" >&2
    if [[ -z "$expected" ]]; then
      expected=$version
    elif [[ "$version" != "$expected" ]]; then
      die "Release 主程序 Go 工具链不一致：$name=$version，want $expected"
    fi
  done <<<"$listing"

  for arch in "${ECS_ARCHES[@]}"; do
    [[ "${seen[$arch]:-0}" -eq 1 ]] || die "Release 缺少主程序架构：$arch"
  done
  [[ -n "$expected" ]] || die "Release 主程序没有统一的 Go 工具链版本"
  printf '%s\n' "$expected"
}

# Keep the verification functions sourceable by the offline regression test.
# The command-line workflow below still runs unchanged when this file is invoked
# as a script.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  return 0
fi

work_dir=""
tag=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --work-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--work-dir requires a value"
      work_dir=$2
      shift 2
      ;;
    --tag)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--tag requires a value"
      tag=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$work_dir" ]] || {
  usage
  die "--work-dir is required"
}
command -v gh >/dev/null 2>&1 || die "gh is required"
command -v go >/dev/null 2>&1 || die "go is required to inspect Release build metadata"

mkdir -p "$work_dir"
work_dir=$(cd "$work_dir" && pwd)
dist="$work_dir/dist"
json_dir="$work_dir/findings"
mkdir -p "$dist" "$json_dir"

if [[ -z "$tag" ]]; then
  # --exclude-drafts / --exclude-pre-releases：只复审真正发给用户的东西。
  tag=$(gh release list --limit 1 --exclude-drafts --exclude-pre-releases \
    --json tagName --jq '.[0].tagName')
  [[ -n "$tag" && "$tag" != "null" ]] || die "找不到任何正式 Release"
fi
echo "scan-released: 复审 $tag" >&2

for arch in "${ECS_ARCHES[@]}"; do
  ecs_retry gh release download "$tag" --dir "$dist" --pattern "ecs_linux_${arch}.tar.gz" --clobber >&2
done

# 下载完整性：摘要不符时扫描结果没有意义。
#
# 本脚本的 stdout 是给 $GITHUB_OUTPUT 用的 key=value，因此这一段的正常输出
# 全部走 stderr。sha256sum -c 每校验一个文件就打印一行 "xxx: 成功"，那些行
# 一旦进入 $GITHUB_OUTPUT，GitHub 会以 Invalid format 让整个 job 失败。
ecs_retry gh release download "$tag" --dir "$dist" --pattern checksums.txt --clobber
(
  cd "$dist"
  grep -E "  ecs_linux_($(
    IFS='|'
    echo "${ECS_ARCHES[*]}"
  ))\.tar\.gz\$" checksums.txt >downloaded-checksums.txt
  sha256sum -c downloaded-checksums.txt >&2
  rm -f downloaded-checksums.txt checksums.txt
)
echo "scan-released: $tag 的 ${#ECS_ARCHES[@]} 个主程序归档摘要一致" >&2

# The runner's Go is only the reader for build metadata.  The value passed to
# triage must come from the binaries users downloaded, not from `go env
# GOVERSION` on this security runner.
release_build_go=$(verify_release_build_go "$dist" "$work_dir/released-binaries")
printf 'release_build_go=%s\n' "$release_build_go"

# release/security.sh 在发现漏洞时以非零退出，这里要的是继续走 triage，
# 因此显式接住它的退出码。
scan_status=0
"$ECS_REPO_ROOT/scripts/release/security.sh" --dist "$dist" --json-dir "$json_dir" || scan_status=$?

printf 'released_tag=%s\n' "$tag"
printf 'findings_dir=%s\n' "$json_dir"
printf 'scan_status=%s\n' "$scan_status"
