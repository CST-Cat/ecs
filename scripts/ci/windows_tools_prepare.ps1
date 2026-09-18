[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Check', 'GateInputs')][string]$Action,
    [string]$InputRoot = '',
    [string]$DownloadRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Action -ceq 'Check') {
$paths = @(
  (Resolve-Path 'scripts/build_tools_windows.ps1').Path,
  (Resolve-Path 'scripts/ci/windows_tools_gate.ps1').Path,
  (Resolve-Path 'scripts/ci/windows_nexttrace_gate.ps1').Path,
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
if ([string]$nexttrace[0].windows_asset_sha256.amd64 -ne '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b') { throw 'NextTrace Windows AMD64 SHA-256 drifted' }
if (@($lock.windows_dll_allowlist).Count -eq 0) { throw 'Windows DLL allowlist is empty' }

& ./scripts/ci/windows_tools_gate.ps1 -CheckOrdinaryUser
$ordinaryUserSucceeded = $?
$ordinaryUserExitCode = $LASTEXITCODE
if (-not $ordinaryUserSucceeded -or $ordinaryUserExitCode -ne 0) { throw "no-admin-operation check failed with exit code $ordinaryUserExitCode" }

$params = & ./scripts/build_tools_windows.ps1 -Target windows_amd64 -PrintParams
$builderSucceeded = $?
$builderExitCode = $LASTEXITCODE
if (-not $builderSucceeded -or $builderExitCode -ne 0) { throw "Windows builder lock/check failed with exit code $builderExitCode" }
$paramsText = $params -join "`n"
foreach ($marker in @('target=windows_amd64', 'toolchain_mode=native', 'smoke_runner=direct', 'validation_scope=functional', 'performance_valid=false', 'nexttrace=verified-upstream-prebuilt', 'nexttrace_asset=nexttrace-tiny_windows_amd64.exe', 'nexttrace_source_sha256=16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b')) {
  if ($paramsText -notmatch [regex]::Escape($marker)) { throw "builder parameters missing $marker" }
}
Write-Output 'LOCK/CHECK passed: pinned target, seven-tool set, verified NextTrace asset metadata, allowlist, PowerShell syntax, and no-admin-operation contract (-CheckOrdinaryUser validates no-admin-operation only; ordinary-user token execution not claimed)'

} else {
    if ([string]::IsNullOrWhiteSpace($InputRoot)) {
        $InputRoot = Join-Path $PWD '.ci/windows-tools-gate-inputs'
    }
    if ([string]::IsNullOrWhiteSpace($DownloadRoot)) {
        $DownloadRoot = Join-Path $env:RUNNER_TEMP 'ecs-windows-gate-downloads'
    }
    $lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
    $inputRoot = [IO.Path]::GetFullPath($InputRoot)
    $downloadRoot = [IO.Path]::GetFullPath($DownloadRoot)
    New-Item -ItemType Directory -Force -Path (Join-Path $inputRoot 'corpus'), (Join-Path $inputRoot 'inspector'), $downloadRoot | Out-Null

    $corpusZip = Join-Path $downloadRoot 'silesia.zip'
    Invoke-WebRequest -UseBasicParsing -Uri $lock.corpus.source_url -OutFile $corpusZip
    if ((Get-FileHash -Algorithm SHA256 $corpusZip).Hash.ToLowerInvariant() -ne [string]$lock.corpus.source_sha256) { throw 'Silesia source hash mismatch' }
    $corpusExtract = Join-Path $downloadRoot 'silesia'
    Expand-Archive -LiteralPath $corpusZip -DestinationPath $corpusExtract -Force
    $corpus = Join-Path $inputRoot ('corpus\' + [string]$lock.corpus.name)
    $output = [IO.File]::Open($corpus, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
      foreach ($member in @($lock.corpus.order)) {
        $memberPath = Join-Path $corpusExtract $member
        if (-not (Test-Path -LiteralPath $memberPath -PathType Leaf)) { throw "Silesia member missing: $member" }
        $input = [IO.File]::OpenRead($memberPath)
        try { $input.CopyTo($output) } finally { $input.Dispose() }
      }
    } finally { $output.Dispose() }
    if ((Get-Item $corpus).Length -ne [int64]$lock.corpus.bytes -or (Get-FileHash -Algorithm SHA256 $corpus).Hash.ToLowerInvariant() -ne [string]$lock.corpus.sha256) { throw 'fixed Silesia corpus mismatch' }

    $inspectorPackages = @(
      'mingw-w64-ucrt-x86_64-binutils',
      'mingw-w64-ucrt-x86_64-gettext-runtime',
      'mingw-w64-ucrt-x86_64-zlib',
      'mingw-w64-ucrt-x86_64-zstd',
      'mingw-w64-ucrt-x86_64-libiconv'
    )
    foreach ($packageName in $inspectorPackages) {
      $package = @($lock.windows_toolchain.packages | Where-Object { $_.name -eq $packageName })
      if ($package.Count -ne 1) { throw "locked inspector package is missing: $packageName" }
      $packageArchive = Join-Path $downloadRoot ($packageName + '.pkg.tar.zst')
      Invoke-WebRequest -UseBasicParsing -Uri $package[0].source_url -OutFile $packageArchive
      if ((Get-FileHash -Algorithm SHA256 $packageArchive).Hash.ToLowerInvariant() -ne [string]$package[0].source_sha256) { throw "package hash mismatch: $packageName" }
      & (Get-Command tar.exe -ErrorAction Stop).Source -xf $packageArchive -C (Join-Path $inputRoot 'inspector')
      if ($LASTEXITCODE -ne 0) { throw "locked package extraction failed: $packageName" }
    }
    $objdump = Join-Path $inputRoot 'inspector\ucrt64\bin\objdump.exe'
    if (-not (Test-Path -LiteralPath $objdump -PathType Leaf)) { throw 'locked objdump is missing' }
}
