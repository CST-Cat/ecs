#!/usr/bin/env bash
set -euo pipefail

# 发布彩排的静态契约门禁。
#
# 这不是为了“证明 YAML 一定能运行”，而是钉死最容易回归、且一旦回归就会产生
# 永久副作用的边界：workflow_dispatch 不能仅凭 tag-shaped ref 获得发布权限；
# SDK 的默认手动模式必须是 rehearsal；三条发布 workflow 的 contents:write
# 只能出现在唯一 publish job；ECS/Bundle 彩排必须走 publish.sh --check-only。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

die() {
  echo "release-rehearsal-contract: $*" >&2
  exit 1
}

assert_contains() {
  local file=$1 needle=$2
  grep -Fq -- "$needle" "$file" || die "$file missing contract: $needle"
}

assert_absent() {
  local file=$1 needle=$2
  if grep -Fq -- "$needle" "$file"; then
    die "$file contains forbidden legacy contract: $needle"
  fi
}

assert_no_redirected_no_color() {
  local file=$1
  if awk '/redirected/ && /--no-color/ { found=1 } END { exit found }' "$file"; then
    return 0
  fi
  die "$file uses --no-color in its redirected output check"
}

assert_single_write_job() {
  local file=$1 count
  count=$(grep -cE '^[[:space:]]+contents: write$' "$file" || true)
  [[ "$count" -eq 1 ]] || die "$file must contain exactly one contents: write, got $count"
}

assert_no_non_publish_write_job() {
  local file=$1 violation
  violation=$(awk '
    /^[[:space:]]{2}[A-Za-z0-9_-]+:$/ {
      job=$1
      sub(/:$/, "", job)
    }
    /^[[:space:]]+contents: write$/ && job != "publish" {
      print "job=" job ": " $0
      exit 1
    }
  ' "$file" || true)
  [[ -z "$violation" ]] || die "$file grants contents: write outside publish: $violation"
}

release=.github/workflows/release.yml
bundle=.github/workflows/bundle-release.yml
windows=.github/workflows/windows-tools.yml
sdk=.github/workflows/freebsd-sdk-release.yml
freeze=scripts/release/freeze.sh
publisher=scripts/release/publish.sh

# ECS：正式发布必须同时是 push 与 v* tag；dispatch 不论选 branch 还是 tag 都是彩排。
assert_contains "$release" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"
assert_contains "$release" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$release" "--check-only"
assert_absent "$release" "    if: startsWith(github.ref, 'refs/tags/v')"
assert_single_write_job "$release"
assert_no_non_publish_write_job "$release"

# Bundle：同样双重门禁，并在正式出口校验触发 tag 等于 tools/BUNDLE。
assert_contains "$bundle" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/bundle-v')"
assert_contains "$bundle" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$bundle" "bundle trigger tag"
assert_contains "$bundle" "--check-only"
assert_absent "$bundle" "    if: startsWith(github.ref, 'refs/tags/bundle-v')"
assert_single_write_job "$bundle"
assert_no_non_publish_write_job "$bundle"

# Windows tools：reusable workflow、真实 runner DAG、locked builder/gate and
# package/E2E boundaries are all part of the static contract.  It must stay
# read-only and must not grow a second host-tool or alternate builder path.
[[ -f "$windows" ]] || die "$windows is missing"
assert_contains "$windows" "workflow_call:"
assert_contains "$windows" "workflow_dispatch:"
assert_contains "$windows" "contents: read"
assert_absent "$windows" "contents: write"
assert_contains "$windows" 'group: windows-tools-${{ github.workflow }}-${{ github.ref }}'
assert_contains "$windows" 'cancel-in-progress: true'
assert_contains "$windows" "needs: lock-check"
assert_contains "$windows" "needs: build"
assert_contains "$windows" "needs: [verify-2022, verify-2025]"
assert_contains "$windows" "needs: [package, verify-2022]"
assert_contains "$windows" "runs-on: windows-2022"
assert_contains "$windows" "runs-on: windows-2025"
assert_contains "$windows" "scripts/build_tools_windows.ps1"
assert_contains "$windows" "scripts/ci/windows_tools_gate.ps1"
assert_contains "$windows" "scripts/package.sh --tools-stage tools-stage --target windows_amd64"
assert_contains "$windows" "needs: [package, verify-2025]"
assert_contains "$windows" "ecs-windows-tools-gate-inputs-2025"
assert_contains "$windows" "go build -trimpath -o ecs.exe ./cmd/ecs"
assert_contains "$windows" "ecs.exe"
assert_contains "$windows" "--version"
assert_contains "$windows" "plan', '--profile', 'standard"
assert_contains "$windows" "Assert-WindowsSystemFacts"
assert_contains "$windows" "Get-JsonPropertyValue"
assert_contains "$windows" "Get-SystemFieldRaw"
assert_contains "$windows" "PSObject.Properties"
assert_contains "$windows" "missing JSON property"
assert_contains "$windows" "system JSON has no structured facts"
assert_contains "$windows" "evidence.valid"
assert_contains "$windows" "evidence.expected"
assert_contains "$windows" "evidence.unit"
assert_contains "$windows" 'failures=$failureText'
assert_contains "$windows" "memory_total"
assert_contains "$windows" "disk_total"
assert_contains "$windows" "uptime_seconds"
assert_contains "$windows" "logical_cpus"
assert_contains "$windows" "noColorOutput"
assert_contains "$windows" "noColorFlagOutput"
assert_contains "$windows" "NO_COLOR did not suppress ANSI output"
assert_contains "$windows" "--no-color did not suppress ANSI output"
assert_no_redirected_no_color "$windows"
assert_contains "$windows" "-tags=windows ./internal/probe -run '^TestWindowsNativeICMPLoopback$'"
assert_contains "$windows" "-tags=windows ./internal/probe -run '^TestWindowsNativeICMPDeadlineDoesNotHang$'"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2022 TestWindowsNativeICMPLoopback failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2022 TestWindowsNativeICMPDeadlineDoesNotHang failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2022 TestProbeCommandWindowsProcessTree failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2022 internal/ui tests failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2025 TestWindowsNativeICMPLoopback failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2025 TestWindowsNativeICMPDeadlineDoesNotHang failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2025 TestProbeCommandWindowsProcessTree failed' }"
assert_contains "$windows" "if (\$LASTEXITCODE -ne 0) { throw 'VERIFY-2025 internal/ui tests failed' }"
assert_contains "$windows" "@('en-US', 'zh-CN')"
assert_contains "$windows" "Assert-EcsLoopbackLatencyReport"
assert_contains "$windows" "ecs.report/v1"
assert_contains "$windows" "loopback_ipv4"
assert_contains "$windows" "icmp-echo-v1"
assert_contains "$windows" "native loopback ICMP"
assert_contains "$windows" "TestProbeCommandWindowsProcessTree"
assert_contains "$windows" "go test ./internal/ui"
assert_contains "$windows" "zstd.exe"
assert_contains "$windows" "npb-ep.exe"
assert_contains "$windows" "npb-ft.exe"
assert_contains "$windows" "stream.exe"
assert_contains "$windows" "openssl.exe"
assert_contains "$windows" "fio.exe"
assert_contains "$windows" "windowsaio"
assert_contains "$windows" "performance_valid=false"
assert_contains "$windows" "NextTrace not bundled"
assert_contains "$windows" "ecs_windows_amd64.zip"
assert_contains "$windows" "ecs-corpus_silesia-v1.tar.gz"
assert_contains "$windows" "scripts/cross.sh --target windows_amd64"
assert_contains "$windows" "CURRENT_COMMIT"
assert_contains "$windows" "run.ps1"
assert_contains "$windows" "install.ps1"
assert_contains "$windows" "New-SelfSignedCertificate"
assert_contains "$windows" "Import-Certificate"
assert_contains "$windows" "SslStream"
assert_contains "$windows" "ECS_RELEASE_BASE"
assert_contains "$windows" "ECS_BUNDLE_RELEASE_BASE"
assert_contains "$windows" "ECS_TOOL_BIN"
assert_contains "$windows" "LOCALAPPDATA"
assert_contains "$windows" "actions/download-artifact"
assert_absent "$windows" "SkipCertificateCheck"
assert_absent "$windows" "releases/download"
assert_absent "$windows" "releases/latest/download"
assert_absent "$windows" ")[0].FullName"
for forbidden in 'WSL' 'Cygwin' 'winget' 'choco' 'ping\.exe' 'skip' 'continue-on-error' 'host PATH'; do
  if grep -Eiq -- "$forbidden" "$windows"; then
    die "$windows contains forbidden Windows CI contract: $forbidden"
  fi
done

# CI must add both Windows gates without serializing the existing matrix, and
# the release chains must consume the ten-target arrays/artifacts.
assert_contains .github/workflows/ci.yml 'win/candidate-c'
assert_contains .github/workflows/ci.yml 'win-candidate-c'
assert_contains .github/workflows/ci.yml 'windows-runtime'
assert_contains .github/workflows/ci.yml 'windows-tools'
assert_contains .github/workflows/ci.yml 'windows-2022'
assert_contains .github/workflows/ci.yml 'windows-2025'
assert_contains .github/workflows/ci.yml 'needs: [unit, compat, quality, integration, race, cross, freebsd, windows-runtime, windows-tools, submissions]'
assert_contains .github/workflows/ci.yml 'Assert-WindowsSystemFacts'
assert_contains .github/workflows/ci.yml 'Get-JsonPropertyValue'
assert_contains .github/workflows/ci.yml 'Get-SystemFieldRaw'
assert_contains .github/workflows/ci.yml 'PSObject.Properties'
assert_contains .github/workflows/ci.yml 'missing JSON property'
assert_contains .github/workflows/ci.yml 'system JSON has no structured facts'
assert_contains .github/workflows/ci.yml 'evidence.valid'
assert_contains .github/workflows/ci.yml 'evidence.expected'
assert_contains .github/workflows/ci.yml 'evidence.unit'
assert_contains .github/workflows/ci.yml 'failures=$failureText'
assert_contains .github/workflows/ci.yml 'memory_total'
assert_contains .github/workflows/ci.yml 'disk_total'
assert_contains .github/workflows/ci.yml 'uptime_seconds'
assert_contains .github/workflows/ci.yml 'logical_cpus'
assert_contains .github/workflows/ci.yml 'noColorOutput'
assert_contains .github/workflows/ci.yml 'noColorFlagOutput'
assert_contains .github/workflows/ci.yml 'NO_COLOR did not suppress ANSI output'
assert_contains .github/workflows/ci.yml '--no-color did not suppress ANSI output'
assert_no_redirected_no_color .github/workflows/ci.yml
assert_contains .github/workflows/ci.yml "-tags=windows ./internal/probe -run '^TestWindowsNativeICMPLoopback$'"
assert_contains .github/workflows/ci.yml "-tags=windows ./internal/probe -run '^TestWindowsNativeICMPDeadlineDoesNotHang$'"
assert_contains .github/workflows/ci.yml "if (\$LASTEXITCODE -ne 0) { throw 'TestWindowsNativeICMPLoopback failed' }"
assert_contains .github/workflows/ci.yml "if (\$LASTEXITCODE -ne 0) { throw 'TestWindowsNativeICMPDeadlineDoesNotHang failed' }"
assert_contains .github/workflows/ci.yml "if (\$LASTEXITCODE -ne 0) { throw 'TestProbeCommandWindowsProcessTree failed' }"
assert_contains .github/workflows/ci.yml "if (\$LASTEXITCODE -ne 0) { throw 'internal/ui tests failed' }"
assert_contains .github/workflows/ci.yml "if (\$LASTEXITCODE -ne 0) { throw 'TestNotifyContextHandlesConsoleInterrupt failed' }"
assert_contains .github/workflows/ci.yml "@('en-US', 'zh-CN')"
assert_contains .github/workflows/ci.yml 'Assert-EcsLoopbackLatencyReport'
assert_contains .github/workflows/ci.yml 'ecs.report/v1'
assert_contains .github/workflows/ci.yml 'loopback_ipv4'
assert_contains .github/workflows/ci.yml 'icmp-echo-v1'
assert_absent .github/workflows/ci.yml 'contents: write'
assert_contains "$bundle" 'windows-build'
assert_contains "$bundle" 'ecs-windows-tools-stage-*'
assert_contains "$bundle" 'ECS_WINDOWS_TARGET_IDS'
assert_contains "$bundle" '--all-targets'
assert_contains "$release" 'windows_amd64'
assert_contains "$release" 'ECS_RELEASE_TARGET_IDS'

# SDK：手动入口默认 rehearsal；publish 需要显式模式 + sdk_version，prepare 与
# rehearsal 本身保持只读，只有 guarded publish 拿写权限。正式版本还必须读取
# 已有 SDK tag 并用 sort -V 证明输入严格单调递增。
assert_contains "$sdk" "release_mode:"
assert_contains "$sdk" "default: rehearsal"
assert_contains "$sdk" "- rehearsal"
assert_contains "$sdk" "- publish"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'rehearsal'"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'publish'"
assert_contains "$sdk" "Prepare SDK release candidate"
assert_contains "$sdk" "matching-refs/tags/ci-freebsd-gnu-sdk-v"
assert_contains "$sdk" "sort -V"
assert_contains "$sdk" "must be strictly newer than existing maximum"
assert_contains "$sdk" "gh release create"
assert_single_write_job "$sdk"

sdk_version_block=$(grep -A4 -F '      sdk_version:' "$sdk" || true)
grep -Fq 'required: false' <<<"$sdk_version_block" ||
  die "$sdk sdk_version must be optional for rehearsal"

# freeze 必须先按 event 分流；workflow_dispatch 分支必须排在 push 分支之前。
dispatch_line=$(grep -nE '^[[:space:]]+workflow_dispatch\)' "$freeze" | head -1 | cut -d: -f1)
push_line=$(grep -nE '^[[:space:]]+push\)' "$freeze" | head -1 | cut -d: -f1)
[[ -n "$dispatch_line" && -n "$push_line" && "$dispatch_line" -lt "$push_line" ]] ||
  die "freeze.sh must classify workflow_dispatch before push/tag parsing"

# check-only 是发布脚本自己的硬边界，而不是只靠 workflow 注释约定。
assert_contains "$publisher" "--check-only"
assert_contains "$publisher" "未访问或修改 GitHub Release"
check_only_line=$(grep -nE '^[[:space:]]*if \[\[ "\$check_only" -eq 1 \]\]' "$publisher" | tail -1 | cut -d: -f1)
gh_line=$(grep -nE '^[[:space:]]*command -v gh ' "$publisher" | head -1 | cut -d: -f1)
[[ -n "$check_only_line" && -n "$gh_line" && "$check_only_line" -lt "$gh_line" ]] ||
  die "publish.sh check-only must return before gh becomes a dependency"

echo "release-rehearsal-contract: all rehearsal/publish boundaries are pinned"
