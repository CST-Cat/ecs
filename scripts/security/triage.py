#!/usr/bin/env python3
"""判定一组 govulncheck 结果能否靠升级 Go 补丁工具链机械修复。

只有一种情况允许自动处理：

    漏洞全部来自 Go stdlib / toolchain
  + 漏洞库明确给出了 fixed 版本
  + 该修复版与当前工具链在同一个小版本系列（1.26.x -> 1.26.y）

这三条同时成立时，"同一份 ecs 源码 + 官方修好的 Go 工具链 = 重新生成的二进制"
是一个可以机械确认的等式，不需要人去判断修复是否正确。

其余情况一律不自动处理，只报告：

  - ecs 自身源码或第三方依赖的漏洞——需要改程序行为；
  - 只有跨小版本的修复（1.26.x -> 1.27.0）——那是一次工具链大版本迁移，
    可能带来语言与运行时行为变化，不该由定时任务自动发起；
  - 漏洞库还没给出修复版本——官方都没修好，升级无从谈起。

输入是 govulncheck -format json 的输出（一个或多个文件，每个是一串 JSON 值）。
输出是 GitHub Actions 的 key=value 形式，写到 stdout。
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

# govulncheck 把 Go 标准库与工具链报成这两个模块名。
TOOLCHAIN_MODULES = {"stdlib", "toolchain"}

GO_VERSION_PATTERN = re.compile(r"^(\d+)\.(\d+)(?:\.(\d+))?$")


def parse_go_version(value: str) -> tuple[int, int, int] | None:
    """把 1.26.6 / go1.26.6 / 1.26 解析成可比较的三元组。"""
    value = value.strip()
    if value.startswith("go"):
        value = value[2:]
    match = GO_VERSION_PATTERN.match(value)
    if not match:
        return None
    major, minor, patch = match.groups()
    return int(major), int(minor), int(patch or 0)


def format_go_version(version: tuple[int, int, int]) -> str:
    return f"{version[0]}.{version[1]}.{version[2]}"


def read_json_stream(path: pathlib.Path) -> list[dict]:
    """读一串首尾相接的 JSON 值。govulncheck 输出的不是 JSON 数组。"""
    decoder = json.JSONDecoder()
    text = path.read_text()
    values: list[dict] = []
    index = 0
    length = len(text)
    while index < length:
        while index < length and text[index].isspace():
            index += 1
        if index >= length:
            break
        value, index = decoder.raw_decode(text, index)
        values.append(value)
    return values


def collect(paths: list[pathlib.Path]) -> tuple[dict[str, dict], list[dict]]:
    """返回 (osv_id -> OSV 记录, findings)。"""
    osvs: dict[str, dict] = {}
    findings: list[dict] = []
    for path in paths:
        for entry in read_json_stream(path):
            if "osv" in entry:
                record = entry["osv"]
                osvs[record["id"]] = record
            elif "finding" in entry:
                findings.append(entry["finding"])
    return osvs, findings


def finding_module(finding: dict) -> str:
    trace = finding.get("trace") or []
    if not trace:
        return ""
    return trace[0].get("module", "")


def fixed_versions_for(osv: dict, module: str) -> list[tuple[int, int, int]]:
    """从 OSV 记录里取出某个模块的全部 fixed 版本。"""
    fixed: list[tuple[int, int, int]] = []
    for affected in osv.get("affected", []):
        package = affected.get("package", {})
        if package.get("name") != module:
            continue
        for entry in affected.get("ranges", []):
            for event in entry.get("events", []):
                if "fixed" not in event:
                    continue
                version = parse_go_version(event["fixed"])
                if version is not None:
                    fixed.append(version)
    return fixed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--current", required=True, help="当前工具链，如 go1.26.6")
    parser.add_argument("json", nargs="+", help="govulncheck -format json 的输出文件")
    arguments = parser.parse_args()

    current = parse_go_version(arguments.current)
    if current is None:
        print(f"triage: 无法解析当前工具链版本：{arguments.current}", file=sys.stderr)
        return 2

    paths = [pathlib.Path(item) for item in arguments.json]
    missing = [path for path in paths if not path.is_file()]
    if missing:
        print(f"triage: 找不到输入文件：{missing}", file=sys.stderr)
        return 2

    osvs, findings = collect(paths)

    # 同一个漏洞会在七个架构上各报一次，按 OSV id 去重。
    unique: dict[str, str] = {}
    for finding in findings:
        osv_id = finding.get("osv")
        if osv_id:
            unique.setdefault(osv_id, finding_module(finding))

    if not unique:
        print("triage: 没有发现漏洞", file=sys.stderr)
        print("vulnerable=false")
        print("upgrade_to=")
        print("reason=no vulnerabilities")
        return 0

    print(f"triage: 去重后 {len(unique)} 个漏洞：{sorted(unique)}", file=sys.stderr)

    non_toolchain = sorted(
        osv_id for osv_id, module in unique.items() if module not in TOOLCHAIN_MODULES
    )
    if non_toolchain:
        reason = f"ecs source or dependency vulnerabilities need human review: {', '.join(non_toolchain)}"
        print(f"triage: {reason}", file=sys.stderr)
        print("vulnerable=true")
        print("upgrade_to=")
        print(f"reason={reason}")
        return 0

    # 全部来自工具链。逐个求"能修好它的、同系列的最低版本"，再取其中最高的。
    required: list[tuple[int, int, int]] = []
    for osv_id, module in sorted(unique.items()):
        candidates = [
            version
            for version in fixed_versions_for(osvs.get(osv_id, {}), module)
            if version > current and version[:2] == current[:2]
        ]
        if not candidates:
            reason = f"{osv_id} has no fixed Go release in the {current[0]}.{current[1]}.x series"
            print(f"triage: {reason}", file=sys.stderr)
            print("vulnerable=true")
            print("upgrade_to=")
            print(f"reason={reason}")
            return 0
        chosen = min(candidates)
        print(f"triage: {osv_id} ({module}) 需要 >= {format_go_version(chosen)}", file=sys.stderr)
        required.append(chosen)

    target = format_go_version(max(required))
    reason = f"all {len(unique)} findings are Go toolchain issues fixed in {target}"
    print(f"triage: 可自动升级到 {target}", file=sys.stderr)
    print("vulnerable=true")
    print(f"upgrade_to={target}")
    print(f"reason={reason}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
