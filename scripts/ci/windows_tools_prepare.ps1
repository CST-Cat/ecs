[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$paths = @(
  (Resolve-Path 'scripts/build_tools_windows.ps1').Path,
  (Resolve-Path 'scripts/ci/windows_tools_gate.ps1').Path,
  (Resolve-Path 'scripts/ci/windows_nexttrace_report_assert.ps1').Path,
  (Resolve-Path 'scripts/ci/windows_icmp_prerequisite.ps1').Path,
  (Resolve-Path 'scripts/tools/windows/common.ps1').Path
)
$paths += @(Get-ChildItem 'scripts/tools/windows' -Filter '*.ps1' -File | ForEach-Object { $_.FullName })
$paths += @(Get-ChildItem 'scripts/ci' -Filter '*.ps1' -File | ForEach-Object { $_.FullName })
foreach ($path in $paths | Sort-Object -Unique) {
  $tokens = $null
  $errors = $null
  [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors) | Out-Null
  if ($errors.Count -ne 0) {
    throw "PowerShell parse failed for ${path}: $($errors -join '; ')"
  }
}

$lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
if ([string]$lock.schema_version -ne 'ecs.tools.lock/v1') { throw 'unexpected tools lock schema' }
$windowsTarget = @($lock.architectures | Where-Object { $_.target -eq 'windows_amd64' })
if ($windowsTarget.Count -ne 1 -or [string]$windowsTarget[0].goos -ne 'windows' -or [string]$windowsTarget[0].goarch -ne 'amd64') {
  throw 'windows_amd64 lock target is missing or ambiguous'
}
$wantTools = @('zstd', 'npb-ep', 'npb-ft', 'openssl', 'stream', 'fio', 'nexttrace-tiny')
if ((@($lock.windows_tools) -join '|') -ne ($wantTools -join '|')) { throw 'Windows tool set drifted' }
if (@($lock.windows_tools | Where-Object { $_ -eq 'ping' -or $_ -eq 'sysbench' -or $_ -eq 'iperf3' }).Count -ne 0) {
  throw 'unsupported tool entered the Windows bundle'
}
$nexttrace = @($lock.tools | Where-Object { $_.name -eq 'nexttrace-tiny' })
if ($nexttrace.Count -ne 1) { throw 'NextTrace lock entry is missing or duplicated' }
if ([string]$nexttrace[0].windows_asset_pattern -ne 'nexttrace-tiny_windows_<architecture>.exe') { throw 'NextTrace Windows asset pattern drifted' }
$nexttraceAsset = ([string]$nexttrace[0].windows_asset_pattern).Replace('<architecture>', 'amd64')
$nexttraceReleasePath = 'releases' + '/download'
$nexttraceURL = "https://github.com/$($nexttrace[0].repository)/$nexttraceReleasePath/$($nexttrace[0].tag)/$nexttraceAsset"
$expectedNexttraceURL = 'https://github.com/nxtrace/NTrace-core/' + $nexttraceReleasePath + '/v1.7.1/nexttrace-tiny_windows_amd64.exe'
if ($nexttraceURL -ne $expectedNexttraceURL) { throw 'NextTrace source URL drifted' }
if (@($lock.windows_dll_allowlist).Count -eq 0) { throw 'Windows DLL allowlist is empty' }
Write-Output 'LOCK/CHECK passed: pinned target, seven-tool set, verified NextTrace asset metadata, allowlist, and PowerShell syntax'
