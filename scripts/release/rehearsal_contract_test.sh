#!/usr/bin/env bash
set -euo pipefail

# 发布彩排的静态契约门禁。
#
# 这不是为了“证明 YAML 一定能运行”，而是钉死最容易回归、且一旦回归就会产生
# 永久副作用的边界：workflow_dispatch 不能仅凭 tag-shaped ref 获得发布权限；
# SDK 的默认手动模式必须是 rehearsal；三条发布 workflow 的 contents:write
# 只能出现在唯一 publish job；ECS/Bundle 彩排必须走 publish.sh --check-only。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

die() {
  echo "release-rehearsal-contract: $*" >&2
  exit 1
}

assert_contains() {
  local file=$1 needle=$2
  grep -Fq -- "$needle" "$file" || die "$file missing contract: $needle"
}

assert_absent() {
  local file=$1 needle=$2
  if grep -Fq -- "$needle" "$file"; then
    die "$file contains forbidden legacy contract: $needle"
  fi
}

assert_single_write_job() {
  local file=$1 count
  count=$(grep -cE '^[[:space:]]+contents: write$' "$file" || true)
  [[ "$count" -eq 1 ]] || die "$file must contain exactly one contents: write, got $count"
}

release=.github/workflows/release.yml
bundle=.github/workflows/bundle-release.yml
sdk=.github/workflows/freebsd-sdk-release.yml
freeze=scripts/release/freeze.sh
publisher=scripts/release/publish.sh

# ECS：正式发布必须同时是 push 与 v* tag；dispatch 不论选 branch 还是 tag 都是彩排。
assert_contains "$release" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"
assert_contains "$release" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$release" "--check-only"
assert_absent "$release" "    if: startsWith(github.ref, 'refs/tags/v')"
assert_single_write_job "$release"

# Bundle：同样双重门禁，并在正式出口校验触发 tag 等于 tools/BUNDLE。
assert_contains "$bundle" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/bundle-v')"
assert_contains "$bundle" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$bundle" "bundle trigger tag"
assert_contains "$bundle" "--check-only"
assert_absent "$bundle" "    if: startsWith(github.ref, 'refs/tags/bundle-v')"
assert_single_write_job "$bundle"

# SDK：手动入口默认 rehearsal；publish 需要显式模式 + sdk_version，prepare 与
# rehearsal 本身保持只读，只有 guarded publish 拿写权限。正式版本还必须读取
# 已有 SDK tag 并用 sort -V 证明输入严格单调递增。
assert_contains "$sdk" "release_mode:"
assert_contains "$sdk" "default: rehearsal"
assert_contains "$sdk" "- rehearsal"
assert_contains "$sdk" "- publish"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'rehearsal'"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'publish'"
assert_contains "$sdk" "Prepare SDK release candidate"
assert_contains "$sdk" "matching-refs/tags/ci-freebsd-gnu-sdk-v"
assert_contains "$sdk" "sort -V"
assert_contains "$sdk" "must be strictly newer than existing maximum"
assert_contains "$sdk" "gh release create"
assert_single_write_job "$sdk"

sdk_version_block=$(grep -A4 -F '      sdk_version:' "$sdk" || true)
grep -Fq 'required: false' <<<"$sdk_version_block" ||
  die "$sdk sdk_version must be optional for rehearsal"

# freeze 必须先按 event 分流；workflow_dispatch 分支必须排在 push 分支之前。
dispatch_line=$(grep -nE '^[[:space:]]+workflow_dispatch\)' "$freeze" | head -1 | cut -d: -f1)
push_line=$(grep -nE '^[[:space:]]+push\)' "$freeze" | head -1 | cut -d: -f1)
[[ -n "$dispatch_line" && -n "$push_line" && "$dispatch_line" -lt "$push_line" ]] ||
  die "freeze.sh must classify workflow_dispatch before push/tag parsing"

# check-only 是发布脚本自己的硬边界，而不是只靠 workflow 注释约定。
assert_contains "$publisher" "--check-only"
assert_contains "$publisher" "未访问或修改 GitHub Release"
check_only_line=$(grep -nE '^[[:space:]]*if \[\[ "\$check_only" -eq 1 \]\]' "$publisher" | tail -1 | cut -d: -f1)
gh_line=$(grep -nE '^[[:space:]]*command -v gh ' "$publisher" | head -1 | cut -d: -f1)
[[ -n "$check_only_line" && -n "$gh_line" && "$check_only_line" -lt "$gh_line" ]] ||
  die "publish.sh check-only must return before gh becomes a dependency"

echo "release-rehearsal-contract: all rehearsal/publish boundaries are pinned"
