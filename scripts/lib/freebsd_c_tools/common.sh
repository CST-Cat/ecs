#!/usr/bin/env bash
# Shared helpers for Linux-hosted FreeBSD C-tools builds (Clang + LLD).

set -euo pipefail

ecs_freebsd_c_die() {
  echo "build-tools-freebsd-c: $*" >&2
  exit 1
}

# Create clang/clang++ wrappers that always target the FreeBSD sysroot.
# Real compilers are invoked by absolute path: a bare `clang` would resolve
# back to this wrapper via PATH and recurse until ARG_MAX.
# The wrappers are probed with the same CC/CFLAGS/LDFLAGS/PKG_CONFIG_PATH
# environment that configure will later see.
ecs_freebsd_c_write_wrappers() {
  local work=$1 triple=$2 sysroot=$3
  local bin="$work/clang-wrap"
  local real_clang real_clangxx
  real_clang=$(command -v clang) || ecs_freebsd_c_die "clang not found before writing wrappers"
  real_clangxx=$(command -v clang++) || ecs_freebsd_c_die "clang++ not found before writing wrappers"
  mkdir -p "$bin"
  cat >"$bin/clang" <<EOF
#!/usr/bin/env bash
exec $real_clang --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  cat >"$bin/clang++" <<EOF
#!/usr/bin/env bash
exec $real_clangxx --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  cat >"$bin/cpp" <<EOF
#!/usr/bin/env bash
exec $real_clang -E --target=$triple --sysroot=$sysroot -fuse-ld=lld "\$@"
EOF
  chmod +x "$bin/clang" "$bin/clang++" "$bin/cpp"
  echo "$bin"
}

# Probe must use the exact toolchain environment later configure will use.
ecs_freebsd_c_probe_wrappers() {
  local work=$1
  local probe="$work/wrapper-probe"
  mkdir -p "$probe"
  cat >"$probe/hello.c" <<'EOF'
#include <stdio.h>
int main(void) { puts("wrapper-ok"); return 0; }
EOF
  # Intentionally pass the same exported CC/CFLAGS/LDFLAGS used by builders.
  "$CC" $CFLAGS $CPPFLAGS -o "$probe/hello" "$probe/hello.c" $LDFLAGS ||
    ecs_freebsd_c_die "wrapper probe failed with CC=$CC CFLAGS=$CFLAGS LDFLAGS=$LDFLAGS"
  [[ -x "$probe/hello" ]] || ecs_freebsd_c_die "wrapper probe produced no binary"
  local file_out
  file_out=$(file "$probe/hello")
  echo "wrapper probe file: $file_out" >&2
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_c_die "wrapper probe is not a FreeBSD ELF: $file_out" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_c_die "wrapper probe is not static: $file_out" ;;
  esac
}

ecs_freebsd_c_assert_static_freebsd_elf() {
  local bin=$1 elf_machine=$2
  [[ -x "$bin" ]] || ecs_freebsd_c_die "missing executable: $bin"
  local file_out
  file_out=$(file "$bin")
  echo "verify $bin: $file_out" >&2
  case "$file_out" in
    *"$elf_machine"*) ;;
    *) ecs_freebsd_c_die "$bin architecture is not $elf_machine" ;;
  esac
  case "$file_out" in
    *FreeBSD*) ;;
    *) ecs_freebsd_c_die "$bin is not a FreeBSD ELF" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) ecs_freebsd_c_die "$bin is not static" ;;
  esac
  if readelf -d "$bin" 2>/dev/null | grep -q NEEDED; then
    ecs_freebsd_c_die "$bin has dynamic NEEDED entries"
  fi
  if strings "$bin" 2>/dev/null | grep -q 'GLIBC_'; then
    ecs_freebsd_c_die "$bin contains glibc symbols"
  fi
}

ecs_freebsd_c_clone_tool() {
  local repository=$1 tag=$2 expected_commit=$3 dest=$4
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/${repository}.git" "$dest" >/dev/null
  local actual
  actual=$(git -C "$dest" rev-parse HEAD)
  [[ "$actual" == "$expected_commit" ]] ||
    ecs_freebsd_c_die "commit mismatch for $repository $tag: expected=$expected_commit actual=$actual"
}

# ---------------------------------------------------------------------------
# Stage-level release post-processing (contract: strip the final 8 tools).
#
# Runs ONCE, after all five tools are built and before provenance/SHA256SUMS
# are written: release post-processing is stage policy, not per-tool build
# logic, so it must not be duplicated inside sysbench.sh/fio.sh/... .
#
# `$STRIP` (llvm-strip) --strip-unneeded may only remove non-allocated
# debug/symbol metadata. To prove that, the runtime-relevant ELF view (ELF
# header identity, DT_NEEDED state, every PT_LOAD program header and every
# SHF_ALLOC section including content SHA-256) is captured before and after
# the strip and compared; any difference fails the build. The post-strip
# structural assert re-runs the per-tool contract, and no .debug_* section
# may survive.
# ---------------------------------------------------------------------------

ecs_freebsd_c_write_elfmeta_script() {
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

ecs_freebsd_c_strip_release_binaries() {
  local stage=$1 work=$2
  command -v "$STRIP" >/dev/null 2>&1 ||
    ecs_freebsd_c_die "strip tool is missing: $STRIP"
  local py="$work/elfmeta.py"
  ecs_freebsd_c_write_elfmeta_script "$py"
  local tool bin before after
  for tool in sysbench zstd openssl fio iperf3; do
    bin="$stage/bin/$tool"
    [[ -x "$bin" ]] || ecs_freebsd_c_die "strip stage: missing binary $tool"
    before="$work/$tool.strip-before.json"
    after="$work/$tool.strip-after.json"
    python3 "$py" capture "$bin" "$before"
    "$STRIP" --strip-unneeded "$bin"
    # Post-strip structural assert: the same contract the per-tool builds
    # enforce (architecture/FreeBSD identity/static/no NEEDED/no GLIBC_).
    ecs_freebsd_c_assert_static_freebsd_elf "$bin" "$ecs_freebsd_elf_machine"
    python3 "$py" capture "$bin" "$after"
    python3 "$py" compare "$before" "$after" "$tool"
    python3 "$py" no-debug "$bin" "$tool"
  done
}

ecs_freebsd_c_write_provenance() {
  local stage=$1 target=$2 triple=$3
  local bin="$stage/bin"
  {
    echo '{'
    echo "  \"target\": \"$target\","
    echo "  \"target_triple\": \"$triple\","
    echo "  \"build_host\": \"ubuntu-24.04-amd64\","
    echo "  \"toolchain_mode\": \"cross\","
    echo "  \"compiler_family\": \"clang\","
    echo "  \"compiler_version\": \"$(clang --version | head -n1 | sed 's/"/\\"/g')\","
    echo "  \"linker\": \"lld\","
    echo "  \"openmp_runtime\": null,"
    echo '  "tools": ['
    local first=1 name
    for name in sysbench zstd openssl fio iperf3; do
      [[ -x "$bin/$name" ]] || continue
      local sha
      sha=$(sha256sum "$bin/$name" | awk '{print $1}')
      if [[ $first -eq 0 ]]; then echo ','; fi
      first=0
      printf '    {"name":"%s","sha256":"%s","compiler_family":"clang"}' "$name" "$sha"
    done
    echo
    echo '  ]'
    echo '}'
  } >"$stage/provenance.json"
}
