#!/bin/sh
set -eu

program="ecs"
repository="${ECS_REPOSITORY:-}"
version="${ECS_VERSION:-latest}"
release_base="${ECS_RELEASE_BASE:-}"
install_dir="${ECS_INSTALL_DIR:-}"
local_binary=""
with_benchmarks=0

usage() {
  printf '%s\n' \
    "ecs installer — downloads one release asset and verifies SHA-256" \
    "" \
    "ecs only supports Linux (amd64, arm64, armv7, 386, s390x, riscv64, ppc64le)." \
    "" \
    "Usage: ./install.sh [--from /path/to/ecs] [--install-dir DIR] [--version VERSION] [--with-benchmarks]" \
    "" \
    "Environment:" \
    "  ECS_REPOSITORY   GitHub owner/repo. Required for remote installation." \
    "  ECS_RELEASE_BASE Custom release directory URL; overrides ECS_REPOSITORY." \
    "  ECS_INSTALL_DIR  Destination directory." \
    "  ECS_VERSION      Release tag, or latest (default)." \
    "" \
    "By default no package manager is changed. --with-benchmarks explicitly installs" \
    "the benchmark tools (sysbench, fio, iperf3, mbw, ioping, smartmontools)" \
    "through the detected system package manager." \
    "Route modules use NextTrace; run.sh prepares a verified temporary binary when needed."
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
    --with-benchmarks)
      with_benchmarks=1
      shift
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
    printf '%s\n' "Choose a writable directory with --install-dir; this script does not invoke sudo." >&2
    exit 1
  fi
  destination="${install_dir}/${program}"
  temp_destination="${install_dir}/.${program}.install.$$"
  cp "$source_file" "$temp_destination"
  chmod 0755 "$temp_destination"
  mv "$temp_destination" "$destination"
  printf 'installed %s\n' "$destination"
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *) printf 'note: add %s to PATH\n' "$install_dir" ;;
  esac
  "$destination" version
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    printf '%s\n' "benchmark dependencies require root; re-run as root or install sysbench fio iperf3 mbw ioping smartmontools manually" >&2
    exit 1
  fi
}

install_benchmark_tools() {
  [ "$with_benchmarks" -eq 1 ] || return 0
  printf '%s\n' "installing standard benchmark tools: sysbench fio iperf3 mbw ioping smartmontools"
  if command -v apt-get >/dev/null 2>&1; then
    as_root env DEBIAN_FRONTEND=noninteractive apt-get update
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y sysbench fio iperf3 mbw ioping smartmontools
  elif command -v dnf >/dev/null 2>&1; then
    as_root dnf install -y sysbench fio iperf3 mbw ioping smartmontools
  elif command -v yum >/dev/null 2>&1; then
    as_root yum install -y sysbench fio iperf3 mbw ioping smartmontools
  elif command -v apk >/dev/null 2>&1; then
    as_root apk add sysbench fio iperf3 mbw ioping smartmontools
  elif command -v pacman >/dev/null 2>&1; then
    as_root pacman -Sy --noconfirm sysbench fio iperf3 mbw ioping smartmontools
  else
    printf '%s\n' "no supported package manager found; install sysbench fio iperf3 mbw ioping smartmontools manually" >&2
    exit 1
  fi
  printf '%s\n' "standard benchmark tools installed"
}

if [ -n "$local_binary" ]; then
  [ -f "$local_binary" ] || { printf 'binary not found: %s\n' "$local_binary" >&2; exit 1; }
  install_binary "$local_binary"
  install_benchmark_tools
  exit 0
fi

os_name=$(uname -s | tr '[:upper:]' '[:lower:]')
machine=$(uname -m | tr '[:upper:]' '[:lower:]')
if [ "$os_name" != "linux" ]; then
  printf 'ecs only supports Linux; detected: %s\n' "$os_name" >&2
  exit 1
fi
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

asset="${program}_${os_name}_${arch}.tar.gz"

if [ -z "$release_base" ]; then
  if [ -z "$repository" ]; then
    printf '%s\n' "ECS_REPOSITORY is required until this checkout is connected to its final GitHub repository." >&2
    printf '%s\n' "Example: ECS_REPOSITORY=owner/ecs ./install.sh" >&2
    exit 1
  fi
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
    case "$version" in
      *[!A-Za-z0-9._+-]*) printf '%s\n' "invalid release version" >&2; exit 1 ;;
    esac
    release_base="https://github.com/${repository}/releases/download/${version}"
  fi
fi
release_base=${release_base%/}

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/ecs-install.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

download() {
  source_url=$1
  destination_file=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$source_url" -o "$destination_file"
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only --tries=3 --timeout=20 -O "$destination_file" "$source_url"
  else
    printf '%s\n' "curl or wget is required to download a release" >&2
    exit 1
  fi
}

download "${release_base}/${asset}" "${work_dir}/${asset}"
download "${release_base}/checksums.txt" "${work_dir}/checksums.txt"

expected_hash=$(awk -v file="$asset" '$2 == file {print $1; exit}' "${work_dir}/checksums.txt")
[ -n "$expected_hash" ] || { printf 'checksum entry missing for %s\n' "$asset" >&2; exit 1; }
case "$expected_hash" in
  *[!A-Fa-f0-9]*|"") printf '%s\n' "invalid SHA-256 entry" >&2; exit 1 ;;
esac
[ "${#expected_hash}" -eq 64 ] || { printf '%s\n' "invalid SHA-256 length" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual_hash=$(sha256sum "${work_dir}/${asset}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual_hash=$(shasum -a 256 "${work_dir}/${asset}" | awk '{print $1}')
else
  printf '%s\n' "sha256sum or shasum is required to verify the release" >&2
  exit 1
fi
[ "$actual_hash" = "$expected_hash" ] || { printf '%s\n' "SHA-256 verification failed" >&2; exit 1; }
printf '%s\n' "SHA-256 verified"

tar -xzf "${work_dir}/${asset}" -C "$work_dir" "$program"
[ -f "${work_dir}/${program}" ] && [ ! -L "${work_dir}/${program}" ] || { printf '%s\n' "release archive does not contain a regular ecs binary" >&2; exit 1; }
install_binary "${work_dir}/${program}"
install_benchmark_tools
