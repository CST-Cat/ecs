# shellcheck shell=bash

lock_tool_field() {
  local tool=$1 field=$2
  jq -er --arg tool "$tool" --arg field "$field" \
    '.tools[] | select(.name == $tool) | .[$field] // empty' "$lock_file"
}

sha256_file() {
  sha256 -q "$1"
}

download_sha256() {
  local url=$1 expected=$2 output=$3 label=$4 actual
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 "$url" -o "$output"
  else
    fetch -o "$output" "$url"
  fi
  actual=$(sha256_file "$output")
  [[ "$actual" == "$expected" ]] || die "$label SHA-256 mismatch: expected $expected, got $actual"
}

clone_release() {
  local repository=$1 tag=$2 expected=$3 destination=$4 actual
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/$repository.git" "$destination" >/dev/null
  actual=$(git -C "$destination" rev-parse HEAD)
  [[ "$actual" == "$expected" ]] || die "$repository $tag resolved to $actual, expected $expected"
}

git_source() {
  printf 'git+https://github.com/%s.git@%s\n' "$1" "$2"
}

# Mirror the Linux bundle contract: compilers always run directly on the build
# host, while only target binaries may be wrapped by a user-mode emulator.
# Native FreeBSD CI leaves ECS_TARGET_RUNNER unset, so this is a zero-behavior-
# change refactor until a cross-build target explicitly selects a runner such as
# qemu-aarch64-static.
target_runner_command=()
smoke_runner=direct
if [[ -n "${ECS_TARGET_RUNNER:-}" ]]; then
  command -v "$ECS_TARGET_RUNNER" >/dev/null 2>&1 ||
    die "target runner is missing: $ECS_TARGET_RUNNER"
  target_runner_command=("$ECS_TARGET_RUNNER")
  smoke_runner=$ECS_TARGET_RUNNER
fi

run_target() {
  if [[ "${#target_runner_command[@]}" -gt 0 ]]; then
    "${target_runner_command[@]}" "$@"
  else
    "$@"
  fi
}

require_layout() {
  [[ -d "$work" ]] || die "phase $phase requires prepared work directory: $work"
  [[ -d "$stage/bin" && -d "$stage/LICENSES" ]] ||
    die "phase $phase requires prepared stage: $stage"
}

require_sources() {
  require_layout
  [[ -d "$sysbench_src/.git" ]] || die 'prepared sysbench source is missing'
  [[ -d "$zstd_src/.git" ]] || die 'prepared zstd source is missing'
  [[ -d "$openssl_src/.git" ]] || die 'prepared OpenSSL source is missing'
  [[ -d "$fio_src/.git" ]] || die 'prepared fio source is missing'
  [[ -d "$iperf3_src/.git" ]] || die 'prepared iperf3 source is missing'
  [[ -d "$npb_src/EP" && -d "$npb_src/FT" ]] || die 'prepared NPB source is missing'
  [[ -s "$stream_src" ]] || die 'prepared STREAM source is missing'
}

validate_freebsd_elf_identity() {
  local tool=$1 binary=$2
  local header="$work/${tool}.elf-header"
  local notes="$work/${tool}.elf-notes"

  if ! readelf -hW "$binary" >"$header" 2>&1; then
    cat "$header" >&2
    die "ELF-header readelf failed for $tool"
  fi

  # GCC-built FreeBSD/aarch64 binaries can retain a GNU/Linux EI_OSABI value
  # while carrying the authoritative FreeBSD ABI version in .note.tag. Accept
  # either the explicit ELF OS/ABI or a FreeBSD-owned ELF note; do not infer the
  # target OS from architecture or from the human-oriented `file` description.
  if grep -Eq 'OS/ABI:[[:space:]]+UNIX - FreeBSD' "$header"; then
    return 0
  fi

  if ! readelf -nW "$binary" >"$notes" 2>&1; then
    cat "$header" "$notes" >&2
    die "ELF-note readelf failed for $tool"
  fi
  if grep -Eq '(^|[[:space:]])FreeBSD([[:space:]]|$)' "$notes"; then
    return 0
  fi

  cat "$header" "$notes" >&2
  die "$tool is missing FreeBSD ELF ABI identity"
}

validate_binary() {
  local tool=$1
  local binary="$stage/bin/$tool"
  chmod 0755 "$binary"
  [[ -s "$binary" ]] || die "built $tool is empty"
  file "$binary"
  validate_freebsd_elf_identity "$tool" "$binary"
  readelf -dW "$binary" >"$work/${tool}.dynamic" 2>&1 ||
    die "dynamic-header readelf failed for $tool"
  readelf -lW "$binary" >"$work/${tool}.program" 2>&1 ||
    die "program-header readelf failed for $tool"
  if grep -Eq '\(NEEDED\)' "$work/${tool}.dynamic" ||
    grep -Eq '(^|[[:space:]])INTERP([[:space:]]|$)' "$work/${tool}.program"; then
    cat "$work/${tool}.dynamic" "$work/${tool}.program" >&2
    die "$tool is not fully static"
  fi
}

phase_sources() {
  [[ ! -e "$work" ]] || die "deterministic build directory already exists: $work"
  [[ ! -e "$stage" ]] || die "target stage already exists: $stage"
  mkdir -p -- "$stage_root"
  mkdir -m 0700 -- "$work"
  mkdir -p "$stage/bin" "$stage/LICENSES"
  stage_created=1

  clone_release "$sysbench_repository" "$sysbench_tag" "$sysbench_commit" "$sysbench_src"
  clone_release "$zstd_repository" "$zstd_tag" "$zstd_commit" "$zstd_src"
  clone_release "$openssl_repository" "$openssl_tag" "$openssl_commit" "$openssl_src"
  clone_release "$fio_repository" "$fio_tag" "$fio_commit" "$fio_src"
  clone_release "$iperf3_repository" "$iperf3_tag" "$iperf3_commit" "$iperf3_src"

  download_sha256 "$npb_url" "$npb_sha" "$npb_archive" "NPB $npb_tag source archive"
  tar -xzf "$npb_archive" -C "$work"
  [[ -d "$npb_src/EP" && -d "$npb_src/FT" ]] || die 'NPB archive omitted NPB3.4-OMP EP or FT'

  download_sha256 "$stream_url" "$stream_sha" "$stream_src" 'official STREAM source'
  stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
  [[ -n "$stream_revision" ]] || die 'could not read official STREAM revision'
}
