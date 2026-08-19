#!/usr/bin/env bash
set -euo pipefail

# 为一次 Go 补丁正式编译器升级开 PR。
#
# Release 编译器的唯一事实来源是 .github/workflows/release.yml 里的
# ECS_RELEASE_GO。本脚本只更新这一个 pin：根 go.mod 仍是源码最低兼容版本，
# devtools/go.mod 仍是安全工具自身的构建环境，两者都不属于 Release 升级。
#
# 这个脚本运行在 security.yml 里唯一带写权限的 job 中，因此它刻意只做三件事：
# 改 Release workflow 的一行、提交、开 PR。不编译、不跑 Docker、不调用任何第三方
# 工具——写权限的暴露面越小越好。
#
# 为什么是开 PR 而不是直接推 main：合并与打 tag 仍由人决定，安全重建随后走
# 与常规发布完全相同的那一条流水线，不存在第二套发布实现。
#
# 注意：GITHUB_TOKEN 开的 PR，其 CI 会停在 approval-required 状态，需要有写
# 权限的人在 PR 页面点一次 "Approve workflows to run"。这是 GitHub 防止递归
# 触发的既定行为。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

release_workflow="$repo_root/.github/workflows/release.yml"

usage() {
  echo "usage: scripts/security/propose_go_upgrade.sh --to 1.26.7 --reason TEXT [--osv-ids IDS]" >&2
}

die() {
  echo "propose-go-upgrade: $*" >&2
  exit 1
}

# Go 版本必须是一个没有 go 前缀、没有 beta/rc 后缀的精确 patch 版本。
# Release policy 同样要求 1.x.y；在这里提前拒绝其它形式，避免把外部 triage
# 输出直接写进 workflow。
valid_go_version() {
  [[ "$1" =~ ^1\.[0-9]+\.[0-9]+$ ]]
}

# 比较两个已经通过 valid_go_version 的版本。返回 0 表示 left 严格大于 right。
# 使用 10# 避免例如 patch 08 被 Bash 当成八进制数。
go_version_gt() {
  local left=$1 right=$2
  local left_major left_minor left_patch
  local right_major right_minor right_patch

  IFS=. read -r left_major left_minor left_patch <<<"$left"
  IFS=. read -r right_major right_minor right_patch <<<"$right"

  if ((10#$left_major != 10#$right_major)); then
    ((10#$left_major > 10#$right_major))
    return
  fi
  if ((10#$left_minor != 10#$right_minor)); then
    ((10#$left_minor > 10#$right_minor))
    return
  fi
  ((10#$left_patch > 10#$right_patch))
}

# 从 workflow 中读取 ECS_RELEASE_GO。这里不解析整份 YAML；只接受明确的键值
# 赋值，并严格要求该赋值恰好出现一次。这样重复 pin 或把值写成 YAML 复杂对象
# 时会在任何 checkout/写入动作之前失败。
release_pin_value() {
  local workflow=$1
  local pin_count pin_line raw quote

  [[ -f "$workflow" ]] || die "找不到 Release workflow: $workflow"

  pin_count=$(awk '
    /^[[:space:]]*ECS_RELEASE_GO[[:space:]]*:/ { count++ }
    END { print count + 0 }
  ' "$workflow")
  [[ "$pin_count" == "1" ]] ||
    die "$workflow 中 ECS_RELEASE_GO 出现 $pin_count 次，必须恰好一次"

  pin_line=$(awk '
    /^[[:space:]]*ECS_RELEASE_GO[[:space:]]*:/ { print; exit }
  ' "$workflow")
  raw=${pin_line#*:}
  # 只去掉值两侧的空白；值内部的空白和注释字符应使版本校验失败。
  raw=$(sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//' <<<"$raw")

  # YAML 可以用单引号或双引号；不接受不成对的引号或引号内再出现引号。
  if [[ "$raw" == \"* || "$raw" == \'* ]]; then
    quote=${raw:0:1}
    [[ "${raw: -1}" == "$quote" && ${#raw} -ge 2 ]] ||
      die "ECS_RELEASE_GO 的值引号不成对: $pin_line"
    raw=${raw:1:${#raw}-2}
    [[ "$raw" != *"$quote"* ]] ||
      die "ECS_RELEASE_GO 的值包含多余引号: $pin_line"
  fi

  valid_go_version "$raw" || die "ECS_RELEASE_GO 不是精确 Go 版本: $raw"
  printf '%s\n' "$raw"
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
valid_go_version "$target" || die "--to must look like 1.26.7, got $target"

current=$(release_pin_value "$release_workflow")
if [[ "$current" == "$target" ]]; then
  echo "propose-go-upgrade: Release compiler 已经是 $target，无需开 PR" >&2
  exit 0
fi
go_version_gt "$target" "$current" ||
  die "目标 Release compiler $target 不是当前版本 $current 的升级"

# gh 失败时不能当成"没有 PR"——那会在已有 PR 的情况下再开一个。
# 所有版本和 pin 校验已在这里完成，查 gh 失败也不会留下文件半修改。
command -v gh >/dev/null 2>&1 || die "gh is required"

branch="security/go-$target"
open_prs=$(gh pr list --head "$branch" --state open --json number --jq 'length')
[[ "$open_prs" == "0" ]] || {
  echo "propose-go-upgrade: $branch 已有 $open_prs 个开着的 PR，跳过" >&2
  exit 0
}

echo "propose-go-upgrade: Release compiler $current -> $target" >&2

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
# -B 而不是 -b：上一次的分支可能还留在远端（PR 被关掉但分支没删）。
git checkout -B "$branch"

replacement_tmp=""
release_backup=""
release_replaced=0
commit_created=0
cleanup() {
  if [[ -n "$replacement_tmp" ]]; then
    rm -f -- "$replacement_tmp"
  fi
  # 在 replacement 已完成但 commit 尚未完成时恢复原文件，避免 git/awk 失败
  # 留下半个升级。commit 成功后则保留提交，供 push/PR 阶段继续处理。
  if [[ "$release_replaced" == "1" && "$commit_created" == "0" &&
    -n "$release_backup" && -f "$release_backup" ]]; then
    cp -- "$release_backup" "$release_workflow"
  fi
  if [[ -n "$release_backup" ]]; then
    rm -f -- "$release_backup"
  fi
}
trap cleanup EXIT

# 备份只用于 commit 之前的失败回滚；临时文件与目标在同一目录，mv 才是原子的。
release_backup=$(mktemp "${release_workflow}.backup.XXXXXX")
cp -- "$release_workflow" "$release_backup"
replacement_tmp=$(mktemp "${release_workflow}.tmp.XXXXXX")
awk -v target="$target" '
  /^[[:space:]]*ECS_RELEASE_GO[[:space:]]*:/ {
    count++
    if (count == 1) {
      prefix = $0
      sub(/[[:space:]]*:[[:space:]].*/, "", prefix)
      print prefix ": \"" target "\""
      next
    }
  }
  { print }
  END {
    if (count != 1) exit 1
  }
' "$release_workflow" >"$replacement_tmp" ||
  die "Release workflow pin 替换失败"

# 重新解析生成物，确保 pin 仍恰好一次且值就是目标；校验通过后才替换目标文件。
new_pin_count=$(awk '
  /^[[:space:]]*ECS_RELEASE_GO[[:space:]]*:/ { count++ }
  END { print count + 0 }
' "$replacement_tmp")
[[ "$new_pin_count" == "1" ]] ||
  die "替换结果中 ECS_RELEASE_GO 出现 $new_pin_count 次，必须恰好一次"
new_pin=$(release_pin_value "$replacement_tmp")
[[ "$new_pin" == "$target" ]] ||
  die "替换结果不是目标 Release compiler $target"

# mktemp 默认是 0600；保留 workflow 原有权限，避免一次正常升级产生无关 mode diff。
chmod --reference="$release_workflow" "$replacement_tmp"
mv -- "$replacement_tmp" "$release_workflow"
replacement_tmp=""
release_replaced=1

body_file=$(mktemp)
trap 'rm -f -- "$body_file"; cleanup' EXIT
{
  echo "自动提出的 Release Go 编译器升级：\`$current\` → \`$target\`。"
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
  echo "这意味着修复是一个可以机械确认的等式：同一份 ecs 源码 + 修好的 Release 编译器。"
  echo "ecs 自身的源码不需要任何改动，go.mod 的最低兼容版本也保持不变。"
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

git add "$release_workflow"
git commit -m "安全：Release Go 编译器升级到 $target

$reason"
commit_created=1
# --force：security/go-* 完全由本脚本管理，内容是确定的（main 之上改一行
# release.yml）。PR 关掉但分支没删时，不强推就会因为非快进而失败。这个命名空间
# 里没有人工提交可丢。
git push --force --set-upstream origin "$branch"

gh pr create \
  --title "安全：Release Go 编译器 $current → $target" \
  --body-file "$body_file" \
  --base main \
  --head "$branch"

echo "propose-go-upgrade: Release compiler PR 已创建" >&2
