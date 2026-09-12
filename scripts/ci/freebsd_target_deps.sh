#!/usr/bin/env bash
set -euo pipefail

# Fetch and install FreeBSD target dependency packages from the immutable
# project-controlled snapshot (ci-freebsd-deps-v1). Never use rolling
# pkg.freebsd.org /latest or Hashed paths as a long-term lock.

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

LOCK_FILE="$ECS_REPO_ROOT/tools/freebsd-target-deps.lock.json"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_target_deps.sh --target freebsd_amd64|freebsd_arm64
                                         --prefix DIR [--work-dir DIR]
       scripts/ci/freebsd_target_deps.sh --print-lock --target TARGET
USAGE
}

die() {
  echo "freebsd-target-deps: $*" >&2
  exit 1
}

target=""
prefix=""
work_dir=""
print_lock=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --prefix)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--prefix requires a value"
      prefix=$2
      shift 2
      ;;
    --work-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--work-dir requires a value"
      work_dir=$2
      shift 2
      ;;
    --print-lock)
      print_lock=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown option: $1"
      ;;
  esac
done

[[ -s "$LOCK_FILE" ]] || die "missing deps lock: $LOCK_FILE"
[[ "$(jq -er '.schema_version' "$LOCK_FILE")" == "ecs.freebsd-target-deps.lock/v1" ]] ||
  die "unsupported deps lock schema"
case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac

snapshot_id=$(jq -er '.snapshot.id' "$LOCK_FILE")
base_url=$(jq -er '.snapshot.base_url' "$LOCK_FILE")
[[ "$snapshot_id" == "ci-freebsd-deps-v1" ]] || die "unexpected snapshot id: $snapshot_id"
case "$base_url" in
  *latest* | *Hashed*) die "snapshot base_url must not be rolling: $base_url" ;;
  *ci-freebsd-deps-v1*) ;;
  *) die "snapshot base_url does not name the immutable snapshot: $base_url" ;;
esac

if [[ "$print_lock" -eq 1 ]]; then
  echo "snapshot_id=$snapshot_id"
  echo "base_url=$base_url"
  echo "freebsd_abi=$(jq -er '.freebsd_abi' "$LOCK_FILE")"
  jq -er --arg t "$target" '
    .packages[$t][] |
    "package=\(.name) version=\(.version) sha256=\(.sha256) asset=\(.asset)"
  ' "$LOCK_FILE"
  exit 0
fi

[[ -n "$prefix" ]] || {
  usage
  die "--prefix is required"
}
if [[ -z "$work_dir" ]]; then
  work_dir="$ECS_REPO_ROOT/.ci/target-deps-work"
fi

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"
command -v tar >/dev/null 2>&1 || die "tar is required"
command -v jq >/dev/null 2>&1 || die "jq is required"

mkdir -p "$work_dir" "$prefix"
echo "freebsd-target-deps: snapshot=$snapshot_id target=$target" >&2

mapfile -t packages < <(jq -cer --arg t "$target" '.packages[$t][] | @base64' "$LOCK_FILE")
[[ "${#packages[@]}" -gt 0 ]] || die "no packages locked for $target"

for encoded in "${packages[@]}"; do
  pkg_json=$(printf '%s' "$encoded" | base64 -d)
  name=$(jq -er '.name' <<<"$pkg_json")
  version=$(jq -er '.version' <<<"$pkg_json")
  asset=$(jq -er '.asset' <<<"$pkg_json")
  url=$(jq -er '.snapshot_url' <<<"$pkg_json")
  sha=$(jq -er '.sha256' <<<"$pkg_json")
  case "$url" in
    *latest* | *Hashed*)
      die "package $name uses a rolling URL: $url"
      ;;
  esac
  case "$url" in
    "$base_url"/*) ;;
    *) die "package $name URL is not under snapshot base: $url" ;;
  esac

  archive="$work_dir/$asset"
  if [[ -s "$archive" ]] && [[ "$(sha256sum "$archive" | awk '{print $1}')" == "$sha" ]]; then
    echo "freebsd-target-deps: reusing verified $asset" >&2
  else
    rm -f "$archive"
    echo "freebsd-target-deps: downloading $asset" >&2
    curl -fsSL --retry 4 --retry-delay 2 -o "$archive" "$url"
    actual=$(sha256sum "$archive" | awk '{print $1}')
    [[ "$actual" == "$sha" ]] ||
      die "SHA256 mismatch for $asset: expected=$sha actual=$actual"
  fi

  echo "freebsd-target-deps: extracting $name $version into $prefix" >&2
  # FreeBSD .pkg is a tar.zst (or legacy txz). Prefer bsdtar which handles both.
  if command -v bsdtar >/dev/null 2>&1; then
    bsdtar -x -f "$archive" -C "$prefix"
  else
    # Try zstd+tar; fall back to xz tar.
    if tar --use-compress-program=unzstd -xf "$archive" -C "$prefix" 2>/dev/null; then
      :
    elif tar -xJf "$archive" -C "$prefix" 2>/dev/null; then
      :
    else
      die "cannot extract FreeBSD package $asset; install bsdtar or zstd"
    fi
  fi
done

# Target prefix must expose luajit and ck to pkg-config for sysbench.
pkgconfig_candidates=(
  "$prefix/usr/local/libdata/pkgconfig"
  "$prefix/usr/local/lib/pkgconfig"
  "$prefix/usr/libdata/pkgconfig"
)
found_pc=0
for dir in "${pkgconfig_candidates[@]}"; do
  if [[ -d "$dir" ]] && ls "$dir"/*luajit* >/dev/null 2>&1 && ls "$dir"/*ck* >/dev/null 2>&1; then
    found_pc=1
    echo "freebsd-target-deps: pkg-config dir $dir" >&2
    break
  fi
done
[[ "$found_pc" -eq 1 ]] ||
  die "target prefix is missing luajit/ck pkg-config metadata under $prefix"

# Record provenance for later stages.
cat >"$prefix/deps-provenance.json" <<EOF
{
  "snapshot_id": "$snapshot_id",
  "target": "$target",
  "freebsd_abi": "$(jq -er '.freebsd_abi' "$LOCK_FILE")",
  "packages": $(jq -c --arg t "$target" '.packages[$t]' "$LOCK_FILE")
}
EOF

echo "freebsd-target-deps: $target dependencies ready at $prefix" >&2
