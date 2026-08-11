#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
package_script="$repo_root/scripts/package.sh"
go_command="${GO:-go}"
stage_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-test.XXXXXX")
log_file="$stage_root/package.log"
package_tmp="$stage_root/package-tmp"
mkdir -p "$package_tmp"
trap 'rm -rf -- "$stage_root"' EXIT

architectures=(amd64 arm64 armv7 386 s390x riscv64 ppc64le)
tools=(sysbench zstd npb-ep npb-ft openssl stream fio iperf3 nexttrace-tiny ping)
corpus_sha256='8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0'
corpus_source_sha256='7d1dd71bfecda66a0ca30d863ed031809f67ecf12717a60fe72c1cc39e28434e'
corpus_file=${ECS_TEST_ZSTD_CORPUS:-}
if [[ -z "$corpus_file" ]]; then
  command -v curl >/dev/null 2>&1
  command -v unzip >/dev/null 2>&1
  corpus_zip="$stage_root/silesia.zip"
  corpus_source="$stage_root/silesia"
  corpus_file="$stage_root/ecs-silesia-v1.corpus"
  curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 \
    https://mattmahoney.net/dc/silesia.zip -o "$corpus_zip"
  [[ "$(sha256sum "$corpus_zip" | awk '{print $1}')" == "$corpus_source_sha256" ]]
  mkdir -p "$corpus_source"
  unzip -q "$corpus_zip" -d "$corpus_source"
  : >"$corpus_file"
  for member in dickens mozilla mr nci ooffice osdb reymont samba sao webster x-ray xml; do
    cat "$corpus_source/$member" >>"$corpus_file"
  done
fi
[[ -f "$corpus_file" && ! -L "$corpus_file" ]]
[[ "$(sha256sum "$corpus_file" | awk '{print $1}')" == "$corpus_sha256" ]]

write_fixture() {
  local path=$1
  printf '%s\n' '#!/bin/sh' 'printf "%s\\n" fixture' >"$path"
  chmod 0755 "$path"
}

write_manifest_fixture() {
  local arch=$1
  local path=$2
  sed "s/\"architecture\": \"amd64\"/\"architecture\": \"$arch\"/g" \
    "$repo_root/tools/manifest.example.json" >"$path"
}

for arch in "${architectures[@]}"; do
  stage_dir="$stage_root/linux_${arch}"
  mkdir -p "$stage_dir/bin" "$stage_dir/LICENSES" "$stage_dir/share/ecs/corpus"
  cp "$repo_root/LICENSE" "$stage_dir/LICENSES/project-license.txt"
  write_manifest_fixture "$arch" "$stage_dir/manifest.json"
  if ! ln "$corpus_file" "$stage_dir/share/ecs/corpus/ecs-silesia-v1.corpus" 2>/dev/null; then
    cp "$corpus_file" "$stage_dir/share/ecs/corpus/ecs-silesia-v1.corpus"
  fi
  for tool in "${tools[@]}"; do
    write_fixture "$stage_dir/bin/$tool"
  done
  # This is an executable sentinel; package_tools must copy only the ten
  # declared tools and must never include an Ookla/speedtest path.
  write_fixture "$stage_dir/bin/ookla"
done

if ! GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  cat "$log_file" >&2
  exit 1
fi

for arch in "${architectures[@]}"; do
  archive="$repo_root/dist/ecs-tools_linux_${arch}.tar.gz"
  [[ -s "$archive" ]] || {
    echo "missing or empty archive: $archive" >&2
    exit 1
  }
  listing=$(tar -tzf "$archive")
  for entry in \
    bin/ \
    bin/sysbench \
    bin/zstd \
    bin/npb-ep \
    bin/npb-ft \
    bin/openssl \
    bin/stream \
    bin/fio \
    bin/iperf3 \
    bin/nexttrace-tiny \
    bin/ping \
    share/ \
    share/ecs/ \
    share/ecs/corpus/ \
    share/ecs/corpus/ecs-silesia-v1.corpus \
    LICENSES/ \
    LICENSES/project-license.txt \
    LICENSE \
    NOTICE \
    manifest.json; do
    printf '%s\n' "$listing" | grep -F -x "$entry" >/dev/null || {
      echo "archive $archive is missing $entry" >&2
      exit 1
    }
  done
  if printf '%s\n' "$listing" | grep -E -i '(^|/)(ookla|speedtest)(/|$)' >/dev/null; then
    echo "archive $archive contains an Ookla path" >&2
    exit 1
  fi
  manifest=$(tar -xzOf "$archive" manifest.json)
  printf '%s\n' "$manifest" | grep -F -x "  \"architecture\": \"$arch\"," >/dev/null || {
    echo "manifest in $archive has the wrong architecture" >&2
    exit 1
  }
  for field in \
    '"license": "GPL-2.0-only"' \
    '"database-drivers"' \
    '"io_uring"' \
    '"libaio"' \
    '"psync"' \
    '"ceph"' \
    '"rbd"' \
    '"rados"' \
    '"gluster"' \
    '"rdma"' \
    '"sctp"' \
    '"thread_modes"' \
    '"corpus_sha256"' \
    '"ci_smoke_class"' \
    '"build_target": "apps/openssl"' \
    '"tiny"' \
    '"iputils-parser"'; do
    printf '%s\n' "$manifest" | grep -F "$field" >/dev/null || {
      echo "manifest in $archive is missing feature/license field $field" >&2
      exit 1
    }
  done
done

if find "$package_tmp" -mindepth 1 -print -quit | grep -q .; then
  echo "package.sh left temporary staging files behind" >&2
  find "$package_tmp" -mindepth 1 -print >&2
  exit 1
fi

archive_count=$(find "$repo_root/dist" -maxdepth 1 -type f -name 'ecs-tools_linux_*.tar.gz' | wc -l)
[[ "$archive_count" -eq "${#architectures[@]}" ]] || {
  echo "expected ${#architectures[@]} tool archives, found $archive_count" >&2
  exit 1
}

rm "$stage_root/linux_arm64/manifest.json"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "missing manifest unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'missing or empty manifest for arm64' "$log_file" >/dev/null || {
  echo "missing-manifest error did not identify the architecture" >&2
  cat "$log_file" >&2
  exit 1
}

write_manifest_fixture arm64 "$stage_root/linux_arm64/manifest.json"
sed -i '0,/"architecture": "arm64"/s//"architecture": "bad"/' \
  "$stage_root/linux_arm64/manifest.json"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "bad manifest unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'invalid manifest for arm64' "$log_file" >/dev/null || {
  echo "bad-manifest error did not identify the architecture" >&2
  cat "$log_file" >&2
  exit 1
}

write_manifest_fixture arm64 "$stage_root/linux_arm64/manifest.json"
rm "$stage_root/linux_arm64/bin/fio"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "missing tool unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'missing fio for arm64' "$log_file" >/dev/null || {
  echo "missing-tool error did not identify the architecture and tool" >&2
  cat "$log_file" >&2
  exit 1
}

echo "package tools layout tests passed"
