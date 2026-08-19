#!/usr/bin/env bash
set -euo pipefail

# triage.py 的判定表测试。
#
# 这段逻辑决定"要不要自动开一个升级 Go 的 PR"，判错的两个方向都很糟：
# 该升不升会让已发布的二进制继续带着漏洞；不该升却升了会让定时任务自作主张
# 发起一次跨版本迁移。因此每条分支都要有用例钉住。
#
# 用例里的 JSON 是 govulncheck -format json 的格式：一串首尾相接的 JSON 值，
# 不是 JSON 数组。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
triage="$repo_root/scripts/security/triage.py"

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

failures=0

# 期望 triage 在给定输入下输出某个 key=value。
expect() {
  local name=$1 fixture=$2 current=$3 key=$4 want=$5
  local output got
  if ! output=$("$triage" --current "$current" "$fixture" 2>/dev/null); then
    echo "FAIL $name: triage 退出码非零" >&2
    failures=$((failures + 1))
    return
  fi
  got=$(awk -F= -v key="$key" '$1 == key { sub("^" key "=", ""); print }' <<<"$output")
  if [[ "$got" != "$want" ]]; then
    echo "FAIL $name: $key = '$got'，want '$want'" >&2
    failures=$((failures + 1))
    return
  fi
  echo "ok   $name"
}

# ---- 1. 干净：没有任何 finding ----
cat >"$work/clean.json" <<'JSON'
{"config": {"protocol_version": "v1.0.0", "scanner_name": "govulncheck"}}
{"progress": {"message": "Scanning your binary for known vulnerabilities..."}}
JSON
expect "无漏洞 -> 不升级" "$work/clean.json" go1.26.6 vulnerable false
expect "无漏洞 -> upgrade_to 为空" "$work/clean.json" go1.26.6 upgrade_to ""

# ---- 2. stdlib 漏洞，同系列有修复 -> 可自动升级 ----
cat >"$work/stdlib.json" <<'JSON'
{"osv": {"id": "GO-2026-1111", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.25.0"}, {"fixed": "1.25.9"},
                                            {"introduced": "1.26.0-0"}, {"fixed": "1.26.7"}]}]}]}}
{"finding": {"osv": "GO-2026-1111", "fixed_version": "v1.26.7", "trace": [{"module": "stdlib"}]}}
JSON
expect "stdlib 同系列有修复 -> 升级" "$work/stdlib.json" go1.26.6 upgrade_to 1.26.7
expect "stdlib 同系列有修复 -> 标记有漏洞" "$work/stdlib.json" go1.26.6 vulnerable true

# ---- 3. 两个 stdlib 漏洞，取能同时修好两者的最高版本 ----
cat >"$work/two.json" <<'JSON'
{"osv": {"id": "GO-2026-1111", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}, {"fixed": "1.26.7"}]}]}]}}
{"osv": {"id": "GO-2026-2222", "affected": [{"package": {"name": "toolchain", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}, {"fixed": "1.26.9"}]}]}]}}
{"finding": {"osv": "GO-2026-1111", "trace": [{"module": "stdlib"}]}}
{"finding": {"osv": "GO-2026-2222", "trace": [{"module": "toolchain"}]}}
JSON
expect "两个工具链漏洞 -> 取最高修复版" "$work/two.json" go1.26.6 upgrade_to 1.26.9

# ---- 4. 只有跨小版本的修复 -> 不自动升级 ----
cat >"$work/cross_minor.json" <<'JSON'
{"osv": {"id": "GO-2026-3333", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}, {"fixed": "1.27.0"}]}]}]}}
{"finding": {"osv": "GO-2026-3333", "trace": [{"module": "stdlib"}]}}
JSON
expect "跨小版本才有修复 -> 不自动升级" "$work/cross_minor.json" go1.26.6 upgrade_to ""
expect "跨小版本才有修复 -> 仍标记有漏洞" "$work/cross_minor.json" go1.26.6 vulnerable true

# ---- 5. 库里还没有修复版 -> 不自动升级 ----
cat >"$work/unfixed.json" <<'JSON'
{"osv": {"id": "GO-2026-4444", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}]}]}]}}
{"finding": {"osv": "GO-2026-4444", "trace": [{"module": "stdlib"}]}}
JSON
expect "尚无修复版 -> 不自动升级" "$work/unfixed.json" go1.26.6 upgrade_to ""

# ---- 6. 非工具链漏洞 -> 交给人 ----
cat >"$work/dependency.json" <<'JSON'
{"osv": {"id": "GO-2026-5555", "affected": [{"package": {"name": "example.com/thing", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "1.2.3"}]}]}]}}
{"finding": {"osv": "GO-2026-5555", "trace": [{"module": "example.com/thing"}]}}
JSON
expect "第三方依赖漏洞 -> 不自动升级" "$work/dependency.json" go1.26.6 upgrade_to ""

# ---- 7. 工具链 + 依赖混合 -> 只要有一个不是工具链就交给人 ----
cat >"$work/mixed.json" <<'JSON'
{"osv": {"id": "GO-2026-1111", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}, {"fixed": "1.26.7"}]}]}]}}
{"osv": {"id": "GO-2026-5555", "affected": [{"package": {"name": "example.com/thing", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "1.2.3"}]}]}]}}
{"finding": {"osv": "GO-2026-1111", "trace": [{"module": "stdlib"}]}}
{"finding": {"osv": "GO-2026-5555", "trace": [{"module": "example.com/thing"}]}}
JSON
expect "混合命中 -> 不自动升级" "$work/mixed.json" go1.26.6 upgrade_to ""

# ---- 8. 已经在修复版上 -> 不重复升级 ----
expect "已在修复版上 -> 不升级" "$work/stdlib.json" go1.26.7 upgrade_to ""

# ---- 9. 七个架构各报一次同一个漏洞 -> 去重后仍是一次升级 ----
for arch in amd64 arm64 armv7 386 s390x riscv64 ppc64le; do
  cp "$work/stdlib.json" "$work/ecs_linux_$arch.json"
done
if output=$("$triage" --current go1.26.6 "$work"/ecs_linux_*.json 2>/dev/null); then
  got=$(awk -F= '$1 == "upgrade_to" { print $2 }' <<<"$output")
  if [[ "$got" == "1.26.7" ]]; then
    echo "ok   七架构重复命中 -> 去重为一次升级"
  else
    echo "FAIL 七架构重复命中：upgrade_to = '$got'，want 1.26.7" >&2
    failures=$((failures + 1))
  fi
else
  echo "FAIL 七架构重复命中：triage 退出码非零" >&2
  failures=$((failures + 1))
fi

# ---- 10. 发布后 wiring：current 必须来自制品元数据，而不是 security runner ----
#
# scan_released.sh 把实际 Release 二进制的 go version -m 结果作为
# release_build_go 写进 $GITHUB_OUTPUT；security.yml 再把该字段传给 triage.py。
# 这个 fixture 固定制品为 go1.26.5、runner 为 go1.26.6、OSV fixed 为
# 1.26.6。若 workflow 又退回使用 runner 的 go env GOVERSION，triage 会把
# 当前版本误认为已是修复版，upgrade_to 就会错误地为空。
cat >"$work/wiring.json" <<'JSON'
{"osv": {"id": "GO-2026-WIRING", "affected": [{"package": {"name": "stdlib", "ecosystem": "Go"},
  "ranges": [{"type": "SEMVER", "events": [{"introduced": "1.26.0-0"}, {"fixed": "1.26.6"}]}]}]}}
{"finding": {"osv": "GO-2026-WIRING", "trace": [{"module": "stdlib"}]}}
JSON

security_workflow="$repo_root/.github/workflows/security.yml"
scan_output="$work/scan-output"
printf '%s\n' \
  'release_build_go=go1.26.5' \
  'released_tag=v0.7.3' >"$scan_output"

if ! grep -F -- 'RELEASE_BUILD_GO: ${{ steps.scan.outputs.release_build_go }}' \
  "$security_workflow" >/dev/null; then
  echo "FAIL 发布后 wiring：security.yml 未消费 scan 的 release_build_go" >&2
  failures=$((failures + 1))
fi
if ! grep -F -- '--current "$RELEASE_BUILD_GO"' "$security_workflow" >/dev/null; then
  echo "FAIL 发布后 wiring：security.yml 未把 Release metadata 传给 triage" >&2
  failures=$((failures + 1))
fi
if grep -F -- '--current "$(go env GOVERSION)"' "$security_workflow" >/dev/null; then
  echo "FAIL 发布后 wiring：security runner 的 GOVERSION 仍被当作 current" >&2
  failures=$((failures + 1))
fi

artifact_current=$(awk -F= '$1 == "release_build_go" { print $2; exit }' "$scan_output")
runner_current=go1.26.6
if [[ "$artifact_current" != go1.26.5 || "$runner_current" != go1.26.6 ||
  "$artifact_current" == "$runner_current" ]]; then
  echo "FAIL 发布后 wiring：fixture 未区分制品与 security runner 版本" >&2
  failures=$((failures + 1))
else
  artifact_upgrade=$(
    "$triage" --current "$artifact_current" "$work/wiring.json" |
      awk -F= '$1 == "upgrade_to" { print $2; exit }'
  )
  runner_upgrade=$(
    "$triage" --current "$runner_current" "$work/wiring.json" |
      awk -F= '$1 == "upgrade_to" { print $2; exit }'
  )
  if [[ "$artifact_upgrade" == 1.26.6 && -z "$runner_upgrade" ]]; then
    echo "ok   发布后 wiring：制品 go1.26.5 -> upgrade_to=1.26.6（runner go1.26.6 未被误用）"
  else
    echo "FAIL 发布后 wiring：artifact upgrade_to='$artifact_upgrade', runner upgrade_to='$runner_upgrade'" >&2
    failures=$((failures + 1))
  fi
fi

echo
if [[ "$failures" -ne 0 ]]; then
  echo "triage tests failed: $failures" >&2
  exit 1
fi
echo "triage tests passed"
