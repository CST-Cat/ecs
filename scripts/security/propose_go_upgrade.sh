#!/usr/bin/env bash
set -euo pipefail

# 为一次 Go 补丁工具链升级开 PR。
#
# 这个脚本运行在 security.yml 里唯一带写权限的 job 中，因此它刻意只做三件事：
# 改 go.mod 的一行、提交、开 PR。不编译、不跑 Docker、不调用任何第三方工具——
# 写权限的暴露面越小越好。
#
# 为什么是开 PR 而不是直接推 main：合并与打 tag 仍由人决定，安全重建随后走
# 与常规发布**完全相同**的那一条流水线，不存在第二套发布实现。
#
# 注意：GITHUB_TOKEN 开的 PR，其 CI 会停在 approval-required 状态，需要有写
# 权限的人在 PR 页面点一次 "Approve workflows to run"。这是 GitHub 防止递归
# 触发的既定行为。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

usage() {
  echo "usage: scripts/security/propose_go_upgrade.sh --to 1.26.7 --reason TEXT [--osv-ids IDS]" >&2
}

die() {
  echo "propose-go-upgrade: $*" >&2
  exit 1
}

target=""
reason=""
osv_ids=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --to)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--to requires a value"
      target=$2
      shift 2
      ;;
    --reason)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--reason requires a value"
      reason=$2
      shift 2
      ;;
    --osv-ids)
      [[ "$#" -ge 2 ]] || die "--osv-ids requires a value"
      osv_ids=$2
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

[[ -n "$target" ]] || {
  usage
  die "--to is required"
}
[[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "--to must look like 1.26.7, got $target"
command -v gh >/dev/null 2>&1 || die "gh is required"

current=$(awk '$1 == "go" { print $2; exit }' go.mod)
[[ -n "$current" ]] || die "go.mod 里没有 go 指令"
[[ "$current" != "$target" ]] || {
  echo "propose-go-upgrade: go.mod 已经是 $target，无需开 PR" >&2
  exit 0
}

# go mod tidy 会删掉与 go 指令等值的 toolchain 行，所以正常情况下它不存在。
# 万一有人加了回来，只改 go 指令就会留下一个更低的 toolchain——而 setup-go
# 优先读 toolchain，升级会静默失效。宁可在这里停住，交给人处理。
if grep -qE '^toolchain[[:space:]]' go.mod; then
  die "go.mod 里有 toolchain 指令，本脚本只改 go 指令，请人工升级以免留下陈旧工具链"
fi

# gh 失败时不能当成"没有 PR"——那会在已有 PR 的情况下再开一个。
# set -e 下赋值失败会直接中止，正是想要的：查不到就别猜。
branch="security/go-$target"
open_prs=$(gh pr list --head "$branch" --state open --json number --jq 'length')
[[ "$open_prs" == "0" ]] || {
  echo "propose-go-upgrade: $branch 已有 $open_prs 个开着的 PR，跳过" >&2
  exit 0
}

echo "propose-go-upgrade: $current -> $target" >&2

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
# -B 而不是 -b：上一次的分支可能还留在远端（PR 被关掉但分支没删）。
git checkout -B "$branch"

# 只改 go 指令那一行。go.mod 是工具链版本的唯一事实来源，因此这一处改完，
# CI、security、release 三条路径读到的都是新版本。
awk -v target="$target" '$1 == "go" && !done { print "go " target; done = 1; next } { print }' \
  go.mod >go.mod.new
mv go.mod.new go.mod
grep -qxF "go $target" go.mod || die "go.mod 改写失败"

body_file=$(mktemp)
trap 'rm -f -- "$body_file"' EXIT
{
  echo "自动提出的 Go 补丁工具链升级：\`$current\` → \`$target\`。"
  echo
  echo "## 为什么"
  echo
  echo "security.yml 对**已发布的**七架构二进制做每日复审时发现：$reason"
  if [[ -n "$osv_ids" ]]; then
    echo
    echo "涉及漏洞：$osv_ids"
  fi
  echo
  echo "全部命中都来自 Go stdlib/toolchain，且官方已在同一小版本系列内给出修复版。"
  echo "这意味着修复是一个可以机械确认的等式：同一份 ecs 源码 + 修好的工具链。"
  echo "ecs 自身的源码不需要任何改动。"
  echo
  echo "## 合并后要做什么"
  echo
  echo "1. 确认本 PR 的 CI 全绿（bot 开的 PR 需要先点一次 **Approve workflows to run**）；"
  echo "2. 合并到 main；"
  echo "3. 打新的 patch tag，走与常规发布**完全相同**的 release.yml 流水线："
  echo "   preflight → tools×7 → assemble → verify/security → attest → publish。"
  echo
  echo "安全重建不走简化路径，制品验证标准与常规发布一致。"
  echo
  echo "---"
  echo "由 \`scripts/security/propose_go_upgrade.sh\` 生成。"
} >"$body_file"

git add go.mod
git commit -m "安全：Go 工具链升级到 $target

$reason"
# --force：security/go-* 完全由本脚本管理，内容是确定的（main 之上改一行
# go.mod）。PR 关掉但分支没删时，不强推就会因为非快进而失败。这个命名空间
# 里没有人工提交可丢。
git push --force --set-upstream origin "$branch"

gh pr create \
  --title "安全：Go 工具链 $current → $target" \
  --body-file "$body_file" \
  --base main \
  --head "$branch"

echo "propose-go-upgrade: PR 已创建" >&2
