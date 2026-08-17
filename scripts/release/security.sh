#!/usr/bin/env bash
set -euo pipefail

# 对一组主程序二进制运行 govulncheck。
#
# 同一份实现服务两个时间点：
#
#   发布前门禁   release.yml 的 security job —— 候选制品现在安全吗？
#   发布后监控   security.yml 的 released job —— 已发布的东西今天还安全吗？
#
# 两者手段完全相同，差别只在 --dist 指向候选制品还是下载回来的已发布制品。
# 做成两份实现的话，跑得少的那一侧迟早会落后。
#
# 扫的是解包出来的实际二进制而不是源码：源码扫描看不见工具链自身的问题，
# 而用户拿到的正是这条工具链编出来的产物。
#
# govulncheck 的版本由 devtools/go.mod 锁定，与 CI 的源码扫描是同一份。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"

usage() {
  echo "usage: scripts/release/security.sh --dist DIR [--json-dir DIR]" >&2
}

die() {
  echo "release-security: $*" >&2
  exit 1
}

dist=""
json_dir=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --dist)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--dist requires a value"
      dist=$2
      shift 2
      ;;
    --json-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--json-dir requires a value"
      json_dir=$2
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

[[ -n "$dist" ]] || {
  usage
  die "--dist is required"
}
[[ "$dist" == /* ]] || dist="$ECS_REPO_ROOT/$dist"
[[ -d "$dist" ]] || die "no such dist directory: $dist"

govulncheck=$(ecs_devtool govulncheck)
scan_root=$(mktemp -d)
trap 'rm -rf -- "$scan_root"' EXIT

# 原样保留 govulncheck 的 JSON：triage 需要 OSV 记录里的 fixed 版本，
# 只留 finding 会把"官方修好了没有"这个信息丢掉。
[[ -n "$json_dir" ]] && mkdir -p "$json_dir"

# govulncheck 的正常输出走 stderr：本脚本是一道门禁，它的结论是退出码。
# stdout 必须保持干净——scan_released.sh 会把自己的 stdout 直接写进
# $GITHUB_OUTPUT，一行不带 '=' 的噪音就会让整个 job 以 Invalid format 失败。
listing=$(ecs_release_binaries "$dist" "$scan_root") || die "无法解出主程序二进制"

failed=0
scanned=0
while IFS=$'\t' read -r name binary; do
  echo "release-security: 扫描 $name" >&2
  if [[ -n "$json_dir" ]]; then
    # -format json 在有漏洞时也返回 0，因此判定必须看内容而不是退出码。
    "$govulncheck" -format json -mode binary "$binary" >"$json_dir/$name.json"
  fi
  if ! "$govulncheck" -mode binary "$binary" >&2; then
    echo "release-security: $name 存在已知漏洞" >&2
    failed=1
  fi
  scanned=$((scanned + 1))
done <<<"$listing"

[[ "$failed" -eq 0 ]] || die "存在已知漏洞"

echo "release-security: $scanned 个二进制均无已知漏洞" >&2
