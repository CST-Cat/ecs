#!/bin/sh
# Verify the stable contract of a cached FreeBSD consumer VM.
#
# 验证缓存 FreeBSD consumer VM 的稳定契约。缓存命中时 prepare 可能被跳过，
# 所以每次真正运行仍必须重新确认 release、架构和 CPU；需要 package 的 gate
# 另外逐项确认当前仓库锁中的精确版本已经安装。
set -eu

target=${1:?usage: freebsd_vm_contract.sh freebsd_amd64|freebsd_arm64 [locked-packages]}
package_mode=${2:-none}
expected_release=${FREEBSD_VM_RELEASE:-15.1}

case "$target" in
  freebsd_amd64)
    case "$(uname -m)" in
      amd64|x86_64) ;;
      *) echo "guest architecture mismatch: target=$target actual=$(uname -m)" >&2; exit 1 ;;
    esac
    package_arch=amd64
    ;;
  freebsd_arm64)
    case "$(uname -m)" in
      arm64|aarch64) ;;
      *) echo "guest architecture mismatch: target=$target actual=$(uname -m)" >&2; exit 1 ;;
    esac
    package_arch=aarch64
    ;;
  *) echo "unsupported FreeBSD target: $target" >&2; exit 1 ;;
esac

guest_ncpu=$(sysctl -n hw.ncpu)
[ "$guest_ncpu" = 4 ] || {
  printf 'guest CPU mismatch: expected=4 actual=%s\n' "$guest_ncpu" >&2
  exit 1
}

userland_version=$(freebsd-version -u)
kernel_version=$(freebsd-version -k)
case "$userland_version" in
  "$expected_release"-RELEASE*) ;;
  *)
    printf 'FreeBSD userland mismatch: expected=%s-RELEASE actual=%s\n' \
      "$expected_release" "$userland_version" >&2
    exit 1
    ;;
esac

if [ "$package_mode" = locked-packages ]; then
  workspace=${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required for locked packages}
  lock="$workspace/tools/freebsd-vm-deps.lock"
  [ -r "$lock" ] || { echo "missing package lock: $lock" >&2; exit 1; }
  installed=$(pkg query '%n|%v')
  package_count=0
  while IFS='|' read -r lock_target name version url sha; do
    case "$lock_target" in
      ''|'#'*) continue ;;
    esac
    [ "$lock_target" = "$package_arch" ] || continue
    printf '%s\n' "$installed" | grep -F -x "$name|$version" >/dev/null || {
      echo "locked package is not installed at the expected version: $name-$version" >&2
      exit 1
    }
    printf 'locked_package=%s|%s\n' "$name" "$version"
    package_count=$((package_count + 1))
  done < "$lock"
  [ "$package_count" -eq 7 ] || {
    echo "expected 7 locked packages for $package_arch, found $package_count" >&2
    exit 1
  }
  printf 'locked_packages=%s\n' "$package_count"
elif [ "$package_mode" != none ]; then
  echo "unsupported package verification mode: $package_mode" >&2
  exit 1
fi

printf 'target=%s\n' "$target"
printf 'guest_uname=%s\n' "$(uname -m)"
printf 'guest_ncpu=%s\n' "$guest_ncpu"
printf 'freebsd_userland=%s\n' "$userland_version"
printf 'freebsd_kernel=%s\n' "$kernel_version"
