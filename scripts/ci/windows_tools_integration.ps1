[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('2022', '2025')][string]$Server,
    [Parameter(Mandatory)][string]$StageArtifactRoot,
    [Parameter(Mandatory)][string]$GateInputsRoot
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Get-PathEvidence {
  param([AllowNull()][string]$Value)
  $present = $null -ne $Value
  $text = if ($present) { $Value } else { '' }
  $sha = [Security.Cryptography.SHA256]::Create()
  try {
    $digest = [BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($text))).Replace('-', '').ToLowerInvariant()
  } finally {
    $sha.Dispose()
  }
  return [ordered]@{
    present = $present
    length = $text.Length
    sha256 = $digest
    value = $text
  }
}

function Get-PathSnapshot {
  $processPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Process)
  return [ordered]@{
    process = Get-PathEvidence $processPath
    machine = Get-PathEvidence (Get-RawWindowsPath -Target Machine)
    user = Get-PathEvidence (Get-RawWindowsPath -Target User)
  }
}

function Write-PathSnapshotEvidence {
  param(
    [Parameter(Mandatory)][string]$Label,
    [Parameter(Mandatory)][object]$Snapshot
  )
  Write-Host ("PATH evidence ${Label}: " + ($Snapshot | ConvertTo-Json -Compress))
}

function Get-PathSnapshotDifferences {
  param(
    [Parameter(Mandatory)][object]$Expected,
    [Parameter(Mandatory)][object]$Actual
  )
  $differences = @()
  foreach ($scope in @('process', 'machine', 'user')) {
    $expectedValue = $Expected[$scope]
    $actualValue = $Actual[$scope]
    if ($expectedValue.present -ne $actualValue.present -or
        $expectedValue.length -ne $actualValue.length -or
        $expectedValue.sha256 -cne $actualValue.sha256 -or
        $expectedValue.value -cne $actualValue.value) {
      $differences += ("{0}: before(length={1},sha256={2}) after(length={3},sha256={4})" -f
        $scope, $expectedValue.length, $expectedValue.sha256, $actualValue.length, $actualValue.sha256)
    }
  }
  return $differences
}

function Get-RawWindowsPath {
  param([Parameter(Mandatory)][EnvironmentVariableTarget]$Target)
  $registryRoot = if ($Target -eq [EnvironmentVariableTarget]::Machine) {
    [Microsoft.Win32.Registry]::LocalMachine
  } else {
    [Microsoft.Win32.Registry]::CurrentUser
  }
  $subKeyName = if ($Target -eq [EnvironmentVariableTarget]::Machine) {
    'SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
  } else {
    'Environment'
  }
  $registryKey = $registryRoot.OpenSubKey($subKeyName, $false)
  if ($null -eq $registryKey) { return $null }
  try {
    $value = $registryKey.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    if ($null -eq $value) { return $null }
    return [string]$value
  } finally {
    $registryKey.Dispose()
  }
}

$pathBefore = Get-PathSnapshot
Write-PathSnapshotEvidence -Label 'before integration' -Snapshot $pathBefore
$stageRoot = $null
$corpusRoot = $null
$sandboxRoot = $null
$stageArtifactRoot = [IO.Path]::GetFullPath($StageArtifactRoot)
$gateInputsRoot = [IO.Path]::GetFullPath($GateInputsRoot)
$pathBeforeWorkload = $null
$testExitCode = 1
$testError = $null
$cleanupErrors = @()
try {
  $stageItems = @(Get-ChildItem -LiteralPath $stageArtifactRoot -Directory -Filter 'windows_amd64' -Recurse)
  if ($stageItems.Count -ne 1) { throw 'downloaded Windows stage is missing or ambiguous' }
  $sourceStage = $stageItems[0].FullName
  $stageRoot = Join-Path $env:RUNNER_TEMP ('ecs frozen tools stage 数据 ' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $stageRoot | Out-Null
  foreach ($item in @(Get-ChildItem -LiteralPath $sourceStage -Force)) {
    Copy-Item -LiteralPath $item.FullName -Destination $stageRoot -Recurse -Force
  }
  $toolBin = Join-Path $stageRoot 'bin'
  if (-not (Test-Path -LiteralPath $toolBin -PathType Container)) { throw 'staged bundle bin directory is missing' }
  $manifestPath = Join-Path $stageRoot 'manifest.json'
  if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'staged bundle manifest.json is missing' }
  $licensesRoot = Join-Path $stageRoot 'LICENSES'
  if (-not (Test-Path -LiteralPath $licensesRoot -PathType Container)) { throw 'staged bundle LICENSES directory is missing' }
  $stageFiles = @(Get-ChildItem -LiteralPath $stageRoot -File -Recurse -Force)
  if ($stageFiles.Count -eq 0) { throw 'staged bundle contains no regular files' }
  foreach ($stageFile in $stageFiles) {
    $stageFile.IsReadOnly = $true
  }
  $stageFiles = @(Get-ChildItem -LiteralPath $stageRoot -File -Recurse -Force)
  foreach ($stageFile in $stageFiles) {
    if (-not $stageFile.IsReadOnly) { throw "staged bundle regular file is writable: $($stageFile.FullName)" }
  }
  $licenseFiles = @(Get-ChildItem -LiteralPath $licensesRoot -File -Recurse -Force)
  if ($licenseFiles.Count -eq 0) { throw 'staged bundle LICENSES directory contains no regular files' }
  if (-not (Get-Item -LiteralPath $manifestPath).IsReadOnly) { throw 'staged bundle manifest.json is writable' }
  foreach ($tool in @('zstd.exe', 'npb-ep.exe', 'npb-ft.exe', 'openssl.exe', 'stream.exe', 'fio.exe', 'nexttrace-tiny.exe')) {
    $toolPath = Join-Path $toolBin $tool
    if (-not (Test-Path -LiteralPath $toolPath -PathType Leaf)) { throw "staged bundle is missing $tool" }
    if (-not (Get-Item -LiteralPath $toolPath).IsReadOnly) { throw "staged bundle $tool is writable" }
  }

  $lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
  $corpusItems = @(Get-ChildItem -LiteralPath $gateInputsRoot -File -Filter ([string]$lock.corpus.name) -Recurse)
  if ($corpusItems.Count -ne 1) { throw 'fixed zstd corpus is missing or ambiguous' }
  $corpusRoot = Join-Path $env:RUNNER_TEMP ('ecs frozen corpus ' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $corpusRoot | Out-Null
  $corpusPath = Join-Path $corpusRoot ([string]$lock.corpus.name)
  Copy-Item -LiteralPath $corpusItems[0].FullName -Destination $corpusPath -Force

  & ./scripts/ci/windows_tools_gate.ps1 -CheckOrdinaryUser
  $pathAfterGate = Get-PathSnapshot
  Write-PathSnapshotEvidence -Label 'after no-admin-operation gate' -Snapshot $pathAfterGate
  $gatePathDifferences = @(Get-PathSnapshotDifferences -Expected $pathBefore -Actual $pathAfterGate)
  if ($gatePathDifferences.Count -ne 0) {
    throw ("no-admin-operation gate changed PATH: " + ($gatePathDifferences -join '; '))
  }

  $sandboxRoot = Join-Path $env:RUNNER_TEMP ('ecs integration sandbox ' + [guid]::NewGuid().ToString('N'))
  $tempRoot = Join-Path $sandboxRoot 'temp with spaces'
  $homeRoot = Join-Path $sandboxRoot 'home with spaces'
  $cacheRoot = Join-Path $sandboxRoot 'cache with spaces'
  New-Item -ItemType Directory -Force -Path $tempRoot, $homeRoot, $cacheRoot, (Join-Path $cacheRoot 'mod') | Out-Null
  $env:TEMP = $tempRoot
  $env:TMP = $tempRoot
  $env:TMPDIR = $tempRoot
  $env:HOME = $homeRoot
  $env:USERPROFILE = $homeRoot
  $env:GOCACHE = $cacheRoot
  $env:GOMODCACHE = Join-Path $cacheRoot 'mod'
  $env:ECS_TOOL_BIN = $toolBin
  $env:ECS_ZSTD_CORPUS = $corpusPath

  $pathBeforeWorkload = Get-PathSnapshot
  Write-PathSnapshotEvidence -Label 'before production workload' -Snapshot $pathBeforeWorkload
  $preparationPathDifferences = @(Get-PathSnapshotDifferences -Expected $pathBefore -Actual $pathBeforeWorkload)
  if ($preparationPathDifferences.Count -ne 0) {
    throw ("integration preparation changed PATH: " + ($preparationPathDifferences -join '; '))
  }

  & go test -tags=integration ./internal/probe -run '^TestIntegrationWindowsFrozenTools$' -count=1 -v
  $testExitCode = $LASTEXITCODE
} catch {
  $testError = $_
} finally {
  foreach ($cleanupTarget in @(
    [pscustomobject]@{ Label = 'sandbox'; Path = $sandboxRoot },
    [pscustomobject]@{ Label = 'corpus'; Path = $corpusRoot },
    [pscustomobject]@{ Label = 'stage'; Path = $stageRoot },
    [pscustomobject]@{ Label = 'stage artifact'; Path = $stageArtifactRoot },
    [pscustomobject]@{ Label = 'gate inputs'; Path = $gateInputsRoot }
  )) {
    if ($null -ne $cleanupTarget.Path -and (Test-Path -LiteralPath $cleanupTarget.Path)) {
      try {
        Remove-Item -LiteralPath $cleanupTarget.Path -Recurse -Force -ErrorAction Stop
      } catch {
        $cleanupErrors += "$($cleanupTarget.Label) cleanup failed: $($_.Exception.Message)"
      }
    }
    if ($null -ne $cleanupTarget.Path -and (Test-Path -LiteralPath $cleanupTarget.Path)) {
      $cleanupErrors += "$($cleanupTarget.Label) remains: $($cleanupTarget.Path)"
    }
  }
}
$postErrors = @()
$pathAfter = Get-PathSnapshot
Write-PathSnapshotEvidence -Label 'after integration and cleanup' -Snapshot $pathAfter
$stepPathDifferences = @(Get-PathSnapshotDifferences -Expected $pathBefore -Actual $pathAfter)
if ($stepPathDifferences.Count -ne 0) {
  $postErrors += ("Windows ECS frozen-tool integration changed process, Machine, or User PATH: " + ($stepPathDifferences -join '; '))
}
if ($null -ne $pathBeforeWorkload) {
  $workloadPathDifferences = @(Get-PathSnapshotDifferences -Expected $pathBeforeWorkload -Actual $pathAfter)
  if ($workloadPathDifferences.Count -ne 0) {
    $postErrors += ("Windows ECS production workload changed process, Machine, or User PATH: " + ($workloadPathDifferences -join '; '))
  }
}
$postErrors += $cleanupErrors
if ($null -ne $testError) {
  $postErrors += "Windows ECS frozen-tool integration raised an exception: $($testError.Exception.Message)"
} elseif ($testExitCode -ne 0) {
  $postErrors += "Windows ECS frozen-tool integration failed with exit code $testExitCode"
}
if ($postErrors.Count -ne 0) { throw ($postErrors -join '; ') }
Write-Output "Windows ECS frozen-tool integration passed on Server ${Server}: ECS_TOOL_BIN was the read-only staged bundle; hostile PATH, sandbox, production probes, parsers, workloads, raw evidence, and PATH isolation were verified"
