#!/usr/bin/env bash
# Shared helpers for the Linux-hosted FreeBSD GNU/OpenMP benchmark builds
# (Stage 5: NPB EP/FT + STREAM via the Stage 4 GNU SDK).
#
# common.sh is needed here (in addition to scripts/lib/common.sh) because the
# GNU stage has its own toolchain plumbing: it consumes a relocated Stage 4
# SDK artifact instead of a host clang, and the structural contract differs
# (libgomp instead of no-OpenMP, GNU target triple instead of clang wrappers).

set -euo pipefail

ecs_freebsd_gnu_die() {
  echo "build-tools-freebsd-gnu: $*" >&2
  exit 1
}

# upload-artifact v4 does not preserve file permissions: once the Stage 4 SDK
# artifact has been downloaded, the driver, cc1/f951/collect2 and the target
# binutils have lost their executable bit. Restore it before any probe.
ecs_freebsd_gnu_restore_sdk_exec_bits() {
  local sdk_prefix=$1 triple=$2 dir
  for dir in "$sdk_prefix/bin" "$sdk_prefix/libexec" "$sdk_prefix/$triple/bin"; do
    [[ -d "$dir" ]] || continue
    find "$dir" -type f -exec chmod +x {} +
  done
}

ecs_freebsd_gnu_validate_sdk() {
  local sdk_prefix=$1 triple=$2
  [[ -d "$sdk_prefix" ]] ||
    ecs_freebsd_gnu_die "GNU SDK prefix does not exist: $sdk_prefix"
  [[ -x "$sdk_prefix/bin/${triple}-gcc" ]] ||
    ecs_freebsd_gnu_die "target gcc missing in SDK prefix: $sdk_prefix/bin/${triple}-gcc"
  [[ -x "$sdk_prefix/bin/${triple}-gfortran" ]] ||
    ecs_freebsd_gnu_die "target gfortran missing in SDK prefix: $sdk_prefix/bin/${triple}-gfortran"
  local gcc_report
  gcc_report=$("$sdk_prefix/bin/${triple}-gcc" --version | head -n1)
  echo "sdk target gcc: $gcc_report" >&2
}

# Create gcc/gfortran wrappers that always pass the freshly installed FreeBSD
# sysroot. The SDK driver embeds the sysroot path of the machine it was built
# on; passing --sysroot explicitly makes the artifact relocation-safe.
# Real compilers are invoked by absolute path: a bare `gcc`/`gfortran` on PATH
# could resolve back to the wrapper and recurse until ARG_MAX (same lesson as
# the C-tools clang wrappers).
ecs_freebsd_gnu_write_wrappers() {
  local work=$1 sdk_prefix=$2 triple=$3 sysroot=$4
  local bin="$work/gnu-wrap"
  local real_gcc="$sdk_prefix/bin/${triple}-gcc"
  local real_gfortran="$sdk_prefix/bin/${triple}-gfortran"
  mkdir -p "$bin"
  cat >"$bin/gcc" <<EOF
#!/usr/bin/env bash
exec $real_gcc --sysroot=$sysroot "\$@"
EOF
  cat >"$bin/gfortran" <<EOF
#!/usr/bin/env bash
exec $real_gfortran --sysroot=$sysroot "\$@"
EOF
  chmod +x "$bin/gcc" "$bin/gfortran"
  echo "$bin"
}

# Probe the relocated SDK with the exact environment shape the benchmarks
# will use: static C and Fortran OpenMP hellos against the fresh sysroot.
ecs_freebsd_gnu_probe_wrappers() {
  local work=$1 file_machine=$2
  local probe="$work/wrapper-probe"
  mkdir -p "$probe"

  cat >"$probe/omp.c" <<'EOF'
#include <stdio.h>
int main(void) {
    int n = 0;
#pragma omp parallel
    {
#pragma omp atomic
        n += 1;
    }
    printf("gnu-c-openmp-threads=%d\n", n);
    return n > 0 ? 0 : 1;
}
EOF
  "$work/gnu-wrap/gcc" -O2 -fopenmp -static -o "$probe/omp-c" "$probe/omp.c" ||
    ecs_freebsd_gnu_die "wrapper probe failed: static C OpenMP hello did not compile"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$probe/omp-c" "$file_machine"

  cat >"$probe/omp.f90" <<'EOF'
program ompf
  use omp_lib
  integer :: n
  n = 0
!$omp parallel
!$omp atomic
  n = n + 1
!$omp end parallel
  print *, 'gnu-fortran-openmp-threads=', n
end program ompf
EOF
  "$work/gnu-wrap/gfortran" -O2 -fopenmp -static -o "$probe/omp-f" "$probe/omp.f90" ||
    ecs_freebsd_gnu_die "wrapper probe failed: static Fortran OpenMP hello did not compile"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$probe/omp-f" "$file_machine"

  echo "wrapper probe ok: static FreeBSD OpenMP C + Fortran hellos" >&2
}

# file_machine is the token `file` prints for the target CPU: x86-64 for
# freebsd_amd64, aarch64 for freebsd_arm64 (empirically fixed by the Stage 4
# probe logs). The FreeBSD identity comes either from the ELF OS/ABI byte
# (amd64: "version 1 (FreeBSD)") or from the FreeBSD ABI note (arm64 GNU ld
# keeps OS/ABI at 0 and `file` prints "for FreeBSD <rel>, FreeBSD-style").
ecs_freebsd_gnu_assert_static_freebsd_elf() {
  local bin=$1 file_machine=$2
  [[ -x "$bin" ]] || ecs_freebsd_gnu_die "missing executable: $bin"
  local file_out
  file_out=$(file "$bin")
  echo "verify $bin: $file_out" >&2
  case "$file_out" in
    *"$file_machine"*) ;;
    *) ecs_freebsd_gnu_die "$bin architecture is not $file_machine" ;;
  esac
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_gnu_die "$bin is not a FreeBSD ELF" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_gnu_die "$bin is not static" ;;
  esac
  if readelf -d "$bin" 2>/dev/null | grep -q NEEDED; then
    ecs_freebsd_gnu_die "$bin has dynamic NEEDED entries"
  fi
  if strings "$bin" 2>/dev/null | grep -q 'GLIBC_'; then
    ecs_freebsd_gnu_die "$bin contains glibc symbols"
  fi
}

# Structural libgomp proof: GNU libgomp symbols are statically linked into the
# binary; LLVM libomp's __kmpc_* runtime symbols must be absent.
ecs_freebsd_gnu_assert_libgomp() {
  local bin=$1 gomp kmpc
  gomp=$(nm "$bin" 2>/dev/null | grep -c ' GOMP_' || true)
  kmpc=$(nm "$bin" 2>/dev/null | grep -c '__kmpc_' || true)
  [[ "$gomp" -gt 0 ]] ||
    ecs_freebsd_gnu_die "$bin has no GOMP_ symbols (libgomp runtime missing)"
  [[ "$kmpc" -eq 0 ]] ||
    ecs_freebsd_gnu_die "$bin contains LLVM libomp (__kmpc_) symbols"
}

# ---------------------------------------------------------------------------
# Stage-level release post-processing (contract: strip the final 8 tools).
#
# Runs ONCE, after ecs_freebsd_gnu_build_npb / ecs_freebsd_gnu_build_stream
# have finished and before provenance/SHA256SUMS are written. The per-tool
# builds already proved FreeBSD/static/arch and GOMP_ present / __kmpc_
# absent (ecs_freebsd_gnu_assert_libgomp) while the symbol table still
# exists; that proof must stay where it is, before the strip —
# --strip-unneeded removes the symbol table it reads — so it is neither
# moved nor repeated here.
#
# The SDK's target strip (${triple}-strip) --strip-unneeded may only remove
# non-allocated debug/symbol metadata. To prove that, the runtime-relevant
# ELF view (ELF header identity, DT_NEEDED state, every PT_LOAD program
# header and every SHF_ALLOC section including content SHA-256) is captured
# before and after the strip and compared; any difference fails the build.
# The structural assert runs again post-strip, and no .debug_* section may
# survive.
# ---------------------------------------------------------------------------

ecs_freebsd_gnu_write_elfmeta_script() {
  local dest=$1
  cat >"$dest" <<'ELFMETA_PY'
#!/usr/bin/env python3
"""Capture and compare the runtime-relevant ELF view for strip equivalence.

Subcommands:
  capture <bin> <out.json>    Record the runtime-relevant metadata of <bin>.
  compare <before> <after> <tool>
                              Fail unless the two captures are identical in
                              every runtime-relevant field.
  no-debug <bin> <tool>       Fail if any .debug_* section remains.

Runtime-relevant view (what a metadata-only strip must not change):
  - ELF header identity: class, data, type, machine, entry point, OS/ABI,
    ABI version, flags;
  - the DT_NEEDED list (must stay empty for these static binaries);
  - every PT_LOAD program header: offset, vaddr, paddr, filesz, memsz,
    flags, align;
  - every SHF_ALLOC section: name, type, flags, address, offset, size and
    the SHA-256 of its file content (SHT_NOBITS sections have no file
    content: only size and address are recorded).

Only non-allocated debug/symbol metadata (.debug_*, .symtab, .strtab,
.comment, ...) may differ between the two captures. Comparison follows the
stage contract exactly: content-bearing SHF_ALLOC sections compare
flags/size/address/offset/content, NOBITS sections compare size/address
only, and zero-size sections skip the meaningless file offset.
"""
import hashlib
import json
import os
import re
import subprocess
import sys

SECTION_LINE = re.compile(r"^\s*\[\s*(\d+)\]\s*(.*)$")


def run(args):
    proc = subprocess.run(args, check=True, capture_output=True, text=True,
                          env=dict(os.environ, LC_ALL="C"))
    return proc.stdout


def elf_header_fields(readelf_h):
    fields = {}
    for line in readelf_h.splitlines():
        if ":" in line:
            key, value = line.split(":", 1)
            fields[key.strip()] = value.strip()
    keys = ("Class", "Data", "Type", "Machine", "Entry point address",
            "OS/ABI", "ABI Version", "Flags")
    missing = [key for key in keys if key not in fields]
    if missing:
        raise SystemExit("readelf -h is missing fields: " + ", ".join(missing))
    return {key: fields[key] for key in keys}


def parse_sections(readelf_sw):
    """Right-anchored parse of `readelf -SW` rows (Phase 0 baseline method)."""
    out = []
    for line in readelf_sw.splitlines():
        m = SECTION_LINE.match(line)
        if not m:
            continue
        nr = int(m.group(1))
        toks = m.group(2).split()
        al, inf, lk = toks[-1], toks[-2], toks[-3]
        if re.fullmatch(r"[0-9a-f]{1,2}", toks[-4]):
            flg, es = "", toks[-4]
            size, off, addr, typ = toks[-5], toks[-6], toks[-7], toks[-8]
        else:
            flg, es = toks[-4], toks[-5]
            size, off, addr, typ = toks[-6], toks[-7], toks[-8], toks[-9]
        if nr == 0:
            name = ""
        else:
            name = toks[0]
            if toks[1] != typ:
                raise SystemExit("token/type mismatch on [%d]: %s" % (nr, line))
        out.append({"nr": nr, "name": name, "type": typ,
                    "address": "0x" + addr, "offset": "0x" + off,
                    "size": int(size, 16), "flags": flg})
    return out


def load_segments(readelf_lw):
    out = []
    for line in readelf_lw.splitlines():
        toks = line.strip().split()
        if not toks or toks[0] != "LOAD":
            continue
        rest = toks[1:]
        if len(rest) == 7:
            off, va, pa, fsz, msz, flg, aln = rest
        elif len(rest) == 8:
            # wide flag columns such as "R E" split into two tokens
            off, va, pa, fsz, msz = rest[:5]
            flg, aln = rest[5] + " " + rest[6], rest[7]
        else:
            raise SystemExit("unexpected readelf -lW LOAD row: " + line.strip())
        out.append({"offset": off, "vaddr": va, "paddr": pa,
                    "filesz": int(fsz, 16), "memsz": int(msz, 16),
                    "flags": flg, "align": aln})
    return out


def capture(bin_path, out_path):
    header = elf_header_fields(run(["readelf", "-h", bin_path]))
    dyn = run(["readelf", "-d", bin_path])
    needed = re.findall(r"\(NEEDED\)\s+Shared library: \[(.*?)\]", dyn)
    loads = load_segments(run(["readelf", "-lW", bin_path]))
    if not loads:
        raise SystemExit("no PT_LOAD program headers parsed in " + bin_path)
    sections = parse_sections(run(["readelf", "-SW", bin_path]))
    file_size = os.path.getsize(bin_path)
    with open(bin_path, "rb") as fh:
        data = fh.read()
    if len(data) != file_size:
        raise SystemExit("file changed while reading: " + bin_path)
    allocs = []
    for sec in sections:
        if "A" not in sec["flags"]:
            continue
        rec = {key: sec[key]
               for key in ("name", "type", "flags", "address", "offset", "size")}
        if sec["type"] == "NOBITS":
            rec["content_sha256"] = None
        else:
            off = int(sec["offset"], 16)
            if off + sec["size"] > file_size:
                raise SystemExit("section %s out of file bounds in %s" %
                                 (sec["name"], bin_path))
            rec["content_sha256"] = hashlib.sha256(
                data[off:off + sec["size"]]).hexdigest()
        allocs.append(rec)
    if not allocs or not any(sec["name"] == ".text" for sec in allocs):
        raise SystemExit("implausible SHF_ALLOC set parsed in " + bin_path)
    record = {"elf_header": header, "dt_needed": needed,
              "load_segments": loads, "alloc_sections": allocs}
    with open(out_path, "w") as fh:
        json.dump(record, fh, indent=1, sort_keys=True)
    print("elfmeta: captured %s (%d PT_LOAD, %d SHF_ALLOC sections)" %
          (bin_path, len(loads), len(allocs)))


def compare(before_path, after_path, tool):
    with open(before_path) as fh:
        before = json.load(fh)
    with open(after_path) as fh:
        after = json.load(fh)
    problems = []
    if before["dt_needed"] or after["dt_needed"]:
        problems.append("dt_needed must stay empty: before=%r after=%r" %
                        (before["dt_needed"], after["dt_needed"]))
    for key in sorted(set(before["elf_header"]) | set(after["elf_header"])):
        if before["elf_header"].get(key) != after["elf_header"].get(key):
            problems.append("elf_header %s: before=%r after=%r" %
                            (key, before["elf_header"].get(key),
                             after["elf_header"].get(key)))
    b_loads, a_loads = before["load_segments"], after["load_segments"]
    if len(b_loads) != len(a_loads):
        problems.append("PT_LOAD count: before=%d after=%d" %
                        (len(b_loads), len(a_loads)))
    else:
        for index, (b_seg, a_seg) in enumerate(zip(b_loads, a_loads)):
            for key in sorted(set(b_seg) | set(a_seg)):
                if b_seg.get(key) != a_seg.get(key):
                    problems.append("PT_LOAD[%d] %s: before=%r after=%r" %
                                    (index, key, b_seg.get(key), a_seg.get(key)))
    b_secs = {sec["name"]: sec for sec in before["alloc_sections"]}
    a_secs = {sec["name"]: sec for sec in after["alloc_sections"]}
    for name in sorted(set(b_secs) - set(a_secs)):
        problems.append("SHF_ALLOC section vanished: %s" % name)
    for name in sorted(set(a_secs) - set(b_secs)):
        problems.append("SHF_ALLOC section appeared: %s" % name)
    for name in sorted(set(b_secs) & set(a_secs)):
        b, a = b_secs[name], a_secs[name]
        # Contract semantics: every SHF_ALLOC section must match on
        # name/flags/size/address plus its file content hash; NOBITS
        # sections (.bss/.tbss style) compare size/address only, and file
        # offsets carry no runtime meaning for content-less sections
        # (NOBITS or zero-size) — a strip may legitimately reassign those.
        if b["type"] == "NOBITS":
            keys = ("size", "address")
        elif b["size"] == 0:
            keys = ("flags", "size", "address")
        else:
            keys = ("flags", "size", "address", "offset", "content_sha256")
        for key in keys:
            if b.get(key) != a.get(key):
                problems.append("SHF_ALLOC %s %s: before=%r after=%r" %
                                (name, key, b.get(key), a.get(key)))
    if problems:
        for problem in problems:
            print("strip-equivalence MISMATCH (%s): %s" % (tool, problem))
        raise SystemExit("strip equivalence failed for %s: %d problem(s)" %
                         (tool, len(problems)))
    print("strip equivalence OK (%s): %d PT_LOAD segments, %d SHF_ALLOC "
          "sections byte-identical" % (tool, len(b_loads), len(b_secs)))


def no_debug(bin_path, tool):
    sections = parse_sections(run(["readelf", "-SW", bin_path]))
    remaining = sorted({sec["name"] for sec in sections
                        if sec["name"].startswith(".debug_")})
    if remaining:
        raise SystemExit("%s still has .debug_* sections after strip: %s" %
                         (tool, ", ".join(remaining)))
    print("no .debug_* sections remain in %s" % tool)


def main(argv):
    if len(argv) == 4 and argv[1] == "capture":
        capture(argv[2], argv[3])
    elif len(argv) == 5 and argv[1] == "compare":
        compare(argv[2], argv[3], argv[4])
    elif len(argv) == 4 and argv[1] == "no-debug":
        no_debug(argv[2], argv[3])
    else:
        raise SystemExit(__doc__)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
ELFMETA_PY
}

ecs_freebsd_gnu_strip_release_binaries() {
  local stage=$1 work=$2 sdk_prefix=$3 triple=$4 file_machine=$5
  local strip_bin="$sdk_prefix/bin/${triple}-strip"
  [[ -x "$strip_bin" ]] ||
    ecs_freebsd_gnu_die "target strip missing in the GNU SDK: $strip_bin"
  local py="$work/elfmeta.py"
  ecs_freebsd_gnu_write_elfmeta_script "$py"
  local tool bin before after
  for tool in npb-ep npb-ft stream; do
    bin="$stage/bin/$tool"
    [[ -x "$bin" ]] || ecs_freebsd_gnu_die "strip stage: missing binary $tool"
    before="$work/$tool.strip-before.json"
    after="$work/$tool.strip-after.json"
    python3 "$py" capture "$bin" "$before"
    "$strip_bin" --strip-unneeded "$bin"
    # Post-strip structural assert: the same contract the per-tool builds
    # enforce (architecture/FreeBSD identity/static/no NEEDED/no GLIBC_).
    ecs_freebsd_gnu_assert_static_freebsd_elf "$bin" "$file_machine"
    python3 "$py" capture "$bin" "$after"
    python3 "$py" compare "$before" "$after" "$tool"
    python3 "$py" no-debug "$bin" "$tool"
  done
}

# Per-tool provenance records. Every tool carries the full Stage 5 contract:
# gcc/14.2.0 cross on ubuntu-24.04 with libgomp, plus its pinned source URL
# and SHA-256 (NPB for npb-ep/npb-ft, STREAM for stream).
ecs_freebsd_gnu_write_provenance() {
  local stage=$1 target=$2 triple=$3 gcc_version=$4
  local npb_url=$5 npb_sha=$6 stream_url=$7 stream_sha=$8
  local bin="$stage/bin"
  local -a records=()
  local name sha source_json
  for name in npb-ep npb-ft stream; do
    [[ -x "$bin/$name" ]] || ecs_freebsd_gnu_die "provenance: missing binary $name"
    sha=$(sha256sum "$bin/$name" | awk '{print $1}')
    case "$name" in
      npb-ep | npb-ft)
        source_json=$(jq -cn --arg url "$npb_url" --arg sha "$npb_sha" \
          '{name:"NPB",version:"3.4.4",url:$url,sha256:$sha}')
        ;;
      stream)
        source_json=$(jq -cn --arg url "$stream_url" --arg sha "$stream_sha" \
          '{name:"STREAM",url:$url,sha256:$sha}')
        ;;
    esac
    records+=("$(jq -cn \
      --arg name "$name" --arg sha "$sha" --arg triple "$triple" \
      --arg gcc "$gcc_version" --argjson source "$source_json" \
      '{name:$name,sha256:$sha,compiler_family:"gcc",compiler_version:$gcc,target_triple:$triple,build_host:"ubuntu-24.04",openmp_runtime:"libgomp",source:$source}')")
  done
  jq -n \
    --arg target "$target" --arg triple "$triple" --arg gcc "$gcc_version" \
    --argjson tools "$(printf '%s\n' "${records[@]}" | jq -s .)" \
    '{target:$target,target_triple:$triple,build_host:"ubuntu-24.04",toolchain_mode:"cross",compiler_family:"gcc",compiler_version:$gcc,openmp_runtime:"libgomp",tools:$tools}' \
    >"$stage/provenance.json"
}
