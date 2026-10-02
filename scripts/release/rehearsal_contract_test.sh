#!/usr/bin/env bash
set -euo pipefail

# 发布彩排契约只检查永久副作用和供给链边界。
# workflow 的 PowerShell 实现细节由其可单独执行的脚本负责，不在这里锁定文本形状。

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

die() {
  echo "release-rehearsal-contract: $*" >&2
  exit 1
}

assert_contains() {
  local file=$1 needle=$2
  if ! grep -Fq -- "$needle" "$file"; then
    die "$file missing contract: $needle"
  fi
}

assert_absent() {
  local file=$1 needle=$2
  if grep -Fq -- "$needle" "$file"; then
    die "$file contains forbidden contract: $needle"
  fi
}

assert_absent_regex() {
  local file=$1 pattern=$2
  if grep -Eiq -- "$pattern" "$file"; then
    die "$file contains forbidden contract pattern: $pattern"
  fi
}

assert_no_core_gate_bypass() {
  local file=$1
  for pattern in     '^[[:space:]]*continue-on-error[[:space:]]*:'     '^[[:space:]]*condition[[:space:]]*:'     '^[[:space:]]*if[[:space:]]*:[[:space:]]*[^#]*(skip|skipped)'     '^[[:space:]]*(skip|skipped)[[:space:]]*[:=]'     '^[[:space:]]*\$(skip|skipped)[[:space:]]*='     '^[[:space:]]*(exit|return)[[:space:]]+0([[:space:]]|$)'; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_no_sensitive_token_evasion() {
  local file=$1
  for pattern in     '\[char\]'     '\[byte[[:space:]]*\[\]\]|GetString[[:space:]]*\([^)]*byte'     '(FromBase64String|ToBase64String|Base64)'     '(SkipCertificateCheck|CertificateCheck)[^[:cntrl:]]*(Replace|Substring)|(Replace|Substring)[^[:cntrl:]]*(SkipCertificateCheck|CertificateCheck)'     '(Skip|CertificateCheck)[^[:cntrl:]]*[+][^[:cntrl:]]*(Skip|CertificateCheck)'     '(Skip|CertificateCheck)[^[:cntrl:]]*-[[:space:]]*f|-[[:space:]]*f[^[:cntrl:]]*(Skip|CertificateCheck)'     'part[[:alnum:]_]*[[:space:]]*[+][[:space:]]*part[[:alnum:]_]*'; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_no_trusted_root_mutation() {
  local file=$1
  for pattern in     'CurrentUser\\Root'     'LocalMachine\\Root'     'Import-Certificate[^[:cntrl:]]*Root'     'Root[^[:cntrl:]]*Import-Certificate'     'X509Store[^[:cntrl:]]*Root'     '[.]Open[[:space:]]*[(][^)]*ReadWrite'     '[.]Add[[:space:]]*[(][[:space:]]*rootCertificate[[:space:]]*[)]'     'certutil[^[:cntrl:]]+-addstore[^[:cntrl:]]+Root'; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_no_product_tls_bypass() {
  local file=$1
  for pattern in     'SkipCertificateCheck'     'ServicePointManager'     'ServerCertificateValidationCallback'     'TrustAll'     'AllowInsecure'     'NoVerify'     'certificate[[:space:]-]+bypass'; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_single_write_job() {
  local file=$1 count
  count=$(awk '/^[[:space:]]+contents: write[[:space:]]*$/ { count++ } END { print count + 0 }' "$file")
  [[ "$count" -eq 1 ]] || die "$file must contain exactly one contents: write job, got $count"
}

assert_no_non_publish_write_job() {
  local file=$1 violation
  if violation=$(awk '
    /^[[:space:]]{2}[A-Za-z0-9_-]+:[[:space:]]*$/ {
      job=$1
      sub(/:$/, "", job)
    }
    /^[[:space:]]+contents: write[[:space:]]*$/ && job != "publish" {
      print "job=" job ": " $0
      exit 1
    }
  ' "$file"); then
    :
  else
    die "$file grants contents: write outside publish: $violation"
  fi
}

assert_versioned_actions() {
  local file=$1 violation
  if violation=$(awk '
    /uses:/ {
      if ($0 ~ /uses:[[:space:]]*\.\//) next
      if ($0 !~ /@v[0-9]+([.][0-9]+)*[[:space:]]*$/) {
        print NR ":" $0
        exit 1
      }
    }
  ' "$file"); then
    :
  else
    die "$file has an external action without a release version tag: $violation"
  fi
}

release=.github/workflows/release.yml
bundle=.github/workflows/bundle-release.yml
windows=.github/workflows/windows-tools.yml
ci=.github/workflows/ci.yml
nexttrace_report_assert=scripts/ci/windows_nexttrace_report_assert.ps1
nexttrace_capability=scripts/ci/windows_nexttrace_capability.ps1
icmp_prerequisite=scripts/ci/windows_icmp_prerequisite.ps1
runtime_contract=scripts/ci/windows_runtime_contract.ps1
tools_prepare=scripts/ci/windows_tools_prepare.ps1
tools_gate=scripts/ci/windows_tools_gate.ps1
tools_integration=scripts/ci/windows_tools_integration.ps1
tools_e2e=scripts/ci/windows_tools_e2e.ps1
tools_package=scripts/ci/windows_tools_package.sh
sdk=.github/workflows/freebsd-sdk-release.yml
freeze=scripts/release/freeze.sh
publisher=scripts/release/publish.sh
run_ps1=run.ps1
install_ps1=install.ps1

# ECS/Bundle：workflow_dispatch 永远是 rehearsal；只有正式 tag push 的 publish
# job 获得 contents: write，彩排路径必须在发布脚本的 check-only 边界内返回。
assert_contains "$release" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"
assert_contains "$release" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$release" "--check-only"
assert_absent "$release" "    if: startsWith(github.ref, 'refs/tags/v')"
assert_single_write_job "$release"
assert_no_non_publish_write_job "$release"
assert_versioned_actions "$release"

assert_contains "$bundle" "if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/bundle-v')"
assert_contains "$bundle" "if: github.event_name == 'workflow_dispatch'"
assert_contains "$bundle" "--check-only"
assert_contains "$bundle" "windows-build:"
assert_contains "$bundle" "uses: ./.github/workflows/windows-tools.yml"
assert_contains "$bundle" "needs: [tools, freebsd-build, windows-build]"
assert_contains "$bundle" "needs: assemble"
assert_absent "$bundle" "    if: startsWith(github.ref, 'refs/tags/bundle-v')"
assert_single_write_job "$bundle"
assert_no_non_publish_write_job "$bundle"
assert_versioned_actions "$bundle"

# Windows tools：只钉安全边界和真实执行链；实现脚本可独立演进。
[[ -f "$windows" ]] || die "$windows is missing"
for needle in   "workflow_call:"   "workflow_dispatch:"   "contents: read"   "runs-on: windows-2022"   "server: ['2022', '2025']"   "needs: lock-check"   "needs: build"   "needs: package"   "name: BUILD windows_amd64"   "name: PACKAGE windows_amd64"   "ecs-windows-tools-stage-windows_amd64"   "ecs-windows-tools-corpus-windows_amd64"   "ecs-windows-tools-dist"   "scripts/package.sh --tools-stage tools-stage --target windows_amd64"   "scripts/cross.sh --target windows_amd64"   "scripts/ci/windows_tools_package.sh --output-dir .ci/windows-tools-dist/bundle --corpus-path corpus-artifact/ecs-silesia-v1.corpus"   "CorpusOutputPath"   "CURRENT_COMMIT"   "run.ps1"   "install.ps1"   "if-no-files-found: error"   "actions/download-artifact@"; do
  assert_contains "$windows" "$needle"
done
assert_contains "$windows" 'runs-on: windows-${{ matrix.server }}'
assert_contains "$windows" 'name: E2E-${{ matrix.server }}'
assert_absent "$windows" "contents: write"
assert_versioned_actions "$windows"
assert_no_core_gate_bypass "$windows"
assert_no_sensitive_token_evasion "$windows"
assert_absent "$windows" "releases/latest/download"
assert_absent "$windows" "ordinary-user execution evidence"
assert_absent "$windows" "tools/lock.json"
assert_absent "$windows" "GateInputs"
assert_absent "$windows" "VERIFY-"
assert_absent "$windows" "integration-windows"
assert_absent "$windows" "jq -er"
assert_absent "$windows" "silesia.zip"
assert_absent "$windows" "sha256sum --check"

for script in "$runtime_contract" "$tools_prepare" "$tools_gate" "$tools_integration" "$tools_e2e" "$tools_package"; do
  [[ -f "$script" ]] || die "$script is missing"
  assert_no_core_gate_bypass "$script"
  assert_no_sensitive_token_evasion "$script"
  assert_no_trusted_root_mutation "$script"
done
for script in "$nexttrace_report_assert" "$icmp_prerequisite"; do
  [[ -f "$script" ]] || die "$script is missing"
done

# The packaged E2E owns the Windows runtime and NextTrace report contracts.
assert_contains "$tools_e2e" 'windows_nexttrace_capability.ps1'
assert_contains "$tools_e2e" 'windows_asset_sha256.amd64'
assert_contains "$tools_e2e" '-Family IPv4'
assert_contains "$tools_e2e" '-MaxHops 12'
assert_contains "$tools_e2e" 'windows_nexttrace_report_assert.ps1'
assert_contains "$tools_e2e" '-Module route'
assert_contains "$tools_e2e" '-Module backtrace'
assert_contains "$tools_e2e" 'windows_runtime_contract.ps1'
assert_contains "$tools_e2e" 'windows_tools_integration.ps1'
assert_contains "$tools_e2e" 'Test-EcsGlobalIPv6Capability'
assert_contains "$tools_e2e" '--ip-version 6'
assert_contains "$tools_e2e" '-CapabilityAwareIPv6'
assert_contains "$tools_e2e" 'Get-NetIPAddress'
assert_contains "$tools_e2e" 'Get-NetRoute'
assert_contains "$tools_e2e" 'ecs.report/v1'
assert_contains "$nexttrace_capability" '$CanonicalMaxHops = 12'
assert_contains "$nexttrace_capability" '--json'
assert_contains "$nexttrace_capability" '--no-color'
assert_contains "$nexttrace_capability" 'trusted pin and observed file digest'
assert_contains "$tools_e2e" 'ExpectedSha256 $expectedNextTraceSha256'
assert_contains "$tools_e2e" 'NextTrace IPv6 gate: not-tested capability=missing'
for script in "$nexttrace_report_assert" "$icmp_prerequisite"; do
  assert_no_core_gate_bypass "$script"
done

assert_no_product_tls_bypass "$run_ps1"
assert_no_product_tls_bypass "$install_ps1"
assert_no_trusted_root_mutation "$windows"
assert_no_trusted_root_mutation "$run_ps1"
assert_no_trusted_root_mutation "$install_ps1"

# CI keeps one aggregate Windows reusable check; that workflow owns both runner versions.
for needle in   "name: windows-tools"   "uses: ./.github/workflows/windows-tools.yml"   "needs: [unit, compat, quality, integration, race, cross, freebsd, windows-tools, submissions]"   "WINDOWS_TOOLS_RESULT"; do
  assert_contains "$ci" "$needle"
done
assert_absent "$ci" "windows-runtime"
assert_absent "$ci" "contents: write"
assert_versioned_actions "$ci"
assert_no_core_gate_bypass "$ci"

# SDK and release scripts keep their own explicit rehearsal/publish boundaries.
assert_contains "$sdk" "release_mode:"
assert_contains "$sdk" "default: rehearsal"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'rehearsal'"
assert_contains "$sdk" "if: github.event_name == 'workflow_dispatch' && inputs.release_mode == 'publish'"
assert_contains "$sdk" "Prepare SDK release candidate"
assert_contains "$sdk" "sort -V"
assert_contains "$sdk" "gh release create"
assert_single_write_job "$sdk"
assert_versioned_actions "$sdk"

sdk_version_block=$(grep -A4 -F '      sdk_version:' "$sdk")
if ! grep -Fq 'required: false' <<<"$sdk_version_block"; then
  die "$sdk sdk_version must be optional for rehearsal"
fi

dispatch_line=$(grep -nE '^[[:space:]]+workflow_dispatch\)' "$freeze" | head -1 | cut -d: -f1)
push_line=$(grep -nE '^[[:space:]]+push\)' "$freeze" | head -1 | cut -d: -f1)
if [[ -z "$dispatch_line" || -z "$push_line" || "$dispatch_line" -ge "$push_line" ]]; then
  die "freeze.sh must classify workflow_dispatch before push/tag parsing"
fi

assert_contains "$publisher" "--check-only"
check_only_line=$(grep -nE '^[[:space:]]*if \[\[ "\$check_only" -eq 1 \]\]' "$publisher" | tail -1 | cut -d: -f1)
gh_line=$(grep -nE '^[[:space:]]*command -v gh ' "$publisher" | head -1 | cut -d: -f1)
if [[ -z "$check_only_line" || -z "$gh_line" || "$check_only_line" -ge "$gh_line" ]]; then
  die "publish.sh check-only must return before gh becomes a dependency"
fi

echo "release-rehearsal-contract: all rehearsal/publish boundaries are pinned"
