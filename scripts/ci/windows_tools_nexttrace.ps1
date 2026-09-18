[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('VERIFY-2022', 'VERIFY-2025')][string]$Label,
    [Parameter(Mandatory)][string]$EcsPath,
    [string]$StageArtifactRoot = '',
    [string]$OutputRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not (Test-Path -LiteralPath $EcsPath -PathType Leaf)) {
    throw "$Label NextTrace gate cannot find ecs.exe: $EcsPath"
}
if ([string]::IsNullOrWhiteSpace($StageArtifactRoot)) {
    $StageArtifactRoot = Join-Path $PWD '.ci/windows-tools-stage-artifact'
}
$StageArtifactRoot = [IO.Path]::GetFullPath($StageArtifactRoot)
$runnerTemp = [string]$env:RUNNER_TEMP
if ([string]::IsNullOrWhiteSpace($runnerTemp) -or -not (Test-Path -LiteralPath $runnerTemp -PathType Container)) {
    throw "$Label NextTrace gate requires RUNNER_TEMP"
}
if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
    $OutputRoot = Join-Path $runnerTemp ("ecs-nexttrace-" + $Label.ToLowerInvariant())
}

$icmpPrerequisite = Join-Path $PWD 'scripts/ci/windows_icmp_prerequisite.ps1'
$icmpOwnerToken = [guid]::NewGuid().ToString('N')
$capabilityPath = [IO.Path]::GetFullPath((Join-Path $runnerTemp ('ecs-nexttrace-capability-' + $Label + '-' + [guid]::NewGuid().ToString('N') + '.json')))
$expectedNextTraceSha256 = '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b'
$setupAttempted = $false

try {
    $setupAttempted = $true
    $LASTEXITCODE = 0
    & $icmpPrerequisite -Action Setup -Label $Label -OwnerToken $icmpOwnerToken -CapabilityAwareIPv6
    $setupSucceeded = $?
    $setupExitCode = $LASTEXITCODE
    if (-not $setupSucceeded -or $setupExitCode -ne 0) { throw "$Label runner ICMP prerequisite setup failed with exit code $setupExitCode" }
    $stageItems = @(Get-ChildItem -LiteralPath $StageArtifactRoot -Directory -Filter 'windows_amd64' -Recurse)
    if ($stageItems.Count -ne 1) { throw "$Label NextTrace gate requires exactly one downloaded windows_amd64 stage" }
    $stage = [IO.Path]::GetFullPath($stageItems[0].FullName)
    $stageBin = [IO.Path]::GetFullPath((Join-Path $stage 'bin'))
    $nextTracePath = [IO.Path]::GetFullPath((Join-Path $stageBin 'nexttrace-tiny.exe'))
    if (-not (Test-Path -LiteralPath $nextTracePath -PathType Leaf)) { throw "$Label staged NextTrace is missing: $nextTracePath" }
    $nextTraceSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $nextTracePath).Hash.ToLowerInvariant()
    if ($nextTraceSha256 -cne $expectedNextTraceSha256) { throw "$Label staged NextTrace SHA-256 mismatch: got $nextTraceSha256" }

    $LASTEXITCODE = 0
    & ./scripts/ci/windows_nexttrace_capability.ps1 -Family IPv4 -Target '1.1.1.1' -MaxHops 12 -NextTracePath $nextTracePath -ExpectedSha256 $expectedNextTraceSha256 -EvidencePath $capabilityPath
    $capabilitySucceeded = $?
    $capabilityExitCode = $LASTEXITCODE
    if (-not $capabilitySucceeded) { throw "$Label NextTrace capability probe invocation failed" }
    if ($capabilityExitCode -ne 0) { throw "$Label NextTrace capability probe failed with exit code $capabilityExitCode" }
    if (-not (Test-Path -LiteralPath $capabilityPath -PathType Leaf)) { throw "$Label NextTrace capability evidence is missing: $capabilityPath" }

    $LASTEXITCODE = 0
    & ./scripts/ci/windows_nexttrace_gate.ps1 -Label $Label -EcsPath $EcsPath -ToolBin $stageBin -OutputRoot $OutputRoot -CapabilityPath $capabilityPath
    $gateSucceeded = $?
    $gateExitCode = $LASTEXITCODE
    if (-not $gateSucceeded -or $gateExitCode -ne 0) { throw "$Label canonical NextTrace gate failed with exit code $gateExitCode" }
} finally {
    if ($setupAttempted) {
        $LASTEXITCODE = 0
        & $icmpPrerequisite -Action Cleanup -Label $Label -OwnerToken $icmpOwnerToken
        $cleanupSucceeded = $?
        $cleanupExitCode = $LASTEXITCODE
        if (-not $cleanupSucceeded -or $cleanupExitCode -ne 0) { throw "$Label runner ICMP prerequisite cleanup failed with exit code $cleanupExitCode" }
    }
}

Write-Output "$Label canonical NextTrace IPv4 gate passed"
