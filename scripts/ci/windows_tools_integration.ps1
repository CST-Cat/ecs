[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('2022', '2025')][string]$Server,
    [Parameter(Mandatory)][string]$StageRoot,
    [Parameter(Mandatory)][string]$CorpusArchivePath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Get-PathEvidence {
  param([AllowNull()][string]$Value)
  return [ordered]@{
    present = $null -ne $Value
    value = $Value
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
        $expectedValue.value -cne $actualValue.value) {
      $differences += ("{0}: before(present={1}) after(present={2})" -f
        $scope, $expectedValue.present, $actualValue.present)
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
$stageCopyRoot = $null
$corpusRoot = $null
$sandboxRoot = $null
$sourceStage = [IO.Path]::GetFullPath($StageRoot)
$corpusArchive = [IO.Path]::GetFullPath($CorpusArchivePath)
$environmentNames = @('TEMP', 'TMP', 'TMPDIR', 'HOME', 'USERPROFILE', 'GOCACHE', 'GOMODCACHE', 'ECS_TOOL_BIN', 'ECS_ZSTD_CORPUS')
$environmentBefore = @{}
foreach ($name in $environmentNames) {
  $environmentBefore[$name] = [pscustomobject]@{
    Present = Test-Path "Env:$name"
    Value = [Environment]::GetEnvironmentVariable($name, [EnvironmentVariableTarget]::Process)
  }
}
$pathBeforeWorkload = $null
$testExitCode = 1
$testError = $null
$cleanupErrors = @()
try {
  if (-not (Test-Path -LiteralPath $sourceStage -PathType Container)) { throw "packaged Windows stage is missing: $sourceStage" }
  if (-not (Test-Path -LiteralPath $corpusArchive -PathType Leaf)) { throw "packaged fixed corpus archive is missing: $corpusArchive" }
  $stageCopyRoot = Join-Path $env:RUNNER_TEMP ('ecs frozen tools stage 数据 ' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $stageCopyRoot | Out-Null
  foreach ($item in @(Get-ChildItem -LiteralPath $sourceStage -Force)) {
    Copy-Item -LiteralPath $item.FullName -Destination $stageCopyRoot -Recurse -Force
  }
  $toolBin = Join-Path $stageCopyRoot 'bin'
  if (-not (Test-Path -LiteralPath $toolBin -PathType Container)) { throw 'staged bundle bin directory is missing' }
  $manifestPath = Join-Path $stageCopyRoot 'manifest.json'
  if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'staged bundle manifest.json is missing' }
  $licensesRoot = Join-Path $stageCopyRoot 'LICENSES'
  if (-not (Test-Path -LiteralPath $licensesRoot -PathType Container)) { throw 'staged bundle LICENSES directory is missing' }
  $stageFiles = @(Get-ChildItem -LiteralPath $stageCopyRoot -File -Recurse -Force)
  if ($stageFiles.Count -eq 0) { throw 'staged bundle contains no regular files' }
  foreach ($stageFile in $stageFiles) {
    $stageFile.IsReadOnly = $true
  }
  $stageFiles = @(Get-ChildItem -LiteralPath $stageCopyRoot -File -Recurse -Force)
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

  $corpusRoot = Join-Path $env:RUNNER_TEMP ('ecs frozen corpus ' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $corpusRoot | Out-Null
  $lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
  $corpusName = [string]$lock.corpus.name
  $tar = (Get-Command tar.exe -ErrorAction Stop).Source
  $archiveMembers = @(& $tar -tzf $corpusArchive 2>&1)
  if ($LASTEXITCODE -ne 0 -or $archiveMembers.Count -ne 1 -or [string]$archiveMembers[0] -cne $corpusName) {
    throw 'packaged fixed zstd corpus archive has an unexpected entry set'
  }
  & $tar -xzf $corpusArchive -C $corpusRoot
  if ($LASTEXITCODE -ne 0) { throw 'packaged fixed zstd corpus archive extraction failed' }
  $corpusPath = Join-Path $corpusRoot $corpusName
  if (-not (Test-Path -LiteralPath $corpusPath -PathType Leaf)) { throw 'extracted fixed zstd corpus is missing' }

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
    [pscustomobject]@{ Label = 'stage copy'; Path = $stageCopyRoot }
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
  foreach ($name in $environmentNames) {
    $saved = $environmentBefore[$name]
    if ($saved.Present) {
      [Environment]::SetEnvironmentVariable($name, [string]$saved.Value, [EnvironmentVariableTarget]::Process)
    } elseif (Test-Path "Env:$name") {
      Remove-Item -LiteralPath "Env:$name" -ErrorAction Stop
    }
    $actualPresent = Test-Path "Env:$name"
    $actualValue = [Environment]::GetEnvironmentVariable($name, [EnvironmentVariableTarget]::Process)
    if ($actualPresent -ne $saved.Present -or $actualValue -cne $saved.Value) {
      $cleanupErrors += "process environment $name was not restored exactly"
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
