#!/usr/bin/env bash
set -euo pipefail

version="${1:-}"
if [[ -z "$version" ]]; then
  echo "usage: scripts/package.sh VERSION" >&2
  exit 1
fi
if [[ ! "$version" =~ ^[0-9A-Za-z._+-]+$ ]]; then
  echo "VERSION may only contain letters, digits, dot, underscore, plus, and hyphen" >&2
  exit 1
fi

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dist_dir="$repo_root/dist"
go_command="${GO:-go}"
commit="${COMMIT:-$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ldflags="-s -w -X ecs/internal/buildinfo.Version=${version} -X ecs/internal/buildinfo.Commit=${commit} -X ecs/internal/buildinfo.BuildDate=${build_date}"

mkdir -p "$dist_dir"
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -name 'ecs_*' -delete
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -name 'checksums.txt' -delete

# ecs 只面向 Linux VPS，发布目标随之收敛到 Linux 架构。
targets=(
  "linux amd64"
  "linux arm64"
  "linux arm 7"
  "linux 386"
  "linux s390x"
  "linux riscv64"
  "linux ppc64le"
)

for target in "${targets[@]}"; do
  read -r goos goarch goarm <<<"$target"
  suffix="${goos}_${goarch}"
  if [[ -n "${goarm:-}" ]]; then
    suffix="${suffix}v${goarm}"
  fi
  stage=$(mktemp -d "${TMPDIR:-/tmp}/ecs-package.XXXXXX")
  binary="$stage/ecs"
  echo "building $suffix"
  (
    cd "$repo_root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="${goarm:-}" \
      "$go_command" build -trimpath -ldflags "$ldflags" -o "$binary" ./cmd/ecs
  )
  cp "$repo_root/LICENSE" "$repo_root/NOTICE" "$repo_root/README.md" "$repo_root/SECURITY.md" "$repo_root/THIRD_PARTY.md" "$stage/"
  tar -C "$stage" -czf "$dist_dir/ecs_${suffix}.tar.gz" ecs LICENSE NOTICE README.md SECURITY.md THIRD_PARTY.md
  rm -rf "$stage"
done

(
  cd "$dist_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ecs_*.tar.gz > checksums.txt
  else
    shasum -a 256 ecs_*.tar.gz > checksums.txt
  fi
)

echo "release assets written to $dist_dir"
