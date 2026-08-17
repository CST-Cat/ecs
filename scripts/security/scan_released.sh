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

# release/security.sh 在发现漏洞时以非零退出，这里要的是继续走 triage，
# 因此显式接住它的退出码。
scan_status=0
"$ECS_REPO_ROOT/scripts/release/security.sh" --dist "$dist" --json-dir "$json_dir" || scan_status=$?

printf 'released_tag=%s\n' "$tag"
printf 'findings_dir=%s\n' "$json_dir"
printf 'scan_status=%s\n' "$scan_status"
