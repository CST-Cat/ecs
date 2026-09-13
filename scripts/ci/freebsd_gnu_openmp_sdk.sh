#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD GNU C/Fortran/OpenMP SDK (Stage 4).
#
# Default mode consumes the immutable prebuilt snapshot pinned in the lock
# (Release ci-freebsd-gnu-sdk-v1, published by freebsd-sdk-release.yml):
# download, verify SHA256, unpack, then run the full assertion and probe
# suite. --acquire-only stops after the same download/verify/unpack (no
# probes, no build, no sysroot install): it re-acquires the exact bytes the
# gate job already probed, pinned by the same SHA256. --from-source instead
# builds Binutils 2.43.1 and GCC 14.2.0 (c,fortran only) targeting the
# FreeBSD 15.1 sysroot. GCC is configured with --disable-lto and
# --disable-gcov, the host-side ELF binaries are slimmed with an exact
# `strip --strip-debug` pass, and the two documentation directories
# (share/man, share/info) are dropped from the CI snapshot.
#
# Probes: C/Fortran static hello, ieee_arithmetic, C OpenMP, Fortran OpenMP.
# Asserts required runtime libs exist and g++/libstdc++ do not.
#
# Slimming pipeline (REQUIREMENTS.md stages 2-5, --from-source publisher
# only), in order: install (GCC configured --disable-lto/--disable-gcov)
# → drop share/man + share/info (stage 5: exactly these two directories)
# → A-side probes + consumer workload on the pre-strip SDK → host ELF
# --strip-debug (stage 2: host ELFs identified by byte identity,
# e_machine == x86-64 and OS/ABI != FreeBSD, inode-deduplicated so hardlink
# aliases stay shared; no component removed, target ELFs/.a/.o/.mod/headers
# stay byte-identical) → LTO/gcov inventory (stages 3/4: existence recorded,
# never hand-deleted) → B-side probes + consumer workload on the final SDK.
# Every A/B output ELF (NPB EP/FT Class A and STREAM via
# scripts/build_tools_freebsd_gnu.sh, plus the five probe binaries) must
# have identical SHF_ALLOC section contents, and the driver -### call-chain
# check must resolve every cc1/f951/collect2/as/ld/plugin/wrapper reference.
# The gate probes always run on the bytes that get published (post-strip in
# from-source mode), so the release proves its own final bytes.

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

LOCK_FILE="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"
SYSROOT_LOCK="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_gnu_openmp_sdk.sh --target freebsd_amd64|freebsd_arm64
                                            --prefix DIR [--work-dir DIR] [--jobs N]
                                            [--acquire-only] [--from-source]
       scripts/ci/freebsd_gnu_openmp_sdk.sh --print-lock --target TARGET
USAGE
}

die() {
  echo "freebsd-gnu-openmp-sdk: $*" >&2
  exit 1
}

target=""
prefix=""
work_dir=""
print_lock=0
from_source=0
acquire_only=0
jobs="${JOBS:-$(nproc 2>/dev/null || echo 2)}"
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
    --jobs)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--jobs requires a value"
      jobs=$2
      shift 2
      ;;
    --from-source)
      from_source=1
      shift
      ;;
    --acquire-only)
      acquire_only=1
      shift
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

[[ -s "$LOCK_FILE" ]] || die "missing gnu lock: $LOCK_FILE"
[[ "$(jq -er '.schema_version' "$LOCK_FILE")" == "ecs.freebsd-gnu-openmp.lock/v1" ]] ||
  die "unsupported gnu lock schema"
case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac

gnu_triple=$(jq -er --arg t "$target" '.targets[$t].gnu_target_triple' "$LOCK_FILE")
clang_triple=$(jq -er --arg t "$target" '.targets[$t].clang_target_triple' "$SYSROOT_LOCK")
gcc_version=$(jq -er '.gcc_version' "$LOCK_FILE")
binutils_version=$(jq -er '.binutils_version' "$LOCK_FILE")
gcc_url=$(jq -er '.sources.gcc.url' "$LOCK_FILE")
gcc_sha=$(jq -er '.sources.gcc.sha256' "$LOCK_FILE")
binutils_url=$(jq -er '.sources.binutils.url' "$LOCK_FILE")
binutils_sha=$(jq -er '.sources.binutils.sha256' "$LOCK_FILE")

if [[ "$print_lock" -eq 1 ]]; then
  cat <<EOF
target=$target
gnu_target_triple=$gnu_triple
clang_target_triple=$clang_triple
gcc_version=$gcc_version
binutils_version=$binutils_version
enable_languages=c,fortran
gcc_sha256=$gcc_sha
binutils_sha256=$binutils_sha
EOF
  exit 0
fi

# Acquisition mode: the default consumes the immutable prebuilt snapshot
# pinned at targets[$t].prebuilt; --acquire-only stops right after unpacking
# that same snapshot; --from-source forces the full fetch/build path used by
# the freebsd-sdk-release.yml publisher.
if [[ "$acquire_only" -eq 1 && "$from_source" -eq 1 ]]; then
  die "--acquire-only consumes the locked prebuilt snapshot and cannot be combined with --from-source"
fi
if [[ "$from_source" -eq 1 ]]; then
  sdk_mode="from-source"
else
  prebuilt_url=$(jq -er --arg t "$target" '.targets[$t].prebuilt.url' "$LOCK_FILE") ||
    die "lock has no prebuilt snapshot for $target; run the freebsd-sdk-release workflow first or pass --from-source"
  prebuilt_sha256=$(jq -er --arg t "$target" '.targets[$t].prebuilt.sha256' "$LOCK_FILE") ||
    die "lock prebuilt snapshot for $target is missing sha256"
  sdk_mode="prebuilt"
fi

[[ -n "$prefix" ]] || {
  usage
  die "--prefix is required"
}
[[ -z "$work_dir" ]] && work_dir="$ECS_REPO_ROOT/.ci/gnu-sdk-work"

# acquire-only never compiles: it only fetches, verifies and unpacks, so the
# build toolchain commands are not required in that mode. file/readelf/python3
# serve the probe and slimming verification machinery; strip is the host debug
# stripper of the stage-2 pass.
if [[ "$acquire_only" -eq 1 ]]; then
  required_commands=(curl sha256sum tar)
else
  required_commands=(curl sha256sum tar make gcc g++ flex bison file strip readelf python3)
fi
for cmd in "${required_commands[@]}"; do
  command -v "$cmd" >/dev/null 2>&1 || die "required command is missing: $cmd"
done
if ! command -v makeinfo >/dev/null 2>&1; then
  export MAKEINFO=true
fi

export LC_ALL=C
export TZ=UTC
# Pin the consumer-build environment so the A/B comparison (strip-before vs
# strip-after SDK) can never differ because of a locale/timezone change or a
# different default SOURCE_DATE_EPOCH (build_tools_freebsd_gnu.sh shares it).
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-946684800}"
export PATH="$prefix/bin:$PATH"

mkdir -p "$work_dir"
sysroot="$work_dir/sysroot"

fetch_verify() {
  local url=$1 sha=$2 dest=$3
  if [[ -s "$dest" && "$(sha256sum "$dest" | awk '{print $1}')" == "$sha" ]]; then
    return 0
  fi
  rm -f "$dest"
  echo "freebsd-gnu-openmp-sdk: downloading $(basename "$dest")" >&2
  curl -fL --retry 4 --retry-delay 2 --connect-timeout 30 -o "$dest" "$url"
  local actual
  actual=$(sha256sum "$dest" | awk '{print $1}')
  [[ "$actual" == "$sha" ]] || die "SHA256 mismatch for $url expected=$sha actual=$actual"
}

echo "freebsd-gnu-openmp-sdk: target=$target triple=$gnu_triple gcc=$gcc_version binutils=$binutils_version" >&2
echo "freebsd-gnu-openmp-sdk: mode=$sdk_mode acquire_only=$acquire_only" >&2

# ---------------------------------------------------------------------------
# Stage 2 machinery (from-source publisher only); the stage 3/4/5 helpers
# (driver_chain_check, feature_inventory, slim_remove_docs) follow it.
#
# Host ELF selection is byte identity, never a filename whitelist: an ELF
# whose e_machine == EM_X86_64 (0x3E) and OS/ABI != FreeBSD (9). Both SDK
# snapshots are produced on ubuntu-24.04 amd64 hosts, so every host-side
# binary (drivers, cc1/f951/lto1, binutils, shared host libs) carries this
# identity; FreeBSD target ELFs never match (amd64 target crt carries
# OS/ABI == 9; the arm64 target toolchain carries e_machine == 0xB7).
#
# The strip pass is `strip --strip-debug` (never --strip-all or
# --strip-unneeded) and it is inode-safe: one physical (st_dev, st_ino)
# object is stripped once and the stripped bytes are written back through
# the original path, so hardlink aliases keep sharing one inode with the
# new content (contract 2.5: no double processing, no orphaned alias).
#
# Proofs (contract 2.6/2.7): per stripped ELF the runtime-relevant view
# (ELF identity, full program-header view, every SHF_ALLOC section incl.
# content SHA-256) must be identical with .debug_* gone; the whole tree is
# manifested before/after and every regular file outside the host-ELF set
# must stay byte-identical with unchanged inode.
# ---------------------------------------------------------------------------

write_phase2_python() {
  local dest=$1
  cat >"$dest" <<'SDK_PHASE2_PY'
#!/usr/bin/env python3
"""Phase 2 SDK host-debug-strip support (REQUIREMENTS.md stage 2).

Host ELF identification is byte identity, never a filename whitelist: an ELF
whose e_machine == EM_X86_64 (0x3E = 62) and OS/ABI != FreeBSD (9). Both SDK
snapshots are produced on ubuntu-24.04 amd64 hosts, so every host-side tool
binary (gcc/gfortran drivers, cc1/f951, binutils, shared host libs)
carries this identity. FreeBSD target ELFs never match: amd64 target crt
objects carry OS/ABI == 9 and the arm64 target toolchain carries
e_machine == 0xB7 (AArch64).

Subcommands:
  manifest <root> <out.tsv>
      Full-tree manifest of every regular file: relpath, "dev:ino" identity,
      sha256, ELF-identity kind, summed .debug_* section bytes, file size.
      Symlinks are recorded (they are never processed); anything else that is
      neither regular file nor symlink is a hard error.
  host-elfs <manifest.tsv> <out.tsv>
      Contract 2.5 inode deduplication: one representative path per unique
      (dev,ino) host-ELF inode plus its full alias list.
  verify-tree <before.tsv> <after.tsv> <host-elfs.tsv>
      Contract 2.6/2.7 verification: same path set, hardlink groups intact,
      every file keeps its inode and kind, every non-host-ELF regular file is
      byte-identical, every host ELF lost all .debug_* sections.
  capture <bin> <out.json>
      Runtime-relevant ELF view (Phase 1 elfmeta semantics, extended): ELF
      header identity, DT_NEEDED list (compared for EQUALITY -- host ELFs
      legitimately link host libs, unlike the static Phase 1 tools), the full
      `readelf -lW` program-header view, every PT_LOAD field, and every
      SHF_ALLOC section (flags/size/address/offset/content SHA-256; NOBITS
      and zero-size sections per Phase 1 semantics). Also records the
      .debug_* inventory.
  compare <before.json> <after.json> <label>
      Fail unless the runtime-relevant views are identical.
  evidence <target> <dir> <out.json>
      Assemble the phase 2 evidence summary from the manifest pair, the host
      inode list and the A/B results file.
"""

import hashlib
import json
import os
import re
import stat as stat_mod
import struct
import subprocess
import sys
from collections import defaultdict

EM_X86_64 = 62          # 0x3E
OSABI_FREEBSD = 9

SECTION_LINE = re.compile(r"^\s*\[\s*(\d+)\]\s*(.*)$")


def die(msg):
    raise SystemExit("sdk-phase2: " + msg)


def run(args):
    proc = subprocess.run(args, check=True, capture_output=True, text=True,
                          env=dict(os.environ, LC_ALL="C"))
    return proc.stdout


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def elf_kind(head):
    if head[:4] != b"\x7fELF":
        return None
    osabi = head[7]
    e_type, e_machine = struct.unpack_from("<HH", head, 16)
    return "elf:m%d:t%d:o%d" % (e_machine, e_type, osabi)


def is_host_kind(kind):
    if kind is None:
        return False
    m = re.fullmatch(r"elf:m(\d+):t(\d+):o(\d+)", kind)
    return bool(m and int(m.group(1)) == EM_X86_64
                and int(m.group(3)) != OSABI_FREEBSD)


def parse_sections(readelf_sw):
    """Right-anchored parse of `readelf -SW` rows (Phase 0 baseline method)."""
    out = []
    for line in readelf_sw.splitlines():
        m = SECTION_LINE.match(line)
        if not m:
            continue
        nr = int(m.group(1))
        toks = m.group(2).split()
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
                die("token/type mismatch on [%d]: %s" % (nr, line))
        out.append({"nr": nr, "name": name, "type": typ,
                    "address": "0x" + addr, "offset": "0x" + off,
                    "size": int(size, 16), "flags": flg})
    return out


def debug_section_bytes(path):
    """Sum of the .debug_* section sizes (manifest/verification counter)."""
    sections = parse_sections(run(["readelf", "-SW", path]))
    return sum(sec["size"] for sec in sections
               if sec["name"].startswith(".debug_"))


def cmd_manifest(root, out_path):
    rows = []
    debug_cache = {}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for fn in sorted(filenames):
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, root)
            if "\t" in rel or "\n" in rel:
                die("path with tab/newline cannot be manifested: %r" % rel)
            st = os.lstat(path)
            if stat_mod.S_ISLNK(st.st_mode):
                rows.append((rel, "link", os.readlink(path), "symlink", "-", 0))
                continue
            if not stat_mod.S_ISREG(st.st_mode):
                die("unexpected non-regular path: %s" % path)
            with open(path, "rb") as fh:
                kind = elf_kind(fh.read(20))
            inode_id = "%d:%d" % (st.st_dev, st.st_ino)
            debug = 0
            if kind is not None:
                if inode_id not in debug_cache:
                    debug_cache[inode_id] = debug_section_bytes(path)
                debug = debug_cache[inode_id]
            rows.append((rel, inode_id, sha256_file(path),
                         kind if kind is not None else "file", str(debug),
                         st.st_size))
    rows.sort(key=lambda r: r[0])
    with open(out_path, "w") as fh:
        for rel, inode_id, sha, kind, debug, size in rows:
            fh.write("\t".join((rel, inode_id, sha, kind, debug, str(size)))
                     + "\n")
    print("manifest: %d entries -> %s" % (len(rows), out_path))


def parse_manifest(path):
    entries = {}
    with open(path) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            rel, inode_id, third, kind, debug, size = line.split("\t")
            entries[rel] = {"id": inode_id, "sha": third, "kind": kind,
                            "debug": debug, "size": int(size)}
    return entries


def cmd_host_elfs(manifest_path, out_path):
    entries = parse_manifest(manifest_path)
    groups = defaultdict(list)
    for rel, entry in entries.items():
        if is_host_kind(entry["kind"]):
            groups[entry["id"]].append(rel)
    with open(out_path, "w") as fh:
        for inode_id in sorted(groups):
            paths = sorted(groups[inode_id])
            fh.write("\t".join((paths[0], inode_id, ",".join(paths))) + "\n")
    print("host-elfs: %d paths, %d unique inodes -> %s" %
          (sum(len(v) for v in groups.values()), len(groups), out_path))


def host_alias_set(host_elfs_path):
    aliases = set()
    with open(host_elfs_path) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if line:
                aliases.update(line.split("\t")[2].split(","))
    return aliases


def cmd_verify_tree(before_path, after_path, host_elfs_path):
    before = parse_manifest(before_path)
    after = parse_manifest(after_path)
    hosts = host_alias_set(host_elfs_path)

    def hardlink_groups(entries):
        groups = defaultdict(set)
        for rel, entry in entries.items():
            if entry["id"] != "link":
                groups[entry["id"]].add(rel)
        return {k: frozenset(v) for k, v in groups.items()}

    problems = []
    for rel in sorted(set(before) - set(after)):
        problems.append("file disappeared: %s" % rel)
    for rel in sorted(set(after) - set(before)):
        problems.append("file appeared: %s" % rel)
    if hardlink_groups(before) != hardlink_groups(after):
        problems.append("hardlink group membership changed (contract 2.5)")
    missing_host = hosts - set(before)
    if missing_host:
        problems.append("host list paths not in manifest: %s" %
                        sorted(missing_host)[:5])
    manifest_hosts = {rel for rel, entry in before.items()
                      if is_host_kind(entry["kind"])}
    if manifest_hosts != hosts:
        problems.append("host-ELF set mismatch: manifest-only=%s list-only=%s"
                        % (sorted(manifest_hosts - hosts)[:5],
                           sorted(hosts - manifest_hosts)[:5]))
    for rel in sorted(set(before) & set(after)):
        b, a = before[rel], after[rel]
        if b["id"] != a["id"]:
            problems.append("inode changed for %s: %s -> %s"
                            % (rel, b["id"], a["id"]))
            continue
        if b["kind"] != a["kind"]:
            problems.append("kind changed for %s: %s -> %s"
                            % (rel, b["kind"], a["kind"]))
            continue
        if rel in hosts:
            if int(a["debug"]) != 0:
                problems.append("host ELF %s still has %s .debug_* bytes"
                                % (rel, a["debug"]))
            if a["sha"] == b["sha"] and int(b["debug"]) > 0:
                problems.append("host ELF %s had %s .debug_* bytes but is "
                                "byte-identical after strip"
                                % (rel, b["debug"]))
        elif b["sha"] != a["sha"]:
            problems.append("non-host file changed: %s" % rel)

    if problems:
        for problem in problems[:50]:
            print("verify-tree FAIL: %s" % problem)
        die("tree verification failed with %d problem(s)" % len(problems))
    debug_before = sum(int(e["debug"]) for e in before.values()
                       if e["debug"] != "-")
    debug_after = sum(int(e["debug"]) for e in after.values()
                      if e["debug"] != "-")
    print("verify-tree OK: %d files (%d host ELF paths, %d hardlink groups); "
          ".debug_* bytes %d -> %d; all non-host files byte-identical" %
          (len(before), len(hosts),
           len(hardlink_groups(before)), debug_before, debug_after))


# --------------------------------------------------------------------------
# elfmeta: runtime-relevant ELF view. Parsing follows the proven Phase 1
# implementation; the DT_NEEDED rule differs on purpose (equality, not
# emptiness) because host ELFs link host libraries.
# --------------------------------------------------------------------------

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
        die("readelf -h is missing fields: " + ", ".join(missing))
    return {key: fields[key] for key in keys}


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
            off, va, pa, fsz, msz = rest[:5]
            flg, aln = rest[5] + " " + rest[6], rest[7]
        else:
            die("unexpected readelf -lW LOAD row: " + line.strip())
        out.append({"offset": off, "vaddr": va, "paddr": pa,
                    "filesz": int(fsz, 16), "memsz": int(msz, 16),
                    "flags": flg, "align": aln})
    return out


def cmd_capture(bin_path, out_path):
    header = elf_header_fields(run(["readelf", "-h", bin_path]))
    dyn = run(["readelf", "-d", bin_path])
    needed = re.findall(r"\(NEEDED\)\s+Shared library: \[(.*?)\]", dyn)
    lw = run(["readelf", "-lW", bin_path])
    # Full program-header view: the phdr table and the section-to-segment
    # mapping carry the load-time semantics; compare them verbatim.
    phdr_lines = [re.sub(r"\s+", " ", line.strip())
                  for line in lw.splitlines() if line.strip()]
    loads = load_segments(lw)
    if not loads:
        die("no PT_LOAD program headers parsed in " + bin_path)
    sections = parse_sections(run(["readelf", "-SW", bin_path]))
    file_size = os.path.getsize(bin_path)
    with open(bin_path, "rb") as fh:
        data = fh.read()
    if len(data) != file_size:
        die("file changed while reading: " + bin_path)
    allocs = []
    debug = []
    for sec in sections:
        if sec["name"].startswith(".debug_"):
            debug.append([sec["name"], sec["size"]])
        if "A" not in sec["flags"]:
            continue
        rec = {key: sec[key]
               for key in ("name", "type", "flags", "address", "offset",
                           "size")}
        if sec["type"] == "NOBITS":
            rec["content_sha256"] = None
        else:
            off = int(sec["offset"], 16)
            if off + sec["size"] > file_size:
                die("section %s out of file bounds in %s" %
                    (sec["name"], bin_path))
            rec["content_sha256"] = hashlib.sha256(
                data[off:off + sec["size"]]).hexdigest()
        allocs.append(rec)
    if not allocs or not any(sec["name"] == ".text" for sec in allocs):
        die("implausible SHF_ALLOC set parsed in " + bin_path)
    record = {"file": bin_path, "file_size": file_size,
              "elf_header": header, "dt_needed": needed,
              "phdr_lines": phdr_lines, "load_segments": loads,
              "alloc_sections": allocs, "debug_sections": debug}
    with open(out_path, "w") as fh:
        json.dump(record, fh, indent=1, sort_keys=True)
    print("elfmeta: captured %s (%d PT_LOAD, %d SHF_ALLOC sections, %d "
          "debug sections)" % (bin_path, len(loads), len(allocs), len(debug)))


def cmd_compare(before_path, after_path, label):
    with open(before_path) as fh:
        before = json.load(fh)
    with open(after_path) as fh:
        after = json.load(fh)
    problems = []
    for key in sorted(set(before["elf_header"]) | set(after["elf_header"])):
        if before["elf_header"].get(key) != after["elf_header"].get(key):
            problems.append("elf_header %s: before=%r after=%r" %
                            (key, before["elf_header"].get(key),
                             after["elf_header"].get(key)))
    if before["dt_needed"] != after["dt_needed"]:
        problems.append("dt_needed: before=%r after=%r" %
                        (before["dt_needed"], after["dt_needed"]))
    if before["phdr_lines"] != after["phdr_lines"]:
        b_only = [l for l in before["phdr_lines"]
                  if l not in after["phdr_lines"]]
        a_only = [l for l in after["phdr_lines"]
                  if l not in before["phdr_lines"]]
        problems.append("program headers changed: before-only=%r after-only=%r"
                        % (b_only[:4], a_only[:4]))
    b_loads, a_loads = before["load_segments"], after["load_segments"]
    if len(b_loads) != len(a_loads):
        problems.append("PT_LOAD count: before=%d after=%d" %
                        (len(b_loads), len(a_loads)))
    else:
        for index, (b_seg, a_seg) in enumerate(zip(b_loads, a_loads)):
            for key in sorted(set(b_seg) | set(a_seg)):
                if b_seg.get(key) != a_seg.get(key):
                    problems.append("PT_LOAD[%d] %s: before=%r after=%r" %
                                    (index, key, b_seg.get(key),
                                     a_seg.get(key)))
    b_secs = {sec["name"]: sec for sec in before["alloc_sections"]}
    a_secs = {sec["name"]: sec for sec in after["alloc_sections"]}
    for name in sorted(set(b_secs) - set(a_secs)):
        problems.append("SHF_ALLOC section vanished: %s" % name)
    for name in sorted(set(a_secs) - set(b_secs)):
        problems.append("SHF_ALLOC section appeared: %s" % name)
    for name in sorted(set(b_secs) & set(a_secs)):
        b, a = b_secs[name], a_secs[name]
        # Phase 1 semantics: content-bearing SHF_ALLOC sections compare
        # flags/size/address/offset/content; NOBITS compares size/address
        # only; zero-size sections skip the meaningless file offset.
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
        for problem in problems[:50]:
            print("runtime-view MISMATCH (%s): %s" % (label, problem))
        die("runtime view comparison failed for %s: %d problem(s)" %
            (label, len(problems)))
    print("runtime-view OK (%s): %d PT_LOAD segments, %d SHF_ALLOC sections "
          "identical; .debug_* bytes %d -> %d" %
          (label, len(b_loads), len(b_secs),
           sum(size for _, size in before["debug_sections"]),
           sum(size for _, size in after["debug_sections"])))


def cmd_evidence(target, directory, out_path):
    before = parse_manifest(os.path.join(directory, "manifest.before.tsv"))
    after = parse_manifest(os.path.join(directory, "manifest.after.tsv"))
    host_paths = host_alias_set(
        os.path.join(directory, "host-elf-inodes.tsv"))
    groups = defaultdict(list)
    for rel, entry in before.items():
        if entry["id"] != "link":
            groups[entry["id"]].append(rel)
    hardlink_groups = {k: v for k, v in groups.items() if len(v) > 1}

    def tree_bytes(entries, unique_inodes):
        if unique_inodes:
            seen = set()
            total = 0
            for rel, entry in entries.items():
                if entry["id"] in seen or entry["id"] == "link":
                    continue
                seen.add(entry["id"])
                total += entry["size"]
            return total
        return sum(entry["size"] for entry in entries.values())

    ab = []
    ab_path = os.path.join(directory, "ab-results.tsv")
    if os.path.exists(ab_path):
        with open(ab_path) as fh:
            for line in fh:
                line = line.rstrip("\n")
                if not line:
                    continue
                name, alloc_equal, sha_equal = line.split("\t")
                ab.append({"name": name,
                           "alloc_sections_equal": alloc_equal,
                           "unified_strip_sha256_equal": sha_equal})
    evidence = {
        "target": target,
        "strip": {
            "command": "strip --strip-debug (host GNU strip)",
            "host_selection": "e_machine==x86-64 && OS/ABI!=FreeBSD "
                              "(byte identity, inode-deduplicated)",
            "host_elf_paths": len(host_paths),
            "host_elf_unique_inodes": len(
                {entry["id"] for rel, entry in before.items()
                 if rel in host_paths}),
            "hardlink_group_count": len(hardlink_groups),
            "debug_bytes_before": sum(int(e["debug"])
                                      for e in before.values()
                                      if e["debug"] != "-"),
            "debug_bytes_after": sum(int(e["debug"])
                                     for e in after.values()
                                     if e["debug"] != "-"),
            "tree_bytes_unique_inodes_before":
                tree_bytes(before, unique_inodes=True),
            "tree_bytes_unique_inodes_after":
                tree_bytes(after, unique_inodes=True),
            "tree_bytes_all_paths_before":
                tree_bytes(before, unique_inodes=False),
            "tree_bytes_all_paths_after":
                tree_bytes(after, unique_inodes=False),
            "non_host_files_unchanged": True,
            "hardlink_groups_intact": True,
        },
        "ab_equivalence": ab,
        "consumer_builds": {"a_side_pre_strip": "pass",
                            "b_side_post_strip": "pass"},
    }
    with open(out_path, "w") as fh:
        json.dump(evidence, fh, indent=1)
        fh.write("\n")
    print("evidence: %s" % out_path)


def main(argv):
    if len(argv) == 4 and argv[1] == "manifest":
        cmd_manifest(argv[2], argv[3])
    elif len(argv) == 4 and argv[1] == "host-elfs":
        cmd_host_elfs(argv[2], argv[3])
    elif len(argv) == 5 and argv[1] == "verify-tree":
        cmd_verify_tree(argv[2], argv[3], argv[4])
    elif len(argv) == 4 and argv[1] == "capture":
        cmd_capture(argv[2], argv[3])
    elif len(argv) == 5 and argv[1] == "compare":
        cmd_compare(argv[2], argv[3], argv[4])
    elif len(argv) == 5 and argv[1] == "evidence":
        cmd_evidence(argv[2], argv[3], argv[4])
    else:
        raise SystemExit(__doc__)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
SDK_PHASE2_PY
}

# Consumer builds (contract 2.9): the real NPB EP/FT Class A + STREAM builds
# through the unmodified Stage 5 builder. A side runs against the pre-strip
# SDK, B side against the final post-strip SDK, in the same job/environment.
phase2_consumer_build() {
  local side=$1
  bash "$ECS_REPO_ROOT/scripts/build_tools_freebsd_gnu.sh" \
    --target "$target" \
    --stage-root "$evidence_dir/$side-stage" \
    --sdk-prefix "$prefix"
}

phase2_collect_outputs() {
  local side=$1
  mkdir -p "$evidence_dir/outputs/$side-npb"
  cp -p "$evidence_dir/$side-stage/$target/bin/npb-ep" \
        "$evidence_dir/$side-stage/$target/bin/npb-ft" \
        "$evidence_dir/$side-stage/$target/bin/stream" \
        "$evidence_dir/outputs/$side-npb/"
}

phase2_compare_output() {
  # One A/B output pair: capture both runtime views and require equality
  # (the gate — any SHF_ALLOC machine-code difference fails the run), then
  # record whether a final uniform --strip-unneeded pass with the SDK's own
  # target strip also makes the whole files byte-identical (contract 2.10
  # "ideal requirement"; informational, non-gating).
  local label=$1 a=$2 b=$3
  [[ -s "$a" && -s "$b" ]] || die "phase2: missing A/B output for $label"
  python3 "$phase2_py" capture "$a" "$evidence_dir/ab/$label.a.json"
  python3 "$phase2_py" capture "$b" "$evidence_dir/ab/$label.b.json"
  python3 "$phase2_py" compare "$evidence_dir/ab/$label.a.json" \
    "$evidence_dir/ab/$label.b.json" "$label"
  local sa="$evidence_dir/ab/$label.a.stripped"
  local sb="$evidence_dir/ab/$label.b.stripped"
  cp "$a" "$sa"
  cp "$b" "$sb"
  "$phase2_target_strip" --strip-unneeded "$sa" "$sb"
  local sha_a sha_b
  sha_a=$(sha256sum "$sa" | awk '{print $1}')
  sha_b=$(sha256sum "$sb" | awk '{print $1}')
  if [[ "$sha_a" == "$sha_b" ]]; then
    printf '%s\tidentical\tidentical\n' "$label" >>"$evidence_dir/ab-results.tsv"
    echo "ab-equivalence: $label unified-strip sha256(A)==sha256(B)" >&2
  else
    printf '%s\tidentical\tdiffer\n' "$label" >>"$evidence_dir/ab-results.tsv"
    echo "ab-equivalence: $label unified-strip sha256 differs (alloc sections equal; recorded)" >&2
  fi
  rm -f "$sa" "$sb"
}

phase2_ab_compare() {
  mkdir -p "$evidence_dir/ab"
  : >"$evidence_dir/ab-results.tsv"
  phase2_target_strip="$prefix/bin/${gnu_triple}-strip"
  [[ -x "$phase2_target_strip" ]] ||
    die "phase2: missing target strip in the SDK: $phase2_target_strip"
  local label
  for label in hello-c hello-f ieee omp-c omp-f; do
    phase2_compare_output "probe-$label" \
      "$evidence_dir/outputs/a-probes/$label" \
      "$evidence_dir/outputs/b-probes/$label"
  done
  for label in npb-ep npb-ft stream; do
    phase2_compare_output "bench-$label" \
      "$evidence_dir/outputs/a-npb/$label" \
      "$evidence_dir/outputs/b-npb/$label"
  done
}

phase2_strip_and_verify() {
  local ev="$evidence_dir/elfmeta"
  mkdir -p "$ev"
  local index=0 rep inode_id paths path tmp name
  while IFS=$'\t' read -r rep inode_id paths; do
    index=$((index + 1))
    name=$(printf '%03d' "$index")
    path="$prefix/$rep"
    python3 "$phase2_py" capture "$path" "$ev/$name.before.json"
    tmp="$path.ecs-strip-tmp"
    if ! strip --strip-debug -o "$tmp" "$path"; then
      rm -f "$tmp"
      die "phase2: strip --strip-debug failed for $rep"
    fi
    # Write the stripped bytes back through the original path: the inode
    # (and therefore every hardlink alias of this object) keeps its
    # identity and picks up the stripped content in place.
    cat "$tmp" >"$path"
    rm -f "$tmp"
    python3 "$phase2_py" capture "$path" "$ev/$name.after.json"
    python3 "$phase2_py" compare "$ev/$name.before.json" \
      "$ev/$name.after.json" "$rep"
  done <"$evidence_dir/host-elf-inodes.tsv"
  python3 "$phase2_py" manifest "$prefix" "$evidence_dir/manifest.after.tsv"
  python3 "$phase2_py" verify-tree "$evidence_dir/manifest.before.tsv" \
    "$evidence_dir/manifest.after.tsv" "$evidence_dir/host-elf-inodes.tsv"
}

# Stage 3 (contract 3.4): the drivers must resolve a complete internal
# toolchain on the final SDK bytes. Every cc1/f951/collect2/as/ld/plugin/
# wrapper path the driver prints for -### must exist on disk, and any
# "cannot find / cannot load / missing lto" report is a hard failure (a
# --disable-lto build must not reference absent LTO components). -### only
# prints the resolved specs and executes nothing, so this adds seconds.
chain_case() {
  local name=$1
  shift
  local log="chain-$name.log"
  if ! "$@" >"$log" 2>&1; then
    sed -n '1,40p' "$log" >&2
    die "driver chain ($name): driver exited non-zero (contract 3.4)"
  fi
  if grep -Eqi 'cannot find|cannot load|missing lto' "$log"; then
    grep -Ei 'cannot find|cannot load|missing lto' "$log" | head -5 >&2
    die "driver chain ($name): unresolved component reference (contract 3.4)"
  fi
  local tok base cand problems=0
  while IFS= read -r tok; do
    tok=${tok//\"/}
    [[ "$tok" == */* ]] || continue
    base=${tok##*/}
    case "$base" in
      cc1 | f951 | collect2 | as | ld | lto1 | lto-wrapper | lto-dump | liblto_plugin.so | liblto_plugin.la)
        cand="/${tok#*/}"
        if [[ ! -e "$cand" ]]; then
          echo "driver chain ($name): referenced tool does not exist: $cand" >&2
          problems=$((problems + 1))
        fi
        ;;
    esac
  done < <(tr ' ' '\n' <"$log")
  [[ "$problems" -eq 0 ]] ||
    die "driver chain ($name): $problems referenced tool(s) missing (contract 3.4)"
  echo "freebsd-gnu-openmp-sdk: [stage3] driver chain OK: $name" >&2
}

driver_chain_check() {
  # Runs inside the probe output dir (the probe sources are already there)
  # and keeps the five -### spec logs next to the probe binaries; the logs
  # land inside the evidence dir and are uploaded with it.
  local out_dir=$1
  (
    cd "$out_dir"
    chain_case c-compile "$gcc_bin" -### --sysroot="$sysroot" -c hello.c
    chain_case c-static-link "$gcc_bin" -### --sysroot="$sysroot" -static hello.c -o chain-c
    chain_case f-static-link "$gfortran_bin" -### --sysroot="$sysroot" -static hello.f90 -o chain-f
    chain_case c-openmp-link "$gcc_bin" -### --sysroot="$sysroot" -fopenmp omp.c -o chain-omp-c
    chain_case f-openmp-link "$gfortran_bin" -### --sysroot="$sysroot" -fopenmp omp.f90 -o chain-omp-f
  )
  # The target assembler/linker are always part of the SDK contract (0.3).
  [[ -x "$prefix/bin/${gnu_triple}-as" ]] ||
    die "driver chain: missing $prefix/bin/${gnu_triple}-as (contract 3.4)"
  [[ -x "$prefix/bin/${gnu_triple}-ld" ]] ||
    die "driver chain: missing $prefix/bin/${gnu_triple}-ld (contract 3.4)"
}

# Stages 3/4 (contracts 3.3/4.3): record what the --disable-lto and
# --disable-gcov builds still install. Existence is evidence only — hand
# deleting LTO or gcov components is forbidden (contract 3.2), so whatever
# GCC still installs stays in the snapshot.
feature_inventory() {
  local out=$1
  : >"$out"
  local pattern paths
  for pattern in lto1 lto-wrapper lto-dump liblto_plugin* '*gcov*'; do
    paths=$(find "$prefix" -name "$pattern" | LC_ALL=C sort)
    if [[ -n "$paths" ]]; then
      while IFS= read -r p; do
        printf '%s\tpresent\t%s\n' "$pattern" "$p"
      done <<<"$paths" >>"$out"
    else
      printf '%s\tabsent\t-\n' "$pattern" >>"$out"
    fi
  done
  cat "$out" >&2
}

# Stage 5 (contract 5.2): remove exactly $prefix/share/man and
# $prefix/share/info from the snapshot. The full share/ listings before and
# after prove the removal stayed inside the two whitelisted directories.
slim_remove_docs() {
  if [[ -d "$prefix/share" ]]; then
    (cd "$prefix" && find share -mindepth 1 | LC_ALL=C sort) \
      >"$evidence_dir/share-before.txt"
  else
    : >"$evidence_dir/share-before.txt"
  fi
  rm -rf "$prefix/share/man" "$prefix/share/info"
  if [[ -d "$prefix/share" ]]; then
    (cd "$prefix" && find share -mindepth 1 | LC_ALL=C sort) \
      >"$evidence_dir/share-after.txt"
  else
    : >"$evidence_dir/share-after.txt"
  fi
  echo "freebsd-gnu-openmp-sdk: [stage5] share/ entries $(wc -l <"$evidence_dir/share-before.txt") -> $(wc -l <"$evidence_dir/share-after.txt") (removed share/man + share/info only)" >&2
}

# The five release probes, run against the given output directory. Always
# executed with the full assertion set; in from-source mode they run again
# on the post-strip bytes so the gate proves the published snapshot.
run_probes() {
  local out_dir=$1
  rm -rf "$out_dir"
  mkdir -p "$out_dir"
  cd "$out_dir"

  cat >hello.c <<'EOF'
#include <stdio.h>
int main(void) {
    puts("gnu-c-ok");
    return 0;
}
EOF
  # The snapshot bakes the publisher's sysroot path (configure-time
  # --with-sysroot); pin the freshly installed sysroot explicitly — the same
  # contract the Stage 5 builder wrappers apply — so consumption never depends
  # on where the snapshot was produced.
  "$gcc_bin" --sysroot="$sysroot" -static -o hello-c hello.c
  file_out=$(file hello-c)
  echo "C hello: $file_out" >&2
  case "$file_out" in
    *FreeBSD*) ;;
    *) die "C hello is not a FreeBSD ELF: $file_out" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) die "C hello is not static: $file_out" ;;
  esac

  cat >hello.f90 <<'EOF'
program hello
  print *, 'gnu-fortran-ok'
end program hello
EOF
  "$gfortran_bin" --sysroot="$sysroot" -static -o hello-f hello.f90
  file_out=$(file hello-f)
  echo "Fortran hello: $file_out" >&2
  case "$file_out" in
    *FreeBSD*) ;;
    *) die "Fortran hello is not a FreeBSD ELF: $file_out" ;;
  esac
  case "$file_out" in
    *static* | *statically*) ;;
    *) die "Fortran hello is not static: $file_out" ;;
  esac

  cat >ieee.f90 <<'EOF'
program ieee_check
  use, intrinsic :: ieee_arithmetic
  print *, ieee_support_nan(1.0)
end program ieee_check
EOF
  "$gfortran_bin" --sysroot="$sysroot" -static -o ieee ieee.f90
  find "$prefix" -name 'ieee_arithmetic.mod' | grep -q . ||
    die "ieee_arithmetic.mod was not installed"

  cat >omp.c <<'EOF'
#include <stdio.h>
#ifdef _OPENMP
#include <omp.h>
#endif
int main(void) {
#ifdef _OPENMP
    int n = 0;
#pragma omp parallel
    {
#pragma omp atomic
        n += 1;
    }
    printf("c-openmp-threads=%d\n", n);
    return n > 0 ? 0 : 1;
#else
    puts("openmp-not-enabled");
    return 1;
#endif
}
EOF
  "$gcc_bin" --sysroot="$sysroot" -static -fopenmp -o omp-c omp.c
  file_out=$(file omp-c)
  case "$file_out" in
    *FreeBSD*static* | *static*FreeBSD*) ;;
    *FreeBSD*)
      case "$file_out" in
        *static* | *statically*) ;;
        *) die "C OpenMP hello is not static: $file_out" ;;
      esac
      ;;
    *) die "C OpenMP hello is not a FreeBSD ELF: $file_out" ;;
  esac

  cat >omp.f90 <<'EOF'
program ompf
  use omp_lib
  integer :: n
  n = 0
!$omp parallel
!$omp atomic
  n = n + 1
!$omp end parallel
  print *, 'fortran-openmp-threads=', n
end program ompf
EOF
  "$gfortran_bin" --sysroot="$sysroot" -static -fopenmp -o omp-f omp.f90
  file_out=$(file omp-f)
  case "$file_out" in
    *FreeBSD*)
      case "$file_out" in
        *static* | *statically*) ;;
        *) die "Fortran OpenMP hello is not static: $file_out" ;;
      esac
      ;;
    *) die "Fortran OpenMP hello is not a FreeBSD ELF: $file_out" ;;
  esac
}

# Acquire the prebuilt snapshot: download from the lock-pinned URL, verify the
# pinned SHA256, unpack into the prefix and drop any leftover cache marker.
# Shared by the default prebuilt mode (which continues with the assertion and
# probe suite) and by --acquire-only (which exits right after unpacking: the
# gate job has already probed these exact bytes, pinned by the same SHA256).
acquire_prebuilt() {
  local sdk_tarball="$work_dir/$(basename "$prebuilt_url")"
  fetch_verify "$prebuilt_url" "$prebuilt_sha256" "$sdk_tarball"
  echo "freebsd-gnu-openmp-sdk: unpacking prebuilt SDK snapshot into $prefix" >&2
  tar -xaf "$sdk_tarball" -C "$prefix" --strip-components=1
  # Older snapshots still carry the marker of the retired cache mechanism;
  # it is not part of the SDK contract.
  rm -f "$prefix/.ecs-gnu-sdk-cache.id"
}

rm -rf "$prefix"
mkdir -p "$prefix"
if [[ "$sdk_mode" == "from-source" ]]; then
  src_root="$work_dir/src"
  build_root="$work_dir/build"
  mkdir -p "$src_root" "$build_root"
  # Batch evidence root (stages 2-5). Everything the release proves about
  # the slimming (manifests, per-ELF captures, A/B outputs, driver chain
  # logs, LTO/gcov inventory, share/ listings and the summary) is collected
  # here and uploaded as its own artifact by freebsd-sdk-release.yml.
  evidence_dir="$work_dir/slim-evidence"
  rm -rf "$evidence_dir"
  mkdir -p "$evidence_dir"
  write_phase2_python "$evidence_dir/sdk_phase2.py"
  phase2_py="$evidence_dir/sdk_phase2.py"
fi

if [[ "$acquire_only" -eq 1 ]]; then
  acquire_prebuilt
  echo "freebsd-gnu-openmp-sdk: $target prebuilt SDK acquired at $prefix (acquire-only: no probes, no build)" >&2
  exit 0
fi

echo "freebsd-gnu-openmp-sdk: installing FreeBSD sysroot" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" \
  --target "$target" \
  --sysroot-dir "$sysroot" \
  --work-dir "$work_dir/sysroot-work"

if [[ "$sdk_mode" == "prebuilt" ]]; then
  acquire_prebuilt
else
  fetch_verify "$binutils_url" "$binutils_sha" "$src_root/binutils-$binutils_version.tar.xz"
  fetch_verify "$gcc_url" "$gcc_sha" "$src_root/gcc-$gcc_version.tar.xz"

  if [[ ! -d "$src_root/binutils-$binutils_version" ]]; then
    tar -xJf "$src_root/binutils-$binutils_version.tar.xz" -C "$src_root"
  fi
  if [[ ! -d "$src_root/gcc-$gcc_version" ]]; then
    tar -xJf "$src_root/gcc-$gcc_version.tar.xz" -C "$src_root"
  fi

  # Use host libgmp/libmpfr/libmpc. Do not download GCC prerequisites (slow
  # and unnecessary when Ubuntu packages are present).

  echo "freebsd-gnu-openmp-sdk: building binutils" >&2
  mkdir -p "$build_root/binutils"
  (
    cd "$build_root/binutils"
    MAKEINFO=true "$src_root/binutils-$binutils_version/configure" \
      --target="$gnu_triple" \
      --prefix="$prefix" \
      --with-sysroot="$sysroot" \
      --disable-nls \
      --disable-werror \
      --disable-multilib \
      --with-native-system-header-dir=/include
    MAKEINFO=true make -j"$jobs"
    MAKEINFO=true make install
  )

  echo "freebsd-gnu-openmp-sdk: building gcc (c,fortran only)" >&2
  mkdir -p "$build_root/gcc"
  (
    cd "$build_root/gcc"
    "$src_root/gcc-$gcc_version/configure" \
      --target="$gnu_triple" \
      --prefix="$prefix" \
      --with-sysroot="$sysroot" \
      --with-native-system-header-dir=/usr/include \
      --enable-languages=c,fortran \
      --disable-bootstrap \
      --disable-lto \
      --disable-gcov \
      --disable-multilib \
      --disable-nls \
      --disable-shared \
      --enable-static \
      --disable-libstdcxx \
      --disable-libatomic \
      --disable-libitm \
      --disable-libsanitizer \
      --disable-libvtv \
      --disable-libssp \
      --without-isl \
      --with-gmp \
      --with-mpfr \
      --with-mpc
    # Full all/install builds every configured target lib. GCC gates
    # libquadmath on a per-target __float128 probe (BUILD_LIBQUADMATH): the
    # probe fails on aarch64, so upstream does not build libquadmath for
    # arm64 and its all/install are no-ops there. languages=c,fortran keeps
    # libstdc++ out of the graph.
    MAKEINFO=true make -j"$jobs" all
    MAKEINFO=true make install
  )

  # Stage 5 (contract 5.2): drop exactly the two documentation directories
  # from the CI snapshot. Nothing else under share/ is touched; the
  # before/after listings are part of the release evidence.
  slim_remove_docs
fi

gcc_bin="$prefix/bin/${gnu_triple}-gcc"
gfortran_bin="$prefix/bin/${gnu_triple}-gfortran"
[[ -x "$gcc_bin" ]] || die "missing $gcc_bin"
[[ -x "$gfortran_bin" ]] || die "missing $gfortran_bin"

# Forbidden: g++ and libstdc++
if [[ -x "$prefix/bin/${gnu_triple}-g++" ]]; then
  die "g++ must not be installed"
fi
if find "$prefix" -name 'libstdc++.a' | grep -q .; then
  die "libstdc++.a must not be installed"
fi
if find "$prefix" -name 'libstdc++.so*' | grep -q .; then
  die "libstdc++.so must not be installed"
fi

# Required runtime static libs, per target: GCC's per-target BUILD_LIBQUADMATH
# probe fails on aarch64, so upstream never builds libquadmath for arm64.
# Also search the broader prefix because libgcc/libgfortran may install elsewhere.
required_libs=$(jq -er --arg t "$target" '.targets[$t].required_libraries[]' "$LOCK_FILE") ||
  die "lock has no required_libraries for target: $target"
while IFS= read -r lib; do
  if ! find "$prefix" -name "$lib" | grep -q .; then
    die "required runtime library missing: $lib"
  fi
done <<<"$required_libs"

if [[ "$sdk_mode" == "from-source" ]]; then
  # A side (contract 2.10): probes and the full consumer workload against
  # the unstripped SDK, identical environment (LC_ALL/TZ/SOURCE_DATE_EPOCH
  # exported above), same sources, same flags, own stage tree.
  echo "freebsd-gnu-openmp-sdk: [stage2] A-side probes (pre-strip SDK)" >&2
  run_probes "$evidence_dir/outputs/a-probes"
  echo "freebsd-gnu-openmp-sdk: [stage2] A-side consumer build (pre-strip SDK)" >&2
  phase2_consumer_build a
  phase2_collect_outputs a

  echo "freebsd-gnu-openmp-sdk: [stage2] manifest before strip" >&2
  python3 "$phase2_py" manifest "$prefix" "$evidence_dir/manifest.before.tsv"
  python3 "$phase2_py" host-elfs "$evidence_dir/manifest.before.tsv" \
    "$evidence_dir/host-elf-inodes.tsv"

  echo "freebsd-gnu-openmp-sdk: [stage2] host ELF debug strip + invariants" >&2
  phase2_strip_and_verify

  echo "freebsd-gnu-openmp-sdk: [stage3/4] LTO/gcov feature inventory" >&2
  feature_inventory "$evidence_dir/feature-inventory.tsv"
fi

# Gate probes run on exactly the bytes that get published: prebuilt mode has
# only this run; from-source mode runs them after the strip (contract 2.8:
# slimming completes before the probes).
if [[ "$sdk_mode" == "from-source" ]]; then
  echo "freebsd-gnu-openmp-sdk: [stage2] B-side probes (post-strip SDK)" >&2
  probe_out_dir="$evidence_dir/outputs/b-probes"
  run_probes "$probe_out_dir"
else
  probe_out_dir="$work_dir/probe"
  run_probes "$probe_out_dir"
fi

# Stage 3 gate on the bytes about to be published (and, in prebuilt mode, on
# the consumed snapshot): every -###-referenced tool must exist.
driver_chain_check "$probe_out_dir"

if [[ "$sdk_mode" == "from-source" ]]; then
  echo "freebsd-gnu-openmp-sdk: [stage2] B-side consumer build (post-strip SDK)" >&2
  phase2_consumer_build b
  phase2_collect_outputs b
  echo "freebsd-gnu-openmp-sdk: [stage2] A/B compiler output equivalence" >&2
  phase2_ab_compare
  python3 "$phase2_py" evidence "$target" "$evidence_dir" "$evidence_dir/evidence.json"
fi

if [[ "$sdk_mode" == "prebuilt" ]]; then
  acquisition_fields="\"acquisition\": \"prebuilt\",
  \"url\": \"$prebuilt_url\",
  \"sha256\": \"$prebuilt_sha256\""
  # Prebuilt consumption probes the released snapshot; the snapshot's own
  # provenance already carries its slimming record, nothing new is computed.
  slim_provenance_fields=""
else
  acquisition_fields="\"acquisition\": \"from-source\""
  # Provenance must describe the real final artifact (contract 0.3):
  # from-source snapshots carry the strip record, the disabled configure
  # features and the removed documentation directories.
  slim_provenance_fields="\"host_debug_strip\": $(jq -c '.strip' "$evidence_dir/evidence.json"),
  \"configure_features\": {\"lto\": \"disabled\", \"gcov\": \"disabled\"},
  \"removed_directories\": [\"share/man\", \"share/info\"],"
fi

cat >"$prefix/sdk-provenance.json" <<EOF
{
  "target": "$target",
  "gnu_target_triple": "$gnu_triple",
  "gcc_version": "$gcc_version",
  "binutils_version": "$binutils_version",
  "enable_languages": ["c", "fortran"],
  "build_host": "ubuntu-24.04-amd64",
  "toolchain_mode": "cross",
  "openmp_runtime": "libgomp",
  "probes": ["c-static-hello", "fortran-static-hello", "ieee_arithmetic", "c-openmp", "fortran-openmp"],
  $slim_provenance_fields
  $acquisition_fields
}
EOF

echo "freebsd-gnu-openmp-sdk: $target SDK ready at $prefix" >&2
"$gcc_bin" --version | head -n1 >&2
"$gfortran_bin" --version | head -n1 >&2
