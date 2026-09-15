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
    die "$file contains forbidden contract: $needle"
  fi
}

assert_absent_regex() {
  local file=$1 pattern=$2
  if grep -Eiq -- "$pattern" "$file"; then
    die "$file contains forbidden contract pattern: $pattern"
  fi
}

assert_exact_count() {
  local file=$1 needle=$2 expected=$3 count
  count=$(awk -v needle="$needle" 'index($0, needle) { count++ } END { print count + 0 }' "$file")
  [[ "$count" -eq "$expected" ]] ||
    die "$file must contain $expected lines with contract $needle, got $count"
}

get_section_bounds() {
  local file=$1 start_marker=$2 end_marker=$3
  local start_line end_line

  start_line=$(awk -v marker="$start_marker" '$0 == marker { print NR; exit }' "$file")
  [[ -n "$start_line" ]] || die "$file missing contract section start: $start_marker"

  if [[ -n "$end_marker" ]]; then
    end_line=$(awk -v marker="$end_marker" -v start="$start_line" \
      'NR > start && $0 == marker { print NR; exit }' "$file")
    [[ -n "$end_line" ]] || die "$file missing contract section end: $end_marker"
  else
    end_line=$(( $(wc -l < "$file") + 1 ))
  fi

  printf '%s %s\n' "$start_line" "$end_line"
}

assert_section_contains() {
  local file=$1 start_marker=$2 end_marker=$3 needle=$4
  local bounds start_line end_line
  bounds=$(get_section_bounds "$file" "$start_marker" "$end_marker")
  read -r start_line end_line <<<"$bounds"
  awk -v start="$start_line" -v end="$end_line" -v needle="$needle" \
    'NR > start && NR < end && index($0, needle) { found=1; exit } END { exit !found }' \
    "$file" || die "$file section $start_marker missing contract: $needle"
}

assert_section_count() {
  local file=$1 start_marker=$2 end_marker=$3 needle=$4 expected=$5
  local bounds start_line end_line count
  bounds=$(get_section_bounds "$file" "$start_marker" "$end_marker")
  read -r start_line end_line <<<"$bounds"
  count=$(awk -v start="$start_line" -v end="$end_line" -v needle="$needle" \
    'NR > start && NR < end && index($0, needle) { count++ } END { print count + 0 }' \
    "$file")
  [[ "$count" -eq "$expected" ]] ||
    die "$file section $start_marker must contain $expected lines with contract $needle, got $count"
}

assert_ordered_in_section() {
  local file=$1 start_marker=$2 end_marker=$3 marker
  local bounds start_line end_line previous line
  shift 3
  bounds=$(get_section_bounds "$file" "$start_marker" "$end_marker")
  read -r start_line end_line <<<"$bounds"
  previous=$start_line
  for marker in "$@"; do
    line=$(awk -v start="$previous" -v end="$end_line" -v needle="$marker" \
      'NR > start && NR < end && index($0, needle) { print NR; exit }' "$file")
    [[ -n "$line" ]] || die "$file section $start_marker missing ordered contract: $marker"
    previous=$line
  done
}

assert_only_exact_line() {
  local file=$1 needle=$2 expected=$3 violation
  violation=$(awk -v needle="$needle" -v expected="$expected" '
    {
      line=$0
      sub(/^[[:space:]]*/, "", line)
      if (index($0, needle) && line != expected) {
        print NR ":" $0
        exit
      }
    }
  ' "$file")
  [[ -z "$violation" ]] || die "$file uses $needle outside its direct contract line: $violation"
}

assert_no_core_gate_bypass() {
  local file=$1
  assert_absent_regex "$file" '^[[:space:]]*(if|continue-on-error|skipped)[[:space:]]*:'
  assert_absent_regex "$file" '^[[:space:]]*condition[[:space:]]*:'
  assert_absent_regex "$file" 'skipped'
  assert_absent_regex "$file" '^[[:space:]]*if[[:space:]]*[(][^[:cntrl:]]*(skip|skipped)'
  assert_absent_regex "$file" '^[[:space:]]*(skip|skipped)[[:space:]]*[:=]'
  assert_absent_regex "$file" '^[[:space:]]*\$(skip|skipped)[[:space:]]*='
  assert_absent_regex "$file" '^[[:space:]]*(exit|return)[[:space:]]+0([[:space:]]|$)'
  assert_absent "$file" '|| true'
}

assert_no_sensitive_token_evasion() {
  local file=$1
  assert_absent_regex "$file" '\[char\]'
  assert_absent_regex "$file" '\[byte[[:space:]]*\[\]\]|GetString[[:space:]]*[(][^)]*byte'
  assert_absent_regex "$file" '(FromBase64String|ToBase64String|Base64)'
  assert_absent_regex "$file" '(SkipCertificateCheck|CertificateCheck)[^[:cntrl:]]*(Replace|Substring)|(Replace|Substring)[^[:cntrl:]]*(SkipCertificateCheck|CertificateCheck)'
  assert_absent_regex "$file" '(Skip|CertificateCheck)[^[:cntrl:]]*[+][^[:cntrl:]]*(Skip|CertificateCheck)'
  assert_absent_regex "$file" '(Skip|CertificateCheck)[^[:cntrl:]]*-[[:space:]]*f|-[[:space:]]*f[^[:cntrl:]]*(Skip|CertificateCheck)'
  assert_absent_regex "$file" "'Skip'[[:space:]]*[+][[:space:]]*'CertificateCheck"
  assert_absent_regex "$file" "'S'[[:space:]]*[+][[:space:]]*'kipCertificateCheck"
  assert_absent_regex "$file" 'part[[:alnum:]_]*[[:space:]]*[+][[:space:]]*part[[:alnum:]_]*'
  assert_absent_regex "$file" '(SkipCertificateCheck|CertificateCheck)[^[:cntrl:]]*(env:|GetEnvironmentVariable|SetEnvironmentVariable)|(env:|GetEnvironmentVariable|SetEnvironmentVariable)[^[:cntrl:]]*(SkipCertificateCheck|CertificateCheck)'
  assert_absent_regex "$file" '(Skip|CertificateCheck)[^[:cntrl:]]*(env:|GetEnvironmentVariable|SetEnvironmentVariable)|(env:|GetEnvironmentVariable|SetEnvironmentVariable)[^[:cntrl:]]*(Skip|CertificateCheck)'
  assert_absent_regex "$file" '^[[:space:]]*#.*(Import-Certificate|SkipCertificateCheck|CertificateCheck|X509Store|certutil)'
}

assert_e2e_bootstrap_contract() {
  local start_marker=$1 end_marker=$2

  assert_section_count "$windows" "$start_marker" "$end_marker" 'Assert-ArtifactChecksum -ChecksumFile' 3
  assert_section_count "$windows" "$start_marker" "$end_marker" 'Invoke-WebRequest:SkipCertificateCheck' 1
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    'if ((Get-Content -Raw -LiteralPath $commitFile.FullName).Trim() -cne [string]$env:GITHUB_SHA)'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    'bootstrap artifact does not identify the current workflow commit'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    '$mainMembers | Sort-Object'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    'main Windows ZIP member set changed'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (-not \$releaseBaseUri.IsAbsoluteUri -or \$releaseBaseUri.Scheme -cne 'https' -or \$releaseBaseUri.Host -cne 'localhost' -or \$releaseBaseUri.Port -ne \$port)"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (-not \$bundleReleaseBaseUri.IsAbsoluteUri -or \$bundleReleaseBaseUri.Scheme -cne 'https' -or \$bundleReleaseBaseUri.Host -cne 'localhost' -or \$bundleReleaseBaseUri.Port -ne \$port)"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "[string]\$requiredTools[0] -cne 'zstd'"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    'install.ps1 unexpectedly accepted protected install target'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    'protected install did not preserve the expected path rejection'
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (\$zstdResults.Count -ne 1 -or [string]\$zstdResults[0].status -eq 'error') { throw 'run.ps1 did not execute the only required zstd tool' }"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if ([string]\$env:ECS_TOOL_BIN -cne \$sentinelToolBin) { throw 'run.ps1 did not restore ECS_TOOL_BIN' }"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (\$machinePathBaseline -cne \$machinePathAfterRun -or \$userPathBaseline -cne \$userPathAfterRun) { throw 'run.ps1 changed the Machine or User PATH' }"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (\$machinePathBaseline -cne \$machinePathAfterInstall -or \$userPathBaseline -cne \$userPathAfterInstall) { throw 'install.ps1 changed the Machine or User PATH' }"
  assert_section_contains "$windows" "$start_marker" "$end_marker" \
    "if (\$machinePathBaseline -cne \$machinePathAfterVersion -or \$userPathBaseline -cne \$userPathAfterVersion) { throw 'installed ecs.exe --version changed the Machine or User PATH' }"

  assert_ordered_in_section "$windows" "$start_marker" "$end_marker" \
    '$commitFile = Get-ArtifactFile' \
    'if ((Get-Content -Raw -LiteralPath $commitFile.FullName).Trim() -cne [string]$env:GITHUB_SHA)' \
    'bootstrap artifact does not identify the current workflow commit' \
    '$mainArchive = Get-ArtifactFile' \
    '$bundleArchive = Get-ArtifactFile' \
    '$corpusArchive = Get-ArtifactFile' \
    '$runScript = Get-ArtifactFile' \
    '$installScript = Get-ArtifactFile' \
    'Assert-ArtifactChecksum -ChecksumFile $mainChecksums' \
    'Assert-ArtifactChecksum -ChecksumFile $bundleChecksums -AssetFile $bundleArchive' \
    'Assert-ArtifactChecksum -ChecksumFile $bundleChecksums -AssetFile $corpusArchive' \
    '[IO.Compression.ZipFile]::OpenRead' \
    "\$expectedMainMembers = @('ecs.exe', 'LICENSE', 'NOTICE', 'README.md', 'README_EN.md', 'SECURITY.md', 'THIRD_PARTY.md')" \
    'if ((@($mainMembers | Sort-Object) -join "`n") -cne (@($expectedMainMembers | Sort-Object) -join "`n"))' \
    'Expand-Archive -LiteralPath $mainArchive.FullName' \
    '$planOutput = @(& $ecsPath plan' \
    '$requiredTools = @($plan.required_tools)' \
    'only-required-tools plan drifted' \
    '$certificate = New-SelfSignedCertificate' \
    '$serverJob = Start-Job -ScriptBlock $serverScript' \
    '$mainBase = "https://localhost:$port/main"' \
    '$bundleBase = "https://localhost:$port/bundle"' \
    '$releaseBaseUri = [Uri]::new' \
    '$bundleReleaseBaseUri = [Uri]::new' \
    'ECS_RELEASE_BASE must be the current local HTTPS fixture' \
    'ECS_BUNDLE_RELEASE_BASE must be the current local HTTPS fixture' \
    '$noProxyDefaultWasPresent = $PSDefaultParameterValues.ContainsKey($noProxyParameterName)' \
    '$noProxyDefaultOriginalValue = $null' \
    '$noProxyDefaultOriginalValue = $PSDefaultParameterValues[$noProxyParameterName]' \
    "\$certificateCheckParameterName = 'Invoke-WebRequest:SkipCertificateCheck'" \
    '$certificateCheckDefaultWasPresent = $PSDefaultParameterValues.ContainsKey($certificateCheckParameterName)' \
    '$certificateCheckDefaultOriginalValue = $null' \
    '$certificateCheckDefaultOriginalValue = $PSDefaultParameterValues[$certificateCheckParameterName]' \
    'try {' \
    '$PSDefaultParameterValues[$certificateCheckParameterName] = $true' \
    'Invoke-WebRequest -UseBasicParsing -Uri "$($env:ECS_RELEASE_BASE)/checksums.txt" -OutFile $probeFile -NoProxy -ErrorAction Stop' \
    '& $runScript.FullName --lang en --profile standard --only zstd --exposure any --yes --format json --output $runReportRoot' \
    '$machinePathBaseline -cne $machinePathAfterRun -or $userPathBaseline -cne $userPathAfterRun' \
    'if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin)' \
    '$runReport = Get-Content -Raw -LiteralPath $runReportItem.FullName | ConvertFrom-Json' \
    '$zstdResults = @($runReport.results | Where-Object { [string]$_.id -eq '\''zstd'\'' })' \
    'if ($zstdResults.Count -ne 1 -or [string]$zstdResults[0].status -eq '\''error'\'')' \
    '$runWorkAfter = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter '\''ecs-run-*'\'' | ForEach-Object { $_.FullName })' \
    'run.ps1 left private staging behind' \
    '$protectedInstallTargets = @(' \
    '$protectedInstallSucceeded = $true' \
    "& \$installScript.FullName -Repository 'CST-Cat/ecs' -Version 'phase8-current' -ReleaseBase \$mainBase -InstallDirectory \$protectedInstallTarget" \
    'protected install target was created' \
    'protected install created a temporary work directory' \
    'if ($protectedInstallSucceeded)' \
    'protected install did not preserve the expected path rejection' \
    '$localAppDataFull = [IO.Path]::GetFullPath($env:LOCALAPPDATA)' \
    '$installDirectory = Join-Path $env:LOCALAPPDATA' \
    'install E2E path escaped LOCALAPPDATA\ecs' \
    "& \$installScript.FullName -Repository 'CST-Cat/ecs' -Version 'phase8-current' -ReleaseBase \$mainBase -InstallDirectory \$installDirectory" \
    'install.ps1 failed against the current artifact fixture' \
    '$machinePathBaseline -cne $machinePathAfterInstall -or $userPathBaseline -cne $userPathAfterInstall' \
    '$installedEcs = Join-Path $installDirectory '\''ecs.exe'\''' \
    '$installedVersion = @(& $installedEcs --version 2>&1)' \
    '$machinePathBaseline -cne $machinePathAfterVersion -or $userPathBaseline -cne $userPathAfterVersion' \
    'Remove-Item -LiteralPath $installDirectory -Recurse -Force' \
    '            } finally {' \
    '$PSDefaultParameterValues[$noProxyParameterName] = $noProxyDefaultOriginalValue' \
    'Invoke-WebRequest no-proxy default was not restored exactly' \
    '[void]$PSDefaultParameterValues.Remove($noProxyParameterName)' \
    'Invoke-WebRequest no-proxy default was not removed' \
    'if ($certificateCheckDefaultWasPresent)' \
    '$PSDefaultParameterValues[$certificateCheckParameterName] = $certificateCheckDefaultOriginalValue' \
    'if (-not $PSDefaultParameterValues.ContainsKey($certificateCheckParameterName) -or' \
    'Invoke-WebRequest certificate-check default was not restored exactly' \
    '[void]$PSDefaultParameterValues.Remove($certificateCheckParameterName)' \
    'if ($PSDefaultParameterValues.ContainsKey($certificateCheckParameterName))' \
    'Invoke-WebRequest certificate-check default was not removed' \
    '$cleanupErrors = @()' \
    'Remove-Job -Job $serverJob' \
    'fixture directory remains' \
    '$runWorkAfterFinal = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter '\''ecs-run-*'\'' | ForEach-Object { $_.FullName })' \
    'private staging remains:' \
    'throw ("E2E-$label cleanup failed: " + ($cleanupErrors -join '\''; '\''))'
}

assert_no_trusted_root_mutation() {
  local file=$1 pattern
  local forbidden_patterns=(
    'CurrentUser\\Root'
    'LocalMachine\\Root'
    'Import-Certificate[^[:cntrl:]]*Root'
    'Root[^[:cntrl:]]*Import-Certificate'
    'X509Store[[:space:]]*[(][[:space:]]*[^[:alnum:]_]*Root'
    'X509Store[^[:cntrl:]]*Root'
    '[.]Open[[:space:]]*[(][^)]*ReadWrite'
    '[.]Add[[:space:]]*[(][[:space:]]*rootCertificate[[:space:]]*[)]'
    'certutil[[:space:]]+(-f[[:space:]]+)?-addstore[[:space:]]+Root'
    'certutil[^[:cntrl:]]+-addstore[^[:cntrl:]]+Root'
  )

  for pattern in "${forbidden_patterns[@]}"; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_no_temporary_standard_user_architecture() {
  local file=$1 pattern
  local forbidden_patterns=(
    'New-LocalUser'
    'Remove-LocalUser'
    'PSCredential'
    'Start-Process[^[:cntrl:]]+-Credential'
    '-LoadUserProfile'
    'ordinary-child'
    'ordinary[[:space:]]+user[[:space:]]+fixture'
    'ordinary[[:space:]-]+child'
    'ordinary[[:space:]-]+user[[:space:]-]+fixture'
    'temporary[[:space:]]+user[[:space:]]+(SID|ACL)'
    'temporary[[:space:]-]+user[[:space:]-]+(SID|ACL)'
    'temporary[[:space:]-]+(SID|ACL)[[:space:]-]+(permission|access|fixture)'
    'temporary[[:space:]-]+(SID|ACL)[[:space:]-]+(SID|ACL)'
    'CommonDocuments'
    'FileSystemAccessRule'
    'New-Object[^[:cntrl:]]*(FileSystemAccessRule|NTAccount)'
    'Set-Acl'
    'AddAccessRule'
    'icacls'
  )

  for pattern in "${forbidden_patterns[@]}"; do
    assert_absent_regex "$file" "$pattern"
  done
}

assert_no_product_tls_bypass() {
  local file=$1 pattern
  local forbidden_patterns=(
    'SkipCertificateCheck'
    'ServicePointManager'
    'ServerCertificateValidationCallback'
    'TrustAll'
    'AllowInsecure'
    'NoVerify'
    'certificate[[:space:]-]+bypass'
  )

  for pattern in "${forbidden_patterns[@]}"; do
    assert_absent_regex "$file" "$pattern"
  done
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
run_ps1=run.ps1
install_ps1=install.ps1

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
assert_contains "$windows" "SslStream"
assert_contains "$windows" "ECS_RELEASE_BASE"
assert_contains "$windows" "ECS_BUNDLE_RELEASE_BASE"
assert_contains "$windows" "ECS_TOOL_BIN"
assert_contains "$windows" "LOCALAPPDATA"
assert_contains "$windows" "actions/download-artifact"
assert_no_trusted_root_mutation "$windows"
assert_no_temporary_standard_user_architecture "$windows"
assert_no_core_gate_bypass "$windows"
assert_no_sensitive_token_evasion "$windows"
assert_exact_count "$windows" "Invoke-WebRequest:SkipCertificateCheck" 2
assert_only_exact_line "$windows" "Invoke-WebRequest:SkipCertificateCheck" \
  "\$certificateCheckParameterName = 'Invoke-WebRequest:SkipCertificateCheck'"
[[ -f "$run_ps1" ]] || die "$run_ps1 is missing"
[[ -f "$install_ps1" ]] || die "$install_ps1 is missing"
assert_no_product_tls_bypass "$run_ps1"
assert_no_product_tls_bypass "$install_ps1"

assert_ordered_in_section "$windows" \
  "      - name: E2E-2022 packaged bundle workloads" \
  "      - name: E2E-2022 current ZIP and bootstrap scripts" \
  'Expand-Archive -LiteralPath $archive -DestinationPath $bundle -Force' \
  "foreach (\$tool in @('zstd.exe', 'npb-ep.exe', 'npb-ft.exe', 'stream.exe', 'openssl.exe', 'fio.exe'))" \
  'scripts/ci/windows_tools_gate.ps1 -StageRoot $stage' \
  "if (-not \$?) { throw 'E2E-2022 packaged workload gate failed' }" \
  'E2E-2022 executed zstd, NPB EP, NPB FT, STREAM, OpenSSL, and fio/windowsaio from the packaged bundle; performance_valid=false'
assert_ordered_in_section "$windows" \
  "      - name: E2E-2025 packaged bundle workloads" \
  "      - name: E2E-2025 current ZIP and bootstrap scripts" \
  'Expand-Archive -LiteralPath $archive -DestinationPath $bundle -Force' \
  "foreach (\$tool in @('zstd.exe', 'npb-ep.exe', 'npb-ft.exe', 'stream.exe', 'openssl.exe', 'fio.exe'))" \
  'scripts/ci/windows_tools_gate.ps1 -StageRoot $stage' \
  "if (-not \$?) { throw 'E2E-2025 packaged workload gate failed' }" \
  'E2E-2025 executed zstd, NPB EP, NPB FT, STREAM, OpenSSL, and fio/windowsaio from the packaged bundle; performance_valid=false'
assert_e2e_bootstrap_contract \
  "      - name: E2E-2022 current ZIP and bootstrap scripts" \
  '  e2e-2025:'
assert_e2e_bootstrap_contract \
  "      - name: E2E-2025 current ZIP and bootstrap scripts" \
  ''
assert_absent "$windows" "releases/download"
assert_absent "$windows" "releases/latest/download"
assert_absent "$windows" ")[0].FullName"
for forbidden in 'WSL' 'Cygwin' 'winget' 'choco' 'ping\.exe' 'host PATH'; do
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
