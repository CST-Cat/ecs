#!/usr/bin/env python3
"""FreeBSD GNU SDK publisher tree manifest and verification machinery.

This is intentionally a publisher-only, low-frequency verifier.  The normal
SDK orchestration invokes it after a from-source install and before publishing
the resulting bytes.

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
      sha256, ELF-identity kind, summed .debug_* section bytes, file size,
      and a fingerprint of sections that --strip-debug is not allowed to
      mutate. Symlinks are recorded (they are never processed); anything else
      that is neither regular file nor symlink is a hard error.
  host-elfs <manifest.tsv> <out.tsv>
      Contract 2.5 inode deduplication: one representative path per unique
      (dev,ino) host-ELF inode plus its full alias list.
  verify-tree <before.tsv> <after.tsv> <host-elfs.tsv>
      Contract 2.6/2.7 verification: same path set, hardlink groups intact,
      every file keeps its inode and kind, every non-host-ELF regular file is
      byte-identical, host ELF debug sections are gone, and no non-debug host
      ELF section changed.
  evidence <target> <dir> <out.json>
      Assemble the phase 2 evidence summary from the manifest pair and the
      host inode list.
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
IGNORED_STRIP_SECTIONS = frozenset((".symtab", ".strtab", ".shstrtab"))

SECTION_LINE = re.compile(r"^\s*\[\s*(\d+)\]\s*(.*)$")


def die(msg):
    raise SystemExit("sdk-tree-verify: " + msg)


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
    """Right-anchored parse of `readelf -SW` rows."""
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
                    "size": int(size, 16), "entsize": "0x" + es,
                    "flags": flg})
    return out


def sections_for(path):
    return parse_sections(run(["readelf", "-SW", path]))


def debug_section_bytes(path):
    """Sum of the .debug_* section sizes (manifest/verification counter)."""
    return sum(sec["size"] for sec in sections_for(path)
               if sec["name"].startswith(".debug_"))


def non_debug_section_fingerprint(path):
    """Fingerprint sections that --strip-debug is not allowed to mutate.

    GNU strip legitimately rewrites the symbol and section-name tables while
    removing debug sections, so those tables and .debug_* sections are omitted.
    All other section metadata and bytes remain part of the fingerprint.  This
    catches an accidental .text/.rodata/etc. mutation without rejecting the
    normal layout changes caused by deleting debug sections.
    """
    sections = sections_for(path)
    records = []
    with open(path, "rb") as fh:
        for sec in sections:
            name = sec["name"]
            if name.startswith(".debug_") or name in IGNORED_STRIP_SECTIONS:
                continue
            if sec["type"] == "NOBITS" or sec["size"] == 0:
                content = ""
            else:
                fh.seek(int(sec["offset"], 16))
                data = fh.read(sec["size"])
                if len(data) != sec["size"]:
                    die("short read for section %s in %s" % (name, path))
                content = hashlib.sha256(data).hexdigest()
            records.append({
                "name": name,
                "type": sec["type"],
                "address": sec["address"],
                "size": sec["size"],
                "entsize": sec["entsize"],
                "flags": sec["flags"],
                "content": content,
            })
    encoded = json.dumps(records, separators=(",", ":"),
                         sort_keys=True).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def cmd_manifest(root, out_path):
    rows = []
    debug_cache = {}
    section_cache = {}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for fn in sorted(filenames):
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, root)
            if "\t" in rel or "\n" in rel:
                die("path with tab/newline cannot be manifested: %r" % rel)
            st = os.lstat(path)
            if stat_mod.S_ISLNK(st.st_mode):
                rows.append((rel, "link", os.readlink(path), "symlink", "-",
                             0, "-"))
                continue
            if not stat_mod.S_ISREG(st.st_mode):
                die("unexpected non-regular path: %s" % path)
            with open(path, "rb") as fh:
                kind = elf_kind(fh.read(20))
            inode_id = "%d:%d" % (st.st_dev, st.st_ino)
            debug = "-"
            sections = "-"
            if kind is not None:
                if inode_id not in debug_cache:
                    debug_cache[inode_id] = debug_section_bytes(path)
                    section_cache[inode_id] = non_debug_section_fingerprint(path)
                debug = str(debug_cache[inode_id])
                sections = section_cache[inode_id]
            rows.append((rel, inode_id, sha256_file(path),
                         kind if kind is not None else "file", debug,
                         st.st_size, sections))
    rows.sort(key=lambda r: r[0])
    with open(out_path, "w") as fh:
        for rel, inode_id, sha, kind, debug, size, sections in rows:
            fh.write("\t".join((rel, inode_id, sha, kind, debug,
                                str(size), sections)) + "\n")
    print("manifest: %d entries -> %s" % (len(rows), out_path))


def parse_manifest(path):
    entries = {}
    with open(path) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            rel, inode_id, third, kind, debug, size, sections = line.split("\t")
            entries[rel] = {"id": inode_id, "sha": third, "kind": kind,
                            "debug": debug, "size": int(size),
                            "sections": sections}
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
            problems.append("inode changed for %s: %s -> %s" %
                            (rel, b["id"], a["id"]))
            continue
        if b["kind"] != a["kind"]:
            problems.append("kind changed for %s: %s -> %s" %
                            (rel, b["kind"], a["kind"]))
            continue
        if rel in hosts:
            if int(a["debug"]) != 0:
                problems.append("host ELF %s still has %s .debug_* bytes" %
                                (rel, a["debug"]))
            if a["sha"] == b["sha"] and int(b["debug"]) > 0:
                problems.append("host ELF %s had %s .debug_* bytes but is "
                                "byte-identical after strip" %
                                (rel, b["debug"]))
            if b["sections"] != a["sections"]:
                problems.append("non-debug ELF sections changed: %s" % rel)
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
        },
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
    elif len(argv) == 5 and argv[1] == "evidence":
        cmd_evidence(argv[2], argv[3], argv[4])
    else:
        raise SystemExit(__doc__)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
