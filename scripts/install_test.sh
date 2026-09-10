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
  local destination=$1 version=$2 exit_status=${3:-0} mode=${4:-0644}
  cat >"$destination" <<EOF
#!/bin/sh
set -eu
printf '%s\\t%s\\n' "\$0" "\$*" >>"\$ECS_INSTALL_TEST_INVOCATIONS"
[ "\$#" -eq 1 ] && [ "\$1" = version ] || exit 64
printf '%s\\n' '$version'
exit $exit_status
EOF
  chmod "$mode" "$destination"
}

test_path="$forbidden_bin:$PATH"
export ECS_INSTALL_TEST_INVOCATIONS="$fixture_log"
export ECS_INSTALL_TEST_FORBIDDEN_LOG="$forbidden_log"

# 新安装必须逐字复制本地 fixture、在临时 candidate 路径验证 `version`，并以 0755 发布。
fixture_one="$fixture_bin/ecs-one"
write_fixture "$fixture_one" fixture-version-one
[[ "$(stat -c '%a' "$fixture_one")" == 644 ]] || fail "local fixture source is not the established 0644 mode"
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
[[ "${invocations[0]}" == "$install_dir"/.ecs.install.*$'\tversion' ]] ||
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
[[ "${invocations[1]}" == "$install_dir"/.ecs.install.*$'\tversion' ]] ||
  fail "replacement validation changed: ${invocations[1]}"
[[ ! -e "$forbidden_log" ]] || fail "replacement install called a forbidden command: $(<"$forbidden_log")"

# A regular 0644 source is copied and chmodded before version validation. A
# failed candidate execution must preserve the previous binary.
old_binary="$test_root/old-binary"
cp "$installed" "$old_binary"
fixture_bad_version="$fixture_bin/ecs-bad-version"
write_fixture "$fixture_bad_version" fixture-version-bad 73
set +e
ECS_INSTALL_DIR="$install_dir" PATH="$test_path" \
  sh "$repo_root/install.sh" --from "$fixture_bad_version" \
  >"$test_root/install-bad-version.stdout" 2>"$test_root/install-bad-version.stderr"
version_status=$?
set -e
[[ "$version_status" -eq 1 ]] || fail "failed version install returned $version_status instead of 1"
cmp -s "$old_binary" "$installed" || fail "version failure replaced the old binary"
mapfile -t invocations <"$fixture_log"
[[ "${#invocations[@]}" -eq 3 ]] || fail "failed version install did not invoke ecs exactly once"
[[ "${invocations[2]}" == "$install_dir"/.ecs.install.*$'\tversion' ]] ||
  fail "failed version validation did not execute the candidate: ${invocations[2]}"
if find "$install_dir" -maxdepth 1 -name '.ecs.install.*' -print -quit | grep -q .; then
  fail "version failure left an atomic-install temporary file"
fi
[[ ! -e "$forbidden_log" ]] || fail "failure-path install called a forbidden command: $(<"$forbidden_log")"

# Build an offline release fixture for downloader selection, HTTPS validation,
# checksum failure preservation, and successful remote installation.
case "$(uname -m)" in
  x86_64|amd64) fixture_arch=amd64 ;;
  aarch64|arm64) fixture_arch=arm64 ;;
  armv7l|armv7) fixture_arch=armv7 ;;
  i386|i686|x86) fixture_arch=386 ;;
  s390x) fixture_arch=s390x ;;
  riscv64) fixture_arch=riscv64 ;;
  ppc64le) fixture_arch=ppc64le ;;
  *) fail "unsupported test architecture: $(uname -m)" ;;
esac
remote_asset="ecs_linux_${fixture_arch}.tar.gz"
remote_release="$test_root/release"
mkdir -p "$remote_release/archive"
cp "$fixture_one" "$remote_release/archive/ecs"
tar -czf "$remote_release/$remote_asset" -C "$remote_release/archive" ecs
remote_digest=$(sha256sum "$remote_release/$remote_asset" | awk '{print $1}')
printf '%s  %s\n' "$remote_digest" "$remote_asset" >"$remote_release/checksums.txt"

make_command_path() {
  local path=$1 command_name target
  mkdir -p "$path"
  for command_name in sh uname tr mkdir mktemp cp chmod mv id awk sha256sum tar gzip rm timeout; do
    target=$(command -v "$command_name") || fail "required command is missing: $command_name"
    ln -s "$target" "$path/$command_name"
  done
}

write_downloader() {
  local path=$1 command_name=$2
  cat >"$path/$command_name" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${ECS_INSTALL_TEST_ARG_LOG:-}" ]; then
  printf '%s\0' "$@" >>"$ECS_INSTALL_TEST_ARG_LOG"
fi
url=""
destination=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|-O)
      shift
      [ "$#" -gt 0 ] || exit 64
      destination=$1
      ;;
    http://*|https://*) url=$1 ;;
  esac
  shift
done
[ -n "$url" ] && [ -n "$destination" ] || exit 65
printf '%s\n' "$url" >>"$ECS_INSTALL_TEST_DOWNLOAD_LOG"
case "$url" in
  "$ECS_INSTALL_TEST_RELEASE_BASE/$ECS_INSTALL_TEST_ASSET") source_path="$ECS_INSTALL_TEST_RELEASE_ROOT/$ECS_INSTALL_TEST_ASSET" ;;
  "$ECS_INSTALL_TEST_RELEASE_BASE/checksums.txt") source_path="$ECS_INSTALL_TEST_RELEASE_ROOT/checksums.txt" ;;
  *) : >"$ECS_INSTALL_TEST_UNEXPECTED_NETWORK"; exit 90 ;;
esac
cp "$source_path" "$destination"
EOF
  chmod 0755 "$path/$command_name"
}

curl_path="$test_root/curl-only-path"
make_command_path "$curl_path"
write_downloader "$curl_path" curl
wget_path="$test_root/wget-only-path"
make_command_path "$wget_path"
write_downloader "$wget_path" wget

http_base="http://fixture.invalid/releases"
http_log="$test_root/http-curl-download.log"
http_unexpected="$test_root/http-curl-unexpected"
set +e
ECS_INSTALL_DIR="$test_root/http-curl-install" ECS_RELEASE_BASE="$http_base" \
  ECS_INSTALL_TEST_DOWNLOAD_LOG="$http_log" ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$http_unexpected" \
  ECS_INSTALL_TEST_RELEASE_BASE="$http_base" ECS_INSTALL_TEST_RELEASE_ROOT="$remote_release" \
  ECS_INSTALL_TEST_ASSET="$remote_asset" PATH="$curl_path" \
  sh "$repo_root/install.sh" \
  >"$test_root/http-curl.stdout" 2>"$test_root/http-curl.stderr"
http_curl_status=$?
set -e
[[ "$http_curl_status" -eq 1 ]] || fail "curl-only HTTP URL returned $http_curl_status instead of 1"
[[ ! -e "$http_log" ]] || fail "curl was invoked for a rejected HTTP URL"
[[ ! -e "$http_unexpected" ]] || fail "curl-only HTTP case reached the downloader"

http_wget_log="$test_root/http-wget-download.log"
http_wget_unexpected="$test_root/http-wget-unexpected"
set +e
ECS_INSTALL_DIR="$test_root/http-wget-install" ECS_RELEASE_BASE="$http_base" \
  ECS_INSTALL_TEST_DOWNLOAD_LOG="$http_wget_log" ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$http_wget_unexpected" \
  ECS_INSTALL_TEST_RELEASE_BASE="$http_base" ECS_INSTALL_TEST_RELEASE_ROOT="$remote_release" \
  ECS_INSTALL_TEST_ASSET="$remote_asset" PATH="$wget_path" \
  sh "$repo_root/install.sh" \
  >"$test_root/http-wget.stdout" 2>"$test_root/http-wget.stderr"
http_wget_status=$?
set -e
[[ "$http_wget_status" -eq 1 ]] || fail "wget-only HTTP URL returned $http_wget_status instead of 1"
[[ ! -e "$http_wget_log" ]] || fail "wget was invoked for a rejected HTTP URL"
[[ ! -e "$http_wget_unexpected" ]] || fail "wget-only HTTP case reached the downloader"

remote_install_dir="$test_root/remote-install"
remote_log="$test_root/remote-download.log"
remote_unexpected="$test_root/remote-unexpected"
remote_args="$test_root/remote-download.args"
if ! ECS_INSTALL_DIR="$remote_install_dir" ECS_RELEASE_BASE="https://fixture.invalid/releases" \
    ECS_INSTALL_TEST_DOWNLOAD_LOG="$remote_log" ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$remote_unexpected" \
    ECS_INSTALL_TEST_ARG_LOG="$remote_args" \
    ECS_INSTALL_TEST_RELEASE_BASE="https://fixture.invalid/releases" ECS_INSTALL_TEST_RELEASE_ROOT="$remote_release" \
    ECS_INSTALL_TEST_ASSET="$remote_asset" PATH="$curl_path" \
    sh "$repo_root/install.sh" \
    >"$test_root/remote.stdout" 2>"$test_root/remote.stderr"; then
  fail "HTTPS fixture install failed: $(<"$test_root/remote.stderr")"
fi
cmp -s "$fixture_one" "$remote_install_dir/ecs" || fail "HTTPS fixture changed the binary contents"
[[ "$(stat -c '%a' "$remote_install_dir/ecs")" == 755 ]] || fail "HTTPS fixture install mode is not 0755"
[[ "$(wc -l <"$remote_log")" -eq 2 ]] || fail "HTTPS fixture did not make exactly two downloads"
[[ ! -e "$remote_unexpected" ]] || fail "HTTPS fixture reached an unexpected network path"
remote_args_text=$(tr '\0' '\n' <"$remote_args")
grep -F -x -- '--connect-timeout' <<<"$remote_args_text" >/dev/null || fail "install curl omitted connect timeout"
grep -F -x -- '--speed-limit' <<<"$remote_args_text" >/dev/null || fail "install curl omitted speed limit"
grep -F -x -- '--speed-time' <<<"$remote_args_text" >/dev/null || fail "install curl omitted speed time"
awk '$0 == "--max-time" { getline; if ($0 == "300") found=1 } END { exit !found }' <<<"$remote_args_text" || fail "install curl omitted 300-second max-time"
awk '$0 == "--retry-max-time" { getline; if ($0 == "300") found=1 } END { exit !found }' <<<"$remote_args_text" || fail "install curl omitted 300-second retry window"

# install.sh derives the release asset name from uname -s, so the FreeBSD entry
# is exercised with a stub uname ahead of the real one. This harness runs on
# Linux; the FreeBSD-specific tool filtering lives in the Go plan and is not
# install.sh's concern.
make_freebsd_install_path() {
  local path=$1 machine=$2
  make_command_path "$path"
  # uname must be replaced, not symlinked: writing the stub through a symlink to
  # the real uname would try to overwrite the system binary.
  rm -f "$path/uname"
  cat >"$path/uname" <<EOF
#!/bin/sh
case "\$1" in
  -s) printf '%s\n' FreeBSD ;;
  -m) printf '%s\n' $machine ;;
  *) printf '%s\n' FreeBSD ;;
esac
EOF
  chmod 0755 "$path/uname"
  write_downloader "$path" curl
}

freebsd_release="$test_root/freebsd-release"
freebsd_asset="ecs_freebsd_${fixture_arch}.tar.gz"
mkdir -p "$freebsd_release/archive"
cp "$fixture_one" "$freebsd_release/archive/ecs"
tar -czf "$freebsd_release/$freebsd_asset" -C "$freebsd_release/archive" ecs
printf '%s  %s\n' "$(sha256sum "$freebsd_release/$freebsd_asset" | awk '{print $1}')" "$freebsd_asset" >"$freebsd_release/checksums.txt"

freebsd_path="$test_root/freebsd-path"
make_freebsd_install_path "$freebsd_path" "$fixture_arch"
freebsd_install_dir="$test_root/freebsd-install"
freebsd_log="$test_root/freebsd-download.log"
freebsd_unexpected="$test_root/freebsd-unexpected"
if ! ECS_INSTALL_DIR="$freebsd_install_dir" ECS_RELEASE_BASE="https://fixture.invalid/releases" \
    ECS_INSTALL_TEST_DOWNLOAD_LOG="$freebsd_log" ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$freebsd_unexpected" \
    ECS_INSTALL_TEST_RELEASE_BASE="https://fixture.invalid/releases" \
    ECS_INSTALL_TEST_RELEASE_ROOT="$freebsd_release" \
    ECS_INSTALL_TEST_ASSET="$freebsd_asset" PATH="$freebsd_path" \
    sh "$repo_root/install.sh" \
    >"$test_root/freebsd.stdout" 2>"$test_root/freebsd.stderr"; then
  fail "FreeBSD fixture install failed: $(<"$test_root/freebsd.stderr")"
fi
cmp -s "$fixture_one" "$freebsd_install_dir/ecs" || fail "FreeBSD fixture changed the binary contents"
grep -F -x "https://fixture.invalid/releases/$freebsd_asset" "$freebsd_log" >/dev/null ||
  fail "FreeBSD fixture did not request $freebsd_asset"
[[ ! -e "$freebsd_unexpected" ]] || fail "FreeBSD fixture reached an unexpected network path"

# The tools lock carries only freebsd_amd64 and freebsd_arm64, so the installer
# must refuse any other FreeBSD architecture before it downloads rather than
# naming an asset that can never exist.
freebsd_bad_path="$test_root/freebsd-bad-path"
make_freebsd_install_path "$freebsd_bad_path" i386
set +e
ECS_INSTALL_DIR="$test_root/freebsd-bad-install" ECS_RELEASE_BASE="https://fixture.invalid/releases" \
  ECS_INSTALL_TEST_DOWNLOAD_LOG="$test_root/freebsd-bad-download.log" \
  ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$test_root/freebsd-bad-unexpected" \
  ECS_INSTALL_TEST_RELEASE_BASE="https://fixture.invalid/releases" \
  ECS_INSTALL_TEST_RELEASE_ROOT="$freebsd_release" ECS_INSTALL_TEST_ASSET="$freebsd_asset" \
  PATH="$freebsd_bad_path" sh "$repo_root/install.sh" \
  >"$test_root/freebsd-bad.stdout" 2>"$test_root/freebsd-bad.stderr"
freebsd_bad_status=$?
set -e
[[ "$freebsd_bad_status" -eq 1 ]] || fail "FreeBSD i386 returned $freebsd_bad_status instead of 1"
grep -F 'only amd64 and arm64 on FreeBSD' "$test_root/freebsd-bad.stderr" >/dev/null ||
  fail "FreeBSD i386 was not rejected with the architecture message"
[[ ! -e "$test_root/freebsd-bad-download.log" ]] || fail "FreeBSD i386 downloaded before rejecting"

# A bad archive checksum must preserve an existing installation and clean the
# work directory without touching the destination candidate path.
bad_release="$test_root/bad-release"
mkdir -p "$bad_release"
cp "$remote_release/$remote_asset" "$bad_release/$remote_asset"
printf '%064d  %s\n' 0 "$remote_asset" >"$bad_release/checksums.txt"
bad_log="$test_root/bad-download.log"
bad_unexpected="$test_root/bad-unexpected"
set +e
ECS_INSTALL_DIR="$install_dir" ECS_RELEASE_BASE="https://fixture.invalid/bad" \
  ECS_INSTALL_TEST_DOWNLOAD_LOG="$bad_log" ECS_INSTALL_TEST_UNEXPECTED_NETWORK="$bad_unexpected" \
  ECS_INSTALL_TEST_RELEASE_BASE="https://fixture.invalid/bad" ECS_INSTALL_TEST_RELEASE_ROOT="$bad_release" \
  ECS_INSTALL_TEST_ASSET="$remote_asset" PATH="$curl_path" \
  sh "$repo_root/install.sh" \
  >"$test_root/bad.stdout" 2>"$test_root/bad.stderr"
bad_status=$?
set -e
[[ "$bad_status" -eq 1 ]] || fail "checksum failure returned $bad_status instead of 1"
cmp -s "$old_binary" "$installed" || fail "checksum failure replaced the old binary"
if find "$install_dir" -maxdepth 1 -name '.ecs.install.*' -print -quit | grep -q .; then
  fail "checksum failure left an atomic-install temporary file"
fi
[[ ! -e "$bad_unexpected" ]] || fail "checksum fixture reached an unexpected network path"

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
