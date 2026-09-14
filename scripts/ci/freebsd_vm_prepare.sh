#!/bin/sh
# Prepare the small, locked tool set used by FreeBSD CI consumer VMs (v1).
#
# This runs before bash/jq are installed, so it intentionally uses only
# FreeBSD base tools. Package rows are consumed in dependency order from the
# repository lock; every downloaded byte is checked before pkg add.
set -eu

workspace=${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}
lock="$workspace/tools/freebsd-vm-deps.lock"
[ -r "$lock" ] || {
  echo "missing FreeBSD VM dependency lock: $lock" >&2
  exit 1
}

case "$(uname -m)" in
  amd64) package_arch=amd64 ;;
  arm64|aarch64) package_arch=aarch64 ;;
  *) echo "unsupported FreeBSD VM architecture: $(uname -m)" >&2; exit 1 ;;
esac

work="${TMPDIR:-/tmp}/ecs-freebsd-vm-deps"
mkdir -p "$work"
chmod 700 "$work"

package_files=
expected_packages=
count=0
while IFS='|' read -r target name version url expected_sha; do
  case "$target" in
    ''|'#'*) continue ;;
  esac
  [ "$target" = "$package_arch" ] || continue
  case "$name" in
    indexinfo|gettext-runtime|oniguruma|bash|jq|file|ca_root_nss) ;;
    *) echo "unexpected package in FreeBSD VM lock: $name" >&2; exit 1 ;;
  esac
  case "$expected_sha" in
    [0123456789abcdef][0123456789abcdef][0123456789abcdef][0123456789abcdef]* ) ;;
    *) echo "invalid SHA256 for FreeBSD VM package: $name" >&2; exit 1 ;;
  esac
  package_file="$work/$name-$version.pkg"
  echo "freebsd-vm-prepare: fetching $name-$version" >&2
  fetch -o "$package_file" "$url"
  actual_sha=$(sha256 -q "$package_file")
  [ "$actual_sha" = "$expected_sha" ] || {
    echo "SHA256 mismatch for $name-$version: expected $expected_sha, got $actual_sha" >&2
    exit 1
  }
  package_files="$package_files
$package_file"
  expected_packages="$expected_packages
$name|$version"
  count=$((count + 1))
done < "$lock"

[ "$count" -eq 7 ] || {
  echo "expected 7 locked packages for $package_arch, found $count" >&2
  exit 1
}

printf '%s\n' "$package_files" | sed '/^$/d' |
while IFS= read -r package_file; do
  # FreeBSD 15's `pkg add` has no `-y` option; ASSUME_ALWAYS_YES is the
  # non-interactive confirmation mechanism shared by the package commands.
  env ASSUME_ALWAYS_YES=yes pkg add "$package_file"
done

installed=$(pkg query '%n|%v')
printf '%s\n' "$expected_packages" | sed '/^$/d' |
while IFS='|' read -r name version; do
  printf '%s\n' "$installed" | grep -F -x "$name|$version" >/dev/null || {
    echo "locked package is not installed at the expected version: $name-$version" >&2
    exit 1
  }
done

echo "freebsd-vm-prepare: installed 7 locked packages for $package_arch" >&2
