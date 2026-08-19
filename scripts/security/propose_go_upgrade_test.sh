#!/usr/bin/env bash
set -euo pipefail

# propose_go_upgrade.sh 的文件边界回归。
#
# 每个用例都在临时 Git 仓库中运行真实脚本；唯一 fake 的外部命令是 gh，Git
# push 使用同一临时目录里的 bare remote。这样既能验证提交/PR 文案，又不会访问
# GitHub 或改动当前 checkout。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
upgrade_script="$repo_root/scripts/security/propose_go_upgrade.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-propose-go-upgrade-test.XXXXXX")
fake_bin="$work/bin"
gh_log="$work/gh.log"
mkdir -p "$fake_bin"
trap 'rm -rf -- "$work"' EXIT

fail() {
  echo "propose-go-upgrade tests: $*" >&2
  exit 1
}

printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'if [[ "${1:-}" == pr && "${2:-}" == list ]]; then' \
  '  echo 0' \
  'elif [[ "${1:-}" == pr && "${2:-}" == create ]]; then' \
  '  printf "%s\\n" "$*" >>"${ECS_PROPOSE_TEST_GH_LOG:?}"' \
  'else' \
  '  echo "unexpected gh invocation: $*" >&2' \
  '  exit 1' \
  'fi' >"$fake_bin/gh"
chmod 0755 "$fake_bin/gh"

new_fixture() {
  local name=$1 kind=$2
  local fixture="$work/$name"
  local remote="$work/$name.remote.git"

  mkdir -p "$fixture/.github/workflows" "$fixture/devtools" "$fixture/scripts/security"
  cp "$upgrade_script" "$fixture/scripts/security/propose_go_upgrade.sh"
  case "$kind" in
    valid)
      printf '%s\n' \
        'name: release' \
        'env:' \
        '  ECS_RELEASE_GO: "1.25.3"' \
        >"$fixture/.github/workflows/release.yml"
      ;;
    malformed)
      printf '%s\n' \
        'name: release' \
        'env:' \
        '  ECS_RELEASE_GO: "not-a-version"' \
        >"$fixture/.github/workflows/release.yml"
      ;;
    duplicate)
      printf '%s\n' \
        'name: release' \
        'env:' \
        '  ECS_RELEASE_GO: "1.25.3"' \
        '  ECS_RELEASE_GO: "1.25.4"' \
        >"$fixture/.github/workflows/release.yml"
      ;;
    *)
      fail "unknown fixture kind: $kind"
      ;;
  esac
  printf '%s\n' 'module ecs' 'go 1.22' >"$fixture/go.mod"
  printf '%s\n' 'module ecs/devtools' 'go 1.24.7' >"$fixture/devtools/go.mod"

  git -C "$fixture" init -q
  git -C "$fixture" config user.name fixture
  git -C "$fixture" config user.email fixture@example.invalid
  git -C "$fixture" branch -M main
  git -C "$fixture" add .
  git -C "$fixture" commit -qm initial
  git init --bare -q "$remote"
  git -C "$fixture" remote add origin "$remote"
  git -C "$fixture" push -q --set-upstream origin main
  printf '%s\n' "$fixture"
}

run_script() {
  local fixture=$1
  shift
  (cd "$fixture" &&
    ECS_PROPOSE_TEST_GH_LOG="$gh_log" PATH="$fake_bin:$PATH" \
      scripts/security/propose_go_upgrade.sh "$@")
}

assert_clean_mods() {
  local fixture=$1 root_hash=$2 devtools_hash=$3
  [[ "$(sha256sum "$fixture/go.mod")" == "$root_hash" ]] ||
    fail "$fixture/go.mod was modified"
  [[ "$(sha256sum "$fixture/devtools/go.mod")" == "$devtools_hash" ]] ||
    fail "$fixture/devtools/go.mod was modified"
}

assert_no_worktree_changes() {
  local fixture=$1
  [[ -z "$(git -C "$fixture" status --short)" ]] ||
    fail "$fixture has unexpected worktree changes"
}

# ---- 正常升级：只改 Release pin，两个 go.mod 字节不变 ----
valid_fixture=$(new_fixture valid valid)
valid_root_hash=$(sha256sum "$valid_fixture/go.mod")
valid_devtools_hash=$(sha256sum "$valid_fixture/devtools/go.mod")
run_script "$valid_fixture" --to 1.25.4 --reason "fixture security finding" >/dev/null 2>&1 ||
  fail "valid upgrade failed"
grep -Fqx '  ECS_RELEASE_GO: "1.25.4"' \
  "$valid_fixture/.github/workflows/release.yml" || fail "Release pin was not upgraded"
assert_clean_mods "$valid_fixture" "$valid_root_hash" "$valid_devtools_hash"
assert_no_worktree_changes "$valid_fixture"
changed_files=$(git -C "$valid_fixture" diff-tree --no-commit-id --name-only -r HEAD)
[[ "$changed_files" == ".github/workflows/release.yml" ]] ||
  fail "upgrade commit changed unexpected files: $changed_files"
commit_subject=$(git -C "$valid_fixture" show -s --format=%s HEAD)
[[ "$commit_subject" == "安全：Release Go 编译器升级到 1.25.4" ]] ||
  fail "upgrade commit has stale semantics: $commit_subject"
grep -F 'Release Go 编译器 1.25.3 → 1.25.4' "$gh_log" >/dev/null ||
  fail "PR title does not name the Release compiler pin"
echo "ok   valid upgrade changes only release pin"

# 失败前的 fixture 都处在干净 main；所有失败必须留下完整原文件，且不创建升级分支。
assert_rejected_without_changes() {
  local name=$1 kind=$2 target=$3
  local fixture root_hash devtools_hash output
  fixture=$(new_fixture "$name" "$kind")
  root_hash=$(sha256sum "$fixture/go.mod")
  devtools_hash=$(sha256sum "$fixture/devtools/go.mod")
  if output=$(run_script "$fixture" --to "$target" --reason "fixture security finding" 2>&1); then
    fail "$name unexpectedly succeeded"
  fi
  assert_clean_mods "$fixture" "$root_hash" "$devtools_hash"
  assert_no_worktree_changes "$fixture"
  [[ -z "$(git -C "$fixture" branch --list "security/go-$target")" ]] ||
    fail "$name created an upgrade branch before validation"
  echo "$output" | grep -F 'propose-go-upgrade:' >/dev/null ||
    fail "$name did not report a bounded validation error"
  echo "ok   $name rejects without half modification"
}

assert_rejected_without_changes malformed_pin malformed 1.25.4
assert_rejected_without_changes duplicate_pin duplicate 1.25.5

# ---- no-op 与降级 ----
noop_fixture=$(new_fixture noop valid)
noop_root_hash=$(sha256sum "$noop_fixture/go.mod")
noop_devtools_hash=$(sha256sum "$noop_fixture/devtools/go.mod")
run_script "$noop_fixture" --to 1.25.3 --reason "fixture security finding" >/dev/null 2>&1 ||
  fail "no-op unexpectedly failed"
assert_clean_mods "$noop_fixture" "$noop_root_hash" "$noop_devtools_hash"
assert_no_worktree_changes "$noop_fixture"
[[ -z "$(git -C "$noop_fixture" branch --list 'security/go-1.25.3')" ]] ||
  fail "no-op created an upgrade branch"
echo "ok   no-op leaves release pin untouched"

assert_rejected_without_changes downgrade valid 1.25.2
assert_rejected_without_changes malformed_target valid not-a-version

echo "propose-go-upgrade tests passed"
