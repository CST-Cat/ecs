#!/usr/bin/env bash
set -euo pipefail

# 对一组主程序二进制运行 govulncheck。
#
# 同一份实现服务两个时间点：
#
#   发布前门禁   release.yml 的 security job —— 候选制品现在安全吗？
#   发布后监控   security.yml 的 released job —— 已发布的东西今天还安全吗？
#
# 两者手段完全相同，差别只在 --dist 指向候选制品还是下载回来的已发布制品。
# 做成两份实现的话，跑得少的那一侧迟早会落后。
#
# 扫的是解包出来的实际二进制而不是源码：源码扫描看不见工具链自身的问题，
# 而用户拿到的正是这条工具链编出来的产物。
#
# govulncheck 的版本由 devtools/go.mod 锁定，与 CI 的源码扫描是同一份。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"

usage() {
  echo "usage: scripts/release/security.sh --dist DIR [--json-dir DIR]" >&2
}

die() {
  printf 'release_security_result=fatal\n'
  echo "release-security: $*" >&2
  exit 1
}

classify_json() {
  local json_file=$1

  # govulncheck -format json 是连续 JSON 值而不是数组。jq -s 同时负责解析
  # 整个 stream 和提取 finding；解析失败必须 hard fail，不能当成"无漏洞"。
  "$jq_bin" -s -r '
    def flatten_values:
      if type == "string" or type == "number" then [.]
      elif type == "array" then [ .[] | flatten_values[] ]
      elif type == "object" then
        [ .severity?, .level?, .rating?, .security_severity?, .score? ]
        | map(select(. != null) | flatten_values)
        | add // []
      else [] end;

    def declared_severity_values($finding; $osv):
      [
        $finding.severity?,
        $finding.security_severity?,
        $finding.cvss?,
        $finding.cvss_v3?,
        $finding.cvss_v4?,
        $osv.severity?,
        $osv.security_severity?,
        $osv.cvss?,
        $osv.cvss_v3?,
        $osv.cvss_v4?,
        $osv.database_specific?.severity?,
        $osv.database_specific?.security_severity?,
        $osv.database_specific?.cvss?,
        $osv.database_specific?.cvss_v3?,
        $osv.database_specific?.cvss_v4?
      ]
      | map(select(. != null) | flatten_values)
      | add // [];

    def named_severe:
      if type == "string" then
        (ascii_upcase
         | gsub("^[[:space:]]+|[[:space:]]+$"; "")
         | . == "HIGH" or . == "CRITICAL")
      else false end;

    # 7.0 以上只在明确的 severity/CVSS 数值字段下解释为 HIGH/CRITICAL；
    # 其它文本（包括漏洞描述里的措辞）不参与判定。
    def cvss_score_severe:
      if type == "number" then . >= 7
      elif type == "string" then
        if test("^[0-9]+(\\.[0-9]+)?$") then tonumber >= 7 else false end
      else false end;

    def severity_kind($values):
      if any($values[]?; named_severe) or any($values[]?; cvss_score_severe) then
        "severe"
      elif ($values | length) > 0 then
        "declared"
      else
        "unknown"
      end;

    # govulncheck binary mode emits a trace frame for a vulnerability that is
    # actually present in the scanned binary. Require a non-empty module too,
    # so an incomplete/fabricated trace cannot open the release gate.
    def trace_reachable:
      ((.trace? | type) == "array")
      and ((.trace | length) > 0)
      and any(.trace[]?;
        (type == "object"
         and ((.module? // "") | type == "string" and length > 0)));

    if length == 0 then
      error("empty govulncheck JSON stream")
    elif any(.[]; type != "object") then
      error("govulncheck JSON stream contains a non-object")
    elif any(.[];
      ((has("osv") and ((.osv | type) != "object"))
       or (has("finding") and ((.finding | type) != "object")))) then
      error("govulncheck JSON message has an invalid object")
    elif any(.[];
      (has("osv")
       and (((.osv.id? // "") | type) != "string"
            or ((.osv.id? // "") | length) == 0))) then
      error("govulncheck OSV message has no id")
    elif any(.[];
      (has("finding")
       and (((.finding.osv? // "") | type) != "string"
            or ((.finding.osv? // "") | length) == 0))) then
      error("govulncheck finding has no OSV id")
    else . end
    | reduce .[] as $message
        ({osvs: {}, findings: []};
         if ($message | has("osv")) then
           .osvs[$message.osv.id] = $message.osv
         elif ($message | has("finding")) then
           .findings += [$message.finding]
         else . end)
    | [
        .findings[] as $finding
        | (.osvs[$finding.osv] // {}) as $osv
        | (declared_severity_values($finding; $osv)) as $severity
        | {
            reachable: ($finding | trace_reachable),
            severity: severity_kind($severity)
          }
      ]
    | {
        finding: length,
        severe_reachable: (map(select(.reachable and .severity == "severe")) | length),
        severe_unreachable: (map(select((.severity == "severe") and (.reachable | not))) | length),
        unknown_severity: (map(select(.severity == "unknown")) | length)
      }
    | [.finding, .severe_reachable, .severe_unreachable, .unknown_severity]
    | @tsv
  ' "$json_file"
}

dist=""
json_dir=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --dist)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--dist requires a value"
      dist=$2
      shift 2
      ;;
    --json-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--json-dir requires a value"
      json_dir=$2
      shift 2
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

[[ -n "$dist" ]] || {
  usage
  die "--dist is required"
}
[[ "$dist" == /* ]] || dist="$ECS_REPO_ROOT/$dist"
[[ -d "$dist" ]] || die "no such dist directory: $dist"

govulncheck=$(ecs_devtool govulncheck)
[[ -n "$govulncheck" && -x "$govulncheck" ]] || die "govulncheck 不可执行：$govulncheck"
scan_root=$(mktemp -d)
trap 'rm -rf -- "$scan_root"' EXIT

# 原样保留 govulncheck 的 JSON：triage 需要 OSV 记录里的 fixed 版本，
# 只留 finding 会把"官方修好了没有"这个信息丢掉。
if [[ -n "$json_dir" ]]; then
  mkdir -p -- "$json_dir" || die "无法创建 JSON 输出目录：$json_dir"
fi

# govulncheck 的正常输出走 stderr。finding 本身只产生告警；只有漏洞记录明确
# 标成 HIGH/CRITICAL（或明确的 CVSS 高危分数），且 finding 对这个二进制有
# 可达 trace，才是发布门禁。严重性字段缺失时不从 summary/details 猜测。
# stdout 必须保持干净——scan_released.sh 会把自己的 stdout 直接写进
# $GITHUB_OUTPUT，一行不带 '=' 的噪音就会让整个 job 以 Invalid format 失败。
listing=$(ecs_release_binaries "$dist" "$scan_root") || die "无法解出主程序二进制"

[[ -n "$listing" ]] || die "未找到可扫描的主程序二进制"

jq_bin=$(command -v jq) || die "找不到 jq，无法解析 govulncheck JSON"

hard_fail=0
block_release=0
scanned=0
while IFS=$'\t' read -r name binary; do
  if [[ -z "$name" || -z "$binary" || "$name" == */* ]]; then
    echo "release-security: 无效的二进制清单行：${name@Q} ${binary@Q}" >&2
    hard_fail=1
    continue
  fi
  if [[ ! -f "$binary" || ! -r "$binary" ]]; then
    echo "release-security: 二进制输入不可读：$binary" >&2
    hard_fail=1
    continue
  fi

  echo "release-security: 扫描 $name" >&2
  json_file="$scan_root/$name.json"
  json_status=0
  if "$govulncheck" -format json -mode binary "$binary" >"$json_file"; then
    json_status=0
  else
    json_status=$?
  fi

  # 文本输出保留给诊断；govulncheck 的 3 是"有 finding"，不是工具错误。
  text_status=0
  if "$govulncheck" -mode binary "$binary" >&2; then
    text_status=0
  else
    text_status=$?
  fi

  if [[ -n "$json_dir" ]]; then
    json_output="$json_dir/$name.json"
    if [[ -d "$json_output" ]]; then
      echo "release-security: JSON 输出目标是目录：$json_output" >&2
      hard_fail=1
    elif ! cp -- "$json_file" "$json_output"; then
      echo "release-security: 无法写入 JSON：$json_output" >&2
      hard_fail=1
    fi
  fi

  case "$json_status" in
    0 | 3) ;;
    *)
      echo "release-security: $name 的 govulncheck JSON 扫描失败（exit $json_status）" >&2
      hard_fail=1
      ;;
  esac
  case "$text_status" in
    0 | 3) ;;
    *)
      echo "release-security: $name 的 govulncheck 文本扫描失败（exit $text_status）" >&2
      hard_fail=1
      ;;
  esac

  # 即使工具报告了 finding，也必须验证完整 JSON；否则不能把解析失败当作
  # 普通告警放过。两个 govulncheck 调用都已完成后才在这里处理该二进制。
  if [[ "$json_status" -eq 0 || "$json_status" -eq 3 ]]; then
    if ! classification=$(classify_json "$json_file"); then
      echo "release-security: 无法解析 $name 的 govulncheck JSON" >&2
      hard_fail=1
      scanned=$((scanned + 1))
      continue
    fi

    finding_count=""
    severe_reachable=""
    severe_unreachable=""
    unknown_severity=""
    IFS=$'\t' read -r finding_count severe_reachable severe_unreachable unknown_severity <<<"$classification"
    if [[ ! "$finding_count" =~ ^[0-9]+$ ||
      ! "$severe_reachable" =~ ^[0-9]+$ ||
      ! "$severe_unreachable" =~ ^[0-9]+$ ||
      ! "$unknown_severity" =~ ^[0-9]+$ ]]; then
      echo "release-security: $name 的 JSON 分类结果无效：$classification" >&2
      hard_fail=1
      scanned=$((scanned + 1))
      continue
    fi

    if [[ "$json_status" -eq 3 && "$finding_count" -eq 0 ]]; then
      echo "release-security: $name 的 govulncheck 返回 finding 状态但 JSON 没有 finding" >&2
      hard_fail=1
    fi
    if [[ "$text_status" -eq 3 && "$finding_count" -eq 0 ]]; then
      echo "release-security: $name 的文本扫描报告 finding，但 JSON 没有 finding" >&2
      hard_fail=1
    fi

    ordinary_count=$((finding_count - severe_reachable - severe_unreachable - unknown_severity))
    echo "release-security: $name finding=$finding_count ordinary=$ordinary_count severe_reachable=$severe_reachable severe_unreachable=$severe_unreachable unknown_severity=$unknown_severity" >&2
    if [[ "$unknown_severity" -gt 0 ]]; then
      echo "release-security: $name 漏洞库未提供可确认的严重性字段，保守按普通 finding 告警" >&2
    fi
    if [[ "$severe_reachable" -gt 0 ]]; then
      echo "release-security: $name 有明确 HIGH/CRITICAL 且对实际二进制可达的 finding，阻止发布" >&2
      block_release=1
    elif [[ "$severe_unreachable" -gt 0 ]]; then
      echo "release-security: $name 有明确 HIGH/CRITICAL 但 trace 对实际二进制不可达，仅告警" >&2
    elif [[ "$finding_count" -gt 0 ]]; then
      echo "release-security: $name 存在普通或严重性不明确的 finding，仅告警" >&2
    fi
  fi
  scanned=$((scanned + 1))
done <<<"$listing"

if [[ "$hard_fail" -ne 0 ]]; then
  die "扫描工具、输入或 JSON 处理失败"
fi
if [[ "$block_release" -ne 0 ]]; then
  printf 'release_security_result=blocked\n'
  echo "release-security: 存在明确严重且对实际二进制可达的漏洞" >&2
  exit 1
fi

printf 'release_security_result=clean\n'
echo "release-security: $scanned 个二进制扫描完成；普通 finding 仅告警" >&2
