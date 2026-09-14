#!/usr/bin/env bash
set -euo pipefail

# This fixture exercises only the publisher tree verifier.  It is deliberately
# not an SDK build gate and never substitutes for the real GCC/gfortran,
# driver-chain, runtime-probe or consumer checks.

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
verifier="$script_dir/tree_verify.py"
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/ecs-freebsd-sdk-verify.XXXXXX")
trap 'rm -rf -- "$fixture_dir"' EXIT

for command in go file readelf strip python3 cp ln; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "sdk-tree-verify-fixture: missing command: $command" >&2
    exit 1
  }
done

base="$fixture_dir/base"
mkdir -p "$base/bin" "$base/target"
cat >"$fixture_dir/fixture.go" <<'EOF'
package main

func main() {}
EOF
GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o "$base/bin/host" "$fixture_dir/fixture.go"
file "$base/bin/host" | grep -q 'x86-64' || {
  echo "sdk-tree-verify-fixture: compiler did not produce x86-64 ELF" >&2
  exit 1
}
printf '%s\n' 'unchanged target payload' >"$base/target/data"
ln "$base/bin/host" "$base/bin/host-alias"

# On the amd64 publisher host use the real host strip.  The fallback only
# makes this verifier-only fixture runnable on non-amd64 development hosts;
# it changes the debug section headers without pretending to run an SDK gate.
fixture_strip_debug() {
  local input=$1 output=$2
  if strip --strip-debug -o "$output" "$input" 2>/dev/null; then
    return 0
  fi
  python3 - "$input" "$output" <<'PY'
import pathlib
import struct
import sys

data = bytearray(pathlib.Path(sys.argv[1]).read_bytes())
if data[:4] != b"\x7fELF" or data[4] != 2 or data[5] != 1:
    raise SystemExit("fixture expects a little-endian ELF64")
shoff = struct.unpack_from("<Q", data, 40)[0]
shentsize = struct.unpack_from("<H", data, 58)[0]
shnum = struct.unpack_from("<H", data, 60)[0]
shstrndx = struct.unpack_from("<H", data, 62)[0]
shstr = shoff + shstrndx * shentsize
str_off = struct.unpack_from("<Q", data, shstr + 24)[0]
str_size = struct.unpack_from("<Q", data, shstr + 32)[0]
strings = data[str_off:str_off + str_size]

for index in range(shnum):
    section = shoff + index * shentsize
    name_off = struct.unpack_from("<I", data, section)[0]
    end = strings.find(b"\0", name_off)
    name = strings[name_off:end].decode("utf-8")
    if name.startswith(".debug_"):
        struct.pack_into("<I", data, section + 4, 8)  # SHT_NOBITS
        struct.pack_into("<Q", data, section + 24, 0)
        struct.pack_into("<Q", data, section + 32, 0)

pathlib.Path(sys.argv[2]).write_bytes(data)
PY
}

fixture_mutate_text() {
  local input=$1 output=$2
  python3 - "$input" "$output" <<'PY'
import pathlib
import struct
import sys

data = bytearray(pathlib.Path(sys.argv[1]).read_bytes())
if data[:4] != b"\x7fELF" or data[4] != 2 or data[5] != 1:
    raise SystemExit("fixture expects a little-endian ELF64")
shoff = struct.unpack_from("<Q", data, 40)[0]
shentsize = struct.unpack_from("<H", data, 58)[0]
shnum = struct.unpack_from("<H", data, 60)[0]
shstrndx = struct.unpack_from("<H", data, 62)[0]
shstr = shoff + shstrndx * shentsize
str_off = struct.unpack_from("<Q", data, shstr + 24)[0]
str_size = struct.unpack_from("<Q", data, shstr + 32)[0]
strings = data[str_off:str_off + str_size]

for index in range(shnum):
    section = shoff + index * shentsize
    name_off = struct.unpack_from("<I", data, section)[0]
    end = strings.find(b"\0", name_off)
    name = strings[name_off:end].decode("utf-8")
    if name == ".text":
        offset = struct.unpack_from("<Q", data, section + 24)[0]
        size = struct.unpack_from("<Q", data, section + 32)[0]
        if size == 0:
            raise SystemExit("empty .text section")
        data[offset] ^= 1
        break
else:
    raise SystemExit("missing .text section")

pathlib.Path(sys.argv[2]).write_bytes(data)
PY
}

clean_base="$fixture_dir/clean-base"
cp -a "$base" "$clean_base"
fixture_strip_debug "$clean_base/bin/host" "$fixture_dir/clean-stripped"
cat "$fixture_dir/clean-stripped" >"$clean_base/bin/host"

make_evidence() {
  local root=$1 evidence=$2
  mkdir -p "$evidence"
  python3 "$verifier" manifest "$root" "$evidence/manifest.tsv"
  python3 "$verifier" host-elfs "$evidence/manifest.tsv" \
    "$evidence/host-elf-inodes.tsv"
}

verify_pass() {
  local name=$1 root=$2
  local evidence_before="$fixture_dir/$name-before"
  local evidence_after="$fixture_dir/$name-after"
  make_evidence "$root" "$evidence_after"
  python3 "$verifier" verify-tree \
    "$evidence_before/manifest.tsv" \
    "$evidence_after/manifest.tsv" \
    "$evidence_before/host-elf-inodes.tsv"
  echo "sdk-tree-verify-fixture: $name PASS"
}

verify_fail() {
  local name=$1 root=$2
  local evidence_before="$fixture_dir/$name-before"
  local evidence_after="$fixture_dir/$name-after"
  local log="$fixture_dir/$name.log"
  make_evidence "$root" "$evidence_after"
  if python3 "$verifier" verify-tree \
      "$evidence_before/manifest.tsv" \
      "$evidence_after/manifest.tsv" \
      "$evidence_before/host-elf-inodes.tsv" >"$log" 2>&1; then
    cat "$log" >&2
    echo "sdk-tree-verify-fixture: $name unexpectedly PASS" >&2
    exit 1
  fi
  echo "sdk-tree-verify-fixture: $name FAIL as expected"
}

copy_case() {
  local name=$1
  local source=${2:-$base}
  cp -a "$source" "$fixture_dir/$name"
}

# An untouched tree is accepted.
copy_case identical "$clean_base"
make_evidence "$fixture_dir/identical" "$fixture_dir/identical-before"
verify_pass identical "$fixture_dir/identical"

# The exact allowed publisher mutation is host debug stripping in place.  The
# alias remains hard-linked to the same inode after the write-back.
copy_case debug-strip
make_evidence "$fixture_dir/debug-strip" "$fixture_dir/debug-strip-before"
fixture_strip_debug "$fixture_dir/debug-strip/bin/host" \
  "$fixture_dir/stripped-host"
cat "$fixture_dir/stripped-host" >"$fixture_dir/debug-strip/bin/host"
verify_pass debug-strip "$fixture_dir/debug-strip"

# A non-debug section mutation is not an allowed strip result.
copy_case section-change
make_evidence "$fixture_dir/section-change" \
  "$fixture_dir/section-change-before"
fixture_strip_debug "$fixture_dir/section-change/bin/host" \
  "$fixture_dir/section-stripped"
cat "$fixture_dir/section-stripped" >"$fixture_dir/section-change/bin/host"
fixture_mutate_text "$fixture_dir/section-change/bin/host" \
  "$fixture_dir/section-mutated"
cat "$fixture_dir/section-mutated" >"$fixture_dir/section-change/bin/host"
verify_fail section-change "$fixture_dir/section-change"

# Every other tree mutation is rejected independently.
copy_case non-host-byte-change "$clean_base"
make_evidence "$fixture_dir/non-host-byte-change" \
  "$fixture_dir/non-host-byte-change-before"
printf '%s\n' 'changed target payload' >"$fixture_dir/non-host-byte-change/target/data"
verify_fail non-host-byte-change "$fixture_dir/non-host-byte-change"

copy_case missing-file "$clean_base"
make_evidence "$fixture_dir/missing-file" "$fixture_dir/missing-file-before"
rm -f "$fixture_dir/missing-file/target/data"
verify_fail missing-file "$fixture_dir/missing-file"

copy_case unexpected-file "$clean_base"
make_evidence "$fixture_dir/unexpected-file" \
  "$fixture_dir/unexpected-file-before"
printf '%s\n' 'unexpected' >"$fixture_dir/unexpected-file/new-file"
verify_fail unexpected-file "$fixture_dir/unexpected-file"

copy_case hardlink-change "$clean_base"
make_evidence "$fixture_dir/hardlink-change" \
  "$fixture_dir/hardlink-change-before"
rm -f "$fixture_dir/hardlink-change/bin/host-alias"
cp "$fixture_dir/hardlink-change/bin/host" \
  "$fixture_dir/hardlink-change/bin/host-alias"
verify_fail hardlink-change "$fixture_dir/hardlink-change"

echo "sdk-tree-verify-fixture: all cases passed"
