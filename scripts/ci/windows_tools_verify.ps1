[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('VERIFY-2022', 'VERIFY-2025')][string]$Label,
    [string]$StageArtifactRoot = '',
    [string]$GateInputsRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ([string]::IsNullOrWhiteSpace($StageArtifactRoot)) {
    $StageArtifactRoot = Join-Path $PWD '.ci/windows-tools-stage-artifact'
}
if ([string]::IsNullOrWhiteSpace($GateInputsRoot)) {
    $GateInputsRoot = Join-Path $PWD '.ci/windows-tools-gate-inputs'
}
$lockPath = Join-Path $PWD 'tools/lock.json'
$lock = Get-Content -Raw -LiteralPath $lockPath | ConvertFrom-Json
$stageItems = @(Get-ChildItem -LiteralPath $StageArtifactRoot -Directory -Filter 'windows_amd64' -Recurse)
if ($stageItems.Count -ne 1) { throw "$Label requires exactly one downloaded windows_amd64 stage" }
$stage = [IO.Path]::GetFullPath($stageItems[0].FullName)
$corpusItems = @(Get-ChildItem -LiteralPath $GateInputsRoot -File -Filter ([string]$lock.corpus.name) -Recurse)
$objdumpItems = @(Get-ChildItem -LiteralPath $GateInputsRoot -File -Filter 'objdump.exe' -Recurse)
if ($corpusItems.Count -ne 1 -or $objdumpItems.Count -ne 1) { throw "$Label locked gate inputs are missing or ambiguous" }

& ./scripts/ci/windows_tools_gate.ps1 -StageRoot $stage -ManifestPath (Join-Path $stage 'manifest.json') -LockPath $lockPath -CorpusPath $corpusItems[0].FullName -ObjdumpPath $objdumpItems[0].FullName
$gateSucceeded = $?
$gateExitCode = $LASTEXITCODE
if (-not $gateSucceeded -or $gateExitCode -ne 0) { throw "$Label gate failed with exit code $gateExitCode" }
Write-Output "$Label passed: PE/manifest/DLL allowlist, locked versions, six real benchmark smoke workloads, and verified NextTrace prebuilt metadata; performance_valid=false; canonical route/backtrace gate follows"
