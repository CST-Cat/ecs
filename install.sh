#!/bin/sh
set -eu

program="ecs"
repository="${ECS_REPOSITORY:-CST-Cat/ecs}"
version="${ECS_VERSION:-latest}"
release_base="${ECS_RELEASE_BASE:-}"
install_dir="${ECS_INSTALL_DIR:-}"
local_binary=""

usage() {
  printf '%s\n' \
    "ecs installer — downloads one release asset and verifies SHA-256" \
    "" \
    "ecs only supports Linux and FreeBSD." \
    "  Linux:   amd64, arm64, armv7, 386, s390x, riscv64, ppc64le" \
    "  FreeBSD: amd64, arm64" \
    "" \
    "Usage: ./install.sh [--from /path/to/ecs] [--install-dir DIR] [--version VERSION]" \
    "" \
    "Environment:" \
    "  ECS_REPOSITORY   GitHub owner/repo override (default: CST-Cat/ecs)." \
    "  ECS_RELEASE_BASE Custom release directory URL; overrides ECS_REPOSITORY." \
    "  ECS_INSTALL_DIR  Destination directory." \
    "  ECS_VERSION      Release tag, or latest (default)." \
    "" \
    "This installer only installs the ecs release binary; it never changes a" \
    "system package database. Benchmark tools are staged as verified frozen" \
    "architecture-matched assets by run.sh when a test run selects them."
}

die() {
  printf '%s\n' "$2" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --from)
      [ "$#" -ge 2 ] || { printf '%s\n' "missing value for --from" >&2; exit 1; }
      local_binary=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || { printf '%s\n' "missing value for --install-dir" >&2; exit 1; }
      install_dir=$2
      shift 2
      ;;
    --version)
      [ "$#" -ge 2 ] || { printf '%s\n' "missing value for --version" >&2; exit 1; }
      version=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown option: %s\n' "$1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [ -z "$install_dir" ]; then
  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    install_dir=/usr/local/bin
  else
    install_dir="${HOME}/.local/bin"
  fi
fi

install_binary() {
  source_file=$1
  mkdir -p "$install_dir"
  if [ ! -w "$install_dir" ]; then
    printf 'destination is not writable: %s\n' "$install_dir" >&2
    printf '%s\n' "Choose a writable directory with --install-dir; this script does not invoke privileged commands." >&2
    exit 1
  fi
  destination="${install_dir}/${program}"
  temp_destination=""
  temp_destination=$(mktemp "${install_dir}/.${program}.install.XXXXXX") || exit 1
  if ! cp "$source_file" "$temp_destination"; then
    rm -f "$temp_destination"
    printf 'could not build install candidate: %s\n' "$temp_destination" >&2
    exit 1
  fi
  if ! chmod 0755 "$temp_destination"; then
    rm -f "$temp_destination"
    printf 'could not set install candidate mode: %s\n' "$temp_destination" >&2
    exit 1
  fi
  if ! candidate_version=$("$temp_destination" version); then
    rm -f "$temp_destination"
    printf 'install candidate failed version validation: %s\n' "$temp_destination" >&2
    exit 1
  fi
  if ! mv "$temp_destination" "$destination"; then
    rm -f "$temp_destination"
    printf 'could not publish install candidate: %s\n' "$destination" >&2
    exit 1
  fi
  printf 'installed %s\n' "$destination"
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *) printf 'note: add %s to PATH\n' "$install_dir" ;;
  esac
  printf '%s\n' "$candidate_version"
}

if [ -n "$local_binary" ]; then
  [ -f "$local_binary" ] || { printf 'binary not found: %s\n' "$local_binary" >&2; exit 1; }
  install_binary "$local_binary"
  exit 0
fi

OS_NAME=$(uname -s)
machine=$(uname -m)
case "$OS_NAME" in
  Linux) os_name=linux; OS=linux ;;
  FreeBSD) os_name=freebsd; OS=freebsd ;;
  *)
    printf 'ecs only supports Linux and FreeBSD; detected: %s\n' "$OS_NAME" >&2
    exit 1
    ;;
esac
case "$machine" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l|armv7) arch=armv7 ;;
  i386|i686|x86) arch=386 ;;
  s390x) arch=s390x ;;
  riscv64) arch=riscv64 ;;
  ppc64le) arch=ppc64le ;;
  *) printf 'unsupported architecture: %s\n' "$machine" >&2; exit 1 ;;
esac

# The tools lock carries only freebsd_amd64 and freebsd_arm64. Accepting any
# other FreeBSD architecture here would name an asset that can never exist, so
# the installer refuses before it downloads rather than reporting a 404.
if [ "$os_name" = "freebsd" ]; then
  case "$arch" in
    amd64|arm64) ;;
    *)
      printf 'ecs supports only amd64 and arm64 on FreeBSD; detected: %s\n' "$machine" >&2
      exit 1
      ;;
  esac
fi

asset="${program}_${os_name}_${arch}.tar.gz"

if [ "$version" != "latest" ]; then
  case "$version" in
    *[!A-Za-z0-9._+-]*) printf '%s\n' "invalid release version" >&2; exit 1 ;;
  esac
fi

if [ -z "$release_base" ]; then
  owner=${repository%%/*}
  repo=${repository#*/}
  [ "$owner" != "$repository" ] || { printf '%s\n' "ECS_REPOSITORY must use owner/repo form" >&2; exit 1; }
  case "$owner" in
    ""|*[!A-Za-z0-9._-]*) printf '%s\n' "invalid ECS_REPOSITORY owner" >&2; exit 1 ;;
  esac
  case "$repo" in
    ""|*/*|*[!A-Za-z0-9._-]*) printf '%s\n' "ECS_REPOSITORY must use safe owner/repo form" >&2; exit 1 ;;
  esac
  if [ "$version" = "latest" ]; then
    release_base="https://github.com/${repository}/releases/latest/download"
  else
    release_base="https://github.com/${repository}/releases/download/${version}"
  fi
fi
release_base=${release_base%/}
case "$release_base" in
  https://*) ;;
  *) printf 'remote release URL must use https://: %s\n' "$release_base" >&2; exit 1 ;;
esac

WORK_ROOT=/tmp
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /*) WORK_ROOT=$TMPDIR ;;
    *) printf '%s\n' "TMPDIR must be an absolute path" >&2; exit 1 ;;
  esac
fi
[ -d "$WORK_ROOT" ] || { printf 'temporary directory does not exist: %s\n' "$WORK_ROOT" >&2; exit 1; }
work_dir=$(mktemp -d "$WORK_ROOT/ecs-install.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if ! rm -rf "$work_dir"; then
    printf 'failed to remove the temporary directory: %s\n' "$work_dir" >&2
    [ "$status" -eq 0 ] && status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

fetch() {
  fetch_max_time=${3:-300}
  case "$1" in
    https://*) ;;
    *) die "远程下载地址必须使用 HTTPS：$1" "remote download URL must use HTTPS: $1" ;;
  esac
  if command -v curl >/dev/null 2>&1; then
    # max-time applies per transfer; retry-max-time bounds the retry window.
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --retry-max-time "$fetch_max_time" \
      --connect-timeout 10 --speed-limit 1024 --speed-time 30 --max-time "$fetch_max_time" \
      "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    command -v timeout >/dev/null 2>&1 ||
      die "wget 路径需要 timeout 来限制总下载时间" "the wget path requires timeout to bound total download time"
    timeout "$fetch_max_time" wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  elif [ "${OS:-}" = freebsd ] && [ -x /usr/bin/fetch ]; then
    # FreeBSD base-system /usr/bin/fetch, the last resort after curl and wget.
    #
    # The path is absolute on purpose. This wrapper function is itself named
    # fetch, and `command -v fetch` inside its own body resolves to the shell
    # function rather than to the executable, so PATH lookup cannot be used to
    # tell them apart. `timeout` is an external program as well, so it could
    # not run a `command fetch ...` word anyway. FreeBSD owns /usr/bin/fetch
    # exactly like it owns /sbin/ping and /usr/sbin/traceroute, so naming the
    # base-system path is both unambiguous and consistent with the probes.
    command -v timeout >/dev/null 2>&1 ||
      die "fetch 路径需要 timeout 来限制总下载时间" "the fetch path requires timeout to bound total download time"
    timeout "$fetch_max_time" /usr/bin/fetch -q -o "$2" "$1"
  else
    die "需要 curl、wget 或 FreeBSD fetch" "curl, wget, or FreeBSD fetch is required"
  fi
}

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  elif command -v sha256 >/dev/null 2>&1; then
    # FreeBSD base-system /sbin/sha256. -q prints the digest alone, so the
    # output already matches the "one lowercase hex line" contract above.
    sha256 -q "$1" | tr '[:upper:]' '[:lower:]'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}' | tr '[:upper:]' '[:lower:]'
  else
    return 1
  fi
}

fetch "${release_base}/${asset}" "${work_dir}/${asset}"
fetch "${release_base}/checksums.txt" "${work_dir}/checksums.txt"

expected_hash=$(awk -v file="$asset" '$2 == file {print $1; exit}' "${work_dir}/checksums.txt" | tr '[:upper:]' '[:lower:]')
[ -n "$expected_hash" ] || { printf 'checksum entry missing for %s\n' "$asset" >&2; exit 1; }
if ! actual_hash=$(file_sha256 "${work_dir}/${asset}"); then
  printf '%s\n' "sha256sum, shasum, sha256, or openssl is required to verify the release" >&2
  exit 1
fi
[ "$actual_hash" = "$expected_hash" ] || { printf '%s\n' "SHA-256 verification failed" >&2; exit 1; }
printf '%s\n' "SHA-256 verified"

tar -xzf "${work_dir}/${asset}" -C "$work_dir" "$program"
[ -f "${work_dir}/${program}" ] && [ ! -L "${work_dir}/${program}" ] || { printf '%s\n' "release archive does not contain a regular ecs binary" >&2; exit 1; }
install_binary "${work_dir}/${program}"
