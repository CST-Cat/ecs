#!/usr/bin/env bash
set -euo pipefail

# integration.sh 的 apt 安装回归测试。
#
# 测试只 source 安装函数，并把 timeout、sudo、apt-get 都替换成临时 fake 命令。
# apt 源也只放在临时目录；不会联网、提权或调用宿主包管理器。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
integration_script="$repo_root/scripts/ci/integration.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-integration-test.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

fake_bin="$work/bin"
apt_root="$work/apt"
test_tmp="$work/tmp"
log_file="$work/commands.log"
state_file="$work/apt-state"
mkdir -p "$fake_bin" "$apt_root/sources.list.d" "$test_tmp"

cat >"$apt_root/sources.list" <<'SOURCES'
deb http://azure.archive.ubuntu.com/ubuntu noble main
deb-src http://azure.archive.ubuntu.com/ubuntu noble main
SOURCES
cat >"$apt_root/sources.list.d/third-party.sources" <<'SOURCES'
Types: deb
URIs: https://packages.example.invalid/repository
Suites: stable
Components: main
SOURCES

cat >"$fake_bin/timeout" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

log=${ECS_APT_TEST_LOG:?}
printf 'timeout\t%s\n' "$*" >>"$log"
[[ "${1:-}" == --signal=TERM ]] || { echo "fake timeout: missing TERM signal" >&2; exit 90; }
shift
[[ "${1:-}" == --kill-after=15s ]] || { echo "fake timeout: missing kill-after" >&2; exit 91; }
shift
[[ "${1:-}" == 5m ]] || { echo "fake timeout: missing five-minute hard timeout" >&2; exit 92; }
shift
"$@"
FAKE

cat >"$fake_bin/sudo" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

log=${ECS_APT_TEST_LOG:?}
printf 'sudo\t%s\n' "$*" >>"$log"
while (($# > 0)) && [[ "$1" == *=* ]]; do
  shift
done
"$@"
FAKE

cat >"$fake_bin/apt-get" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

log=${ECS_APT_TEST_LOG:?}
state=${ECS_APT_TEST_STATE:?}
mode=${ECS_FAKE_APT_MODE:-success}
action=
source_list=
source_parts=
for arg in "$@"; do
  case "$arg" in
    update|install) action=$arg ;;
    Dir::Etc::sourcelist=*) source_list=${arg#*=} ;;
    Dir::Etc::sourceparts=*) source_parts=${arg#*=} ;;
  esac
done
{
  printf 'apt-get\t%s' "$action"
  printf '\t%s' "$@"
  printf '\n'
} >>"$log"

expect_fallback_sources() {
  [[ -n "$source_list" && -f "$source_list" ]] || {
    echo "fake apt: fallback sourcelist was not supplied" >&2
    exit 93
  }
  grep -Fqx 'deb http://archive.ubuntu.com/ubuntu noble main' "$source_list" || {
    echo "fake apt: Azure source was not switched in the fallback list" >&2
    exit 94
  }
  grep -Fqx 'deb-src http://archive.ubuntu.com/ubuntu noble main' "$source_list" || {
    echo "fake apt: deb-src Azure source was not switched" >&2
    exit 95
  }
  if grep -Fq 'azure.archive.ubuntu.com' "$source_list"; then
    echo "fake apt: fallback list still contains Azure" >&2
    exit 96
  fi
  [[ -n "$source_parts" && -f "$source_parts/third-party.sources" ]] || {
    echo "fake apt: third-party source was not copied" >&2
    exit 97
  }
  grep -Fqx 'URIs: https://packages.example.invalid/repository' \
    "$source_parts/third-party.sources" || {
    echo "fake apt: third-party source was modified" >&2
    exit 98
  }
}

if [[ "$action" == update ]]; then
  update_count=0
  [[ -s "$state" ]] && update_count=$(<"$state")
  if [[ "$update_count" -eq 0 ]]; then
    printf '1\n' >"$state"
    echo "fake apt: original Azure update failed" >&2
    exit 41
  fi
  expect_fallback_sources
  if [[ "$mode" == fallback-update-fail ]]; then
    echo "fake apt: fallback update failed" >&2
    exit 43
  fi
  printf '2\n' >"$state"
  exit 0
fi

if [[ "$action" == install ]]; then
  expect_fallback_sources
  if [[ "$mode" == fallback-install-fail ]]; then
    echo "fake apt: fallback install failed" >&2
    exit 47
  fi
  exit 0
fi

echo "fake apt: unknown operation" >&2
exit 99
FAKE

chmod 0755 "$fake_bin/timeout" "$fake_bin/sudo" "$fake_bin/apt-get"

export ECS_APT_SOURCE_ROOT="$apt_root"
export ECS_APT_TIMEOUT_COMMAND="$fake_bin/timeout"
export ECS_APT_SUDO_COMMAND="$fake_bin/sudo"
export ECS_APT_GET_COMMAND="$fake_bin/apt-get"
export ECS_APT_TEST_LOG="$log_file"
export ECS_APT_TEST_STATE="$state_file"
export TMPDIR="$test_tmp"

source "$integration_script"

fail() {
  echo "integration apt regression: $*" >&2
  exit 1
}

assert_contains() {
  local haystack=$1 needle=$2 description=$3
  [[ "$haystack" == *"$needle"* ]] || fail "$description (missing: $needle)"
}

assert_file_contains() {
  local path=$1 needle=$2 description=$3
  grep -Fq -- "$needle" "$path" || fail "$description (missing: $needle)"
}

assert_file_not_contains() {
  local path=$1 needle=$2 description=$3
  if grep -Fq -- "$needle" "$path"; then
    fail "$description (unexpected: $needle)"
  fi
}

# 默认本地路径只报告跳过，不执行任何 apt 命令，即使测试提供了 fake apt。
unset ECS_INSTALL_TOOLS CI
rm -f -- "$log_file"
skip_output=$(ecs_install_tools_if_requested should-not-install 2>&1) || {
  fail "默认跳过路径返回失败：$skip_output"
}
assert_contains "$skip_output" '跳过安装' '默认路径没有报告跳过安装'
[[ ! -e "$log_file" ]] || fail '默认路径调用了 fake apt'

# 第一次 update 失败后只切换 Azure 源；备用 update/install 都必须经过同一组
# timeout 与 Acquire 参数。fake apt 会在命令执行期间读取临时备用源并检查第三方
# .sources 文件没有变化。
export ECS_INSTALL_TOOLS=1
export ECS_FAKE_APT_MODE=success
: >"$log_file"
rm -f -- "$state_file"
success_output=$(ecs_install_tools_if_requested fio sysbench 2>&1) || {
  fail "备用源成功路径返回失败：$success_output"
}
assert_contains "$success_output" 'fake apt: original Azure update failed' '没有保留首次 update 错误输出'
assert_file_contains "$apt_root/sources.list" 'azure.archive.ubuntu.com' '宿主（模拟）sources.list 被修改'
assert_file_not_contains "$apt_root/sources.list" 'deb http://archive.ubuntu.com/ubuntu noble main' '原始 sources.list 被切换'
assert_file_contains "$apt_root/sources.list.d/third-party.sources" \
  'URIs: https://packages.example.invalid/repository' '原始第三方源被修改'

apt_calls=$(grep -c $'^apt-get\t' "$log_file")
[[ "$apt_calls" -eq 3 ]] || fail "期望 update、备用 update、install 三次 apt 调用，得到 $apt_calls"
sudo_calls=$(grep -c $'^sudo\t' "$log_file")
[[ "$sudo_calls" -eq 3 ]] || fail "期望三次 sudo 调用，得到 $sudo_calls"
timeout_calls=$(grep -c $'^timeout\t' "$log_file")
[[ "$timeout_calls" -eq 3 ]] || fail "期望三次硬超时调用，得到 $timeout_calls"

apt_lines=$(grep $'^apt-get\t' "$log_file")
while IFS= read -r line; do
  assert_contains "$line" 'Acquire::http::Timeout=20' 'apt 调用缺少 HTTP 超时'
  assert_contains "$line" 'Acquire::https::Timeout=20' 'apt 调用缺少 HTTPS 超时'
  assert_contains "$line" 'Acquire::Retries=2' 'apt 调用缺少有限重试次数'
done <<<"$apt_lines"
timeout_lines=$(grep $'^timeout\t' "$log_file")
while IFS= read -r line; do
  assert_contains "$line" '--signal=TERM' '硬超时缺少 TERM 信号'
  assert_contains "$line" '--kill-after=15s' '硬超时缺少强制终止边界'
  assert_contains "$line" '5m' '硬超时不是五分钟'
done <<<"$timeout_lines"
[[ -z "$(find "$test_tmp" -mindepth 1 -print -quit)" ]] || fail '备用源临时目录未清理'

# 备用 update 失败时，函数必须失败，并同时留下原始与备用错误文本。
export ECS_FAKE_APT_MODE=fallback-update-fail
: >"$log_file"
rm -f -- "$state_file"
failure_status=0
if failure_output=$(ecs_install_tools fio 2>&1); then
  fail '备用 update 失败却返回成功'
else
  failure_status=$?
fi
[[ "$failure_status" -eq 43 ]] || fail "备用 update 返回码为 $failure_status，期望 43"
assert_contains "$failure_output" 'fake apt: original Azure update failed' '备用 update 失败时缺少原始错误'
assert_contains "$failure_output" 'fake apt: fallback update failed' '备用 update 失败时缺少备用错误'
assert_contains "$failure_output" '原始与备用错误均已保留' '备用 update 失败时缺少诊断摘要'

# 备用 install 失败同样不能被吞掉，且保留首次 update 与备用 install 的诊断。
export ECS_FAKE_APT_MODE=fallback-install-fail
: >"$log_file"
rm -f -- "$state_file"
failure_status=0
if failure_output=$(ecs_install_tools fio 2>&1); then
  fail '备用 install 失败却返回成功'
else
  failure_status=$?
fi
[[ "$failure_status" -eq 47 ]] || fail "备用 install 返回码为 $failure_status，期望 47"
assert_contains "$failure_output" 'fake apt: original Azure update failed' '备用 install 失败时缺少原始错误'
assert_contains "$failure_output" 'fake apt: fallback install failed' '备用 install 失败时缺少备用错误'
assert_contains "$failure_output" '原始与备用错误均已保留' '备用 install 失败时缺少诊断摘要'
[[ -z "$(find "$test_tmp" -mindepth 1 -print -quit)" ]] || fail '失败路径未清理备用源临时目录'

echo 'integration apt install regression tests passed'
