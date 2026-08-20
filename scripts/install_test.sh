#!/usr/bin/env bash
set -euo pipefail

# install.sh 的本地安装回归：fixture 只记录安装器对目标二进制的调用；
# PATH 哨兵保证这些场景既不访问网络，也不调用 sudo 或系统包管理器。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-install-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "install.sh tests: $*" >&2
  exit 1
}

assert_empty_dir() {
  local directory=$1 description=$2
  if find "$directory" -mindepth 1 -print -quit | grep -q .; then
    fail "$description left files under $directory"
  fi
}

fixture_bin="$test_root/fixture-bin"
fixture_log="$test_root/fixture-invocations.log"
forbidden_bin="$test_root/forbidden-bin"
forbidden_log="$test_root/forbidden-invocations.log"
install_dir="$test_root/install"
mkdir -p "$fixture_bin" "$forbidden_bin"

for command_name in sudo apt apt-get dnf yum apk pacman curl wget; do
  cat >"$forbidden_bin/$command_name" <<'EOF'
#!/bin/sh
printf '%s\n' "$0" >>"$ECS_INSTALL_TEST_FORBIDDEN_LOG"
exit 90
EOF
  chmod 0755 "$forbidden_bin/$command_name"
done

write_fixture() {
  local destination=$1 version=$2
  cat >"$destination" <<EOF
#!/bin/sh
set -eu
printf '%s\\t%s\\n' "\$0" "\$*" >>"\$ECS_INSTALL_TEST_INVOCATIONS"
[ "\$#" -eq 1 ] && [ "\$1" = version ] || exit 64
printf '%s\\n' '$version'
EOF
  chmod 0644 "$destination"
}

test_path="$forbidden_bin:$PATH"
export ECS_INSTALL_TEST_INVOCATIONS="$fixture_log"
export ECS_INSTALL_TEST_FORBIDDEN_LOG="$forbidden_log"

# 新安装必须逐字复制本地 fixture、固定为 0755，并实际从目标路径调用 `version`。
fixture_one="$fixture_bin/ecs-one"
write_fixture "$fixture_one" fixture-version-one
if ! ECS_INSTALL_DIR="$install_dir" PATH="$test_path" \
    sh "$repo_root/install.sh" --from "$fixture_one" \
    >"$test_root/install-one.stdout" 2>"$test_root/install-one.stderr"; then
  fail "fresh local install failed: $(<"$test_root/install-one.stderr")"
fi
installed="$install_dir/ecs"
[[ -f "$installed" && ! -L "$installed" ]] || fail "fresh install did not create a regular ecs file"
cmp -s "$fixture_one" "$installed" || fail "fresh install changed the fixture contents"
[[ -x "$installed" ]] || fail "fresh install is not executable"
[[ "$(stat -c '%a' "$installed")" == 755 ]] || fail "fresh install mode is not 0755"
mapfile -t invocations <"$fixture_log"
[[ "${#invocations[@]}" -eq 1 ]] || fail "fresh install invoked ecs ${#invocations[@]} times instead of once"
[[ "${invocations[0]}" == "$installed"$'\tversion' ]] ||
  fail "fresh install validation changed: ${invocations[0]}"
[[ ! -e "$forbidden_log" ]] || fail "default local install called a forbidden command: $(<"$forbidden_log")"

# 替换已有目标仍必须得到完整的新文件，并清理同目录中的安装临时文件。
fixture_two="$fixture_bin/ecs-two"
write_fixture "$fixture_two" fixture-version-two
if ! ECS_INSTALL_DIR="$install_dir" PATH="$test_path" \
    sh "$repo_root/install.sh" --from "$fixture_two" \
    >"$test_root/install-two.stdout" 2>"$test_root/install-two.stderr"; then
  fail "replacement local install failed: $(<"$test_root/install-two.stderr")"
fi
cmp -s "$fixture_two" "$installed" || fail "replacement did not publish the complete new fixture"
[[ "$(stat -c '%a' "$installed")" == 755 ]] || fail "replacement mode is not 0755"
if find "$install_dir" -maxdepth 1 -name '.ecs.install.*' -print -quit | grep -q .; then
  fail "replacement left an atomic-install temporary file"
fi
mapfile -t invocations <"$fixture_log"
[[ "${#invocations[@]}" -eq 2 ]] || fail "two installs invoked ecs ${#invocations[@]} times instead of twice"
[[ "${invocations[1]}" == "$installed"$'\tversion' ]] ||
  fail "replacement validation changed: ${invocations[1]}"
[[ ! -e "$forbidden_log" ]] || fail "replacement install called a forbidden command: $(<"$forbidden_log")"

# 未知选项必须在创建安装目录或临时文件之前给出稳定错误。
unknown_dir="$test_root/unknown-destination"
unknown_tmp="$test_root/unknown-tmp"
mkdir -p "$unknown_tmp"
set +e
TMPDIR="$unknown_tmp" PATH="$test_path" \
  sh "$repo_root/install.sh" --install-dir "$unknown_dir" --definitely-unknown \
  >"$test_root/unknown.stdout" 2>"$test_root/unknown.stderr"
unknown_status=$?
set -e
[[ "$unknown_status" -eq 1 ]] || fail "unknown option returned $unknown_status instead of 1"
[[ "$(head -n 1 "$test_root/unknown.stderr")" == 'unknown option: --definitely-unknown' ]] ||
  fail "unknown-option diagnostic changed: $(head -n 1 "$test_root/unknown.stderr")"
[[ ! -e "$unknown_dir" ]] || fail "unknown option created the install directory"
assert_empty_dir "$unknown_tmp" "unknown option"

# 缺失 --from 的值也必须在任何目标写入或临时目录创建之前失败。
missing_dir="$test_root/missing-destination"
missing_tmp="$test_root/missing-tmp"
mkdir -p "$missing_tmp"
set +e
TMPDIR="$missing_tmp" PATH="$test_path" \
  sh "$repo_root/install.sh" --install-dir "$missing_dir" --from \
  >"$test_root/missing.stdout" 2>"$test_root/missing.stderr"
missing_status=$?
set -e
[[ "$missing_status" -eq 1 ]] || fail "missing --from value returned $missing_status instead of 1"
[[ "$(<"$test_root/missing.stderr")" == 'missing value for --from' ]] ||
  fail "missing --from diagnostic changed: $(<"$test_root/missing.stderr")"
[[ ! -e "$missing_dir" ]] || fail "missing --from value created the install directory"
assert_empty_dir "$missing_tmp" "missing --from value"
[[ ! -e "$forbidden_log" ]] || fail "an early failure called a forbidden command: $(<"$forbidden_log")"

echo "install.sh behavior tests passed"
