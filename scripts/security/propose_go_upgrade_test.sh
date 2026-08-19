#!/usr/bin/env bash
set -euo pipefail

# 只验证自动升级修改 Release compiler pin，而不改两个 go.mod。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
upgrade_script="$repo_root/scripts/security/propose_go_upgrade.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-propose-go-upgrade-test.XXXXXX")
fake_bin="$work/bin"
mkdir -p "$fake_bin"
trap 'rm -rf -- "$work"' EXIT

cat >"$fake_bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == pr && "${2:-}" == list ]]; then
  echo 0
elif [[ "${1:-}" == pr && "${2:-}" == create ]]; then
  exit 0
else
  echo "unexpected gh invocation: $*" >&2
  exit 1
fi
SH
chmod 0755 "$fake_bin/gh"

fixture="$work/fixture"
remote="$work/remote.git"
mkdir -p "$fixture/.github/workflows" "$fixture/devtools" "$fixture/scripts/security"
cp "$upgrade_script" "$fixture/scripts/security/propose_go_upgrade.sh"
printf '%s\n' 'name: release' 'env:' '  ECS_RELEASE_GO: "1.25.3"' >"$fixture/.github/workflows/release.yml"
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

root_hash=$(sha256sum "$fixture/go.mod")
devtools_hash=$(sha256sum "$fixture/devtools/go.mod")
(cd "$fixture" && PATH="$fake_bin:$PATH" scripts/security/propose_go_upgrade.sh \
  --to 1.25.4 --reason "fixture finding" >/dev/null)

grep -Fqx '  ECS_RELEASE_GO: "1.25.4"' "$fixture/.github/workflows/release.yml" ||
  { echo "Release pin was not upgraded" >&2; exit 1; }
[[ "$(sha256sum "$fixture/go.mod")" == "$root_hash" ]] || { echo "go.mod changed" >&2; exit 1; }
[[ "$(sha256sum "$fixture/devtools/go.mod")" == "$devtools_hash" ]] || { echo "devtools/go.mod changed" >&2; exit 1; }
[[ -z "$(git -C "$fixture" status --short)" ]] || { echo "fixture is dirty" >&2; exit 1; }
[[ "$(git -C "$fixture" diff-tree --no-commit-id --name-only -r HEAD)" == ".github/workflows/release.yml" ]] ||
  { echo "upgrade changed files outside release pin" >&2; exit 1; }

echo "propose-go-upgrade tests passed"
