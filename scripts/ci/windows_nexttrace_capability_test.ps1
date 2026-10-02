[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$helperPath = Join-Path $PSScriptRoot 'windows_nexttrace_capability.ps1'
$testNextTracePath = Join-Path ([IO.Path]::GetTempPath()) 'nexttrace-tiny.exe'
# Dot-sourcing exposes only pure classifier/validation functions; no process is started.
. $helperPath -Family IPv4 -Target '1.1.1.1' -NextTracePath $testNextTracePath -ExpectedSha256 (('0' * 64) -join '') -EvidencePath (Join-Path ([IO.Path]::GetTempPath()) 'capability.json')

function Assert-TestEqual {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Actual,
        [Parameter(Mandatory)][AllowNull()][object]$Expected,
        [Parameter(Mandatory)][string]$Name
    )
    if ($Actual -is [bool] -or $Expected -is [bool]) {
        if ([bool]$Actual -ne [bool]$Expected) {
            throw "${Name}: got '$Actual', want '$Expected'"
        }
        return
    }
    if ([string]$Actual -cne [string]$Expected) {
        throw "${Name}: got '$Actual', want '$Expected'"
    }
}

function Assert-TestThrows {
    param(
        [Parameter(Mandatory)][scriptblock]$Script,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$MessagePattern
    )
    $threw = $false
    try {
        & $Script
    } catch {
        $threw = $true
        if ($_.Exception.Message -notmatch $MessagePattern) {
            throw "$Name threw the wrong error: $($_.Exception.Message)"
        }
    }
    if (-not $threw) {
        throw "$Name did not hard-fail"
    }
}

function New-TestFacts {
    param(
        [Parameter(Mandatory)][int]$RespondingHops,
        [Parameter(Mandatory)][int]$PublicRespondingHops,
        [int]$ExitCode = 0
    )
    [pscustomobject]@{
        ExecutionStatus = 'completed'
        ParseStatus = 'parsed'
        ExitCode = $ExitCode
        HopSlots = 1
        RespondingHops = $RespondingHops
        PublicRespondingHops = $PublicRespondingHops
    }
}

$nativePublic = New-TestFacts -RespondingHops 1 -PublicRespondingHops 1
$nativePrivate = New-TestFacts -RespondingHops 1 -PublicRespondingHops 0
$nativeNone = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$directPublic = New-TestFacts -RespondingHops 1 -PublicRespondingHops 1
$directPrivate = New-TestFacts -RespondingHops 1 -PublicRespondingHops 0
$directNone = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0

$decisionCases = @(
    [pscustomobject]@{ Name = 'native+direct public response'; Native = $nativePublic; Direct = $directPublic; Decision = 'available'; LiveNetworkNotProven = $false }
    [pscustomobject]@{ Name = 'native private/direct public response'; Native = $nativePrivate; Direct = $directPublic; Decision = 'available'; LiveNetworkNotProven = $false }
    [pscustomobject]@{ Name = 'native no response/direct public response'; Native = $nativeNone; Direct = $directPublic; Decision = 'available'; LiveNetworkNotProven = $false }
    [pscustomobject]@{ Name = 'neither probe has a public response'; Native = $nativeNone; Direct = $directNone; Decision = 'not-testable'; LiveNetworkNotProven = $true }
)
foreach ($case in $decisionCases) {
    $decision = Get-EcsCapabilityDecision -NativeFacts $case.Native -DirectFacts $case.Direct
    Assert-TestEqual -Actual $decision.Decision -Expected $case.Decision -Name "$($case.Name) decision"
    Assert-TestEqual -Actual $decision.LiveNetworkNotProven -Expected $case.LiveNetworkNotProven -Name "$($case.Name) live-network flag"
}

Assert-TestThrows -Name 'native response/direct no response' -MessagePattern 'native tracert observed responding hops' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativePublic -DirectFacts $directNone | Out-Null
}
Assert-TestThrows -Name 'native public/direct private response mismatch' -MessagePattern 'native tracert observed a public responding hop but direct staged NextTrace observed no legal public responding hop' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativePublic -DirectFacts $directPrivate | Out-Null
}

$nativeIncompleteExecution = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$nativeIncompleteExecution.ExecutionStatus = 'not-run-capability-unavailable'
Assert-TestThrows -Name 'native incomplete execution status' -MessagePattern 'native capability facts did not complete and parse successfully' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativeIncompleteExecution -DirectFacts $directNone | Out-Null
}
$nativeIncompleteParse = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$nativeIncompleteParse.ParseStatus = 'not-run-capability-unavailable'
Assert-TestThrows -Name 'native incomplete parse status' -MessagePattern 'native capability facts did not complete and parse successfully' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativeIncompleteParse -DirectFacts $directNone | Out-Null
}
$directIncompleteExecution = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$directIncompleteExecution.ExecutionStatus = 'not-run-capability-unavailable'
Assert-TestThrows -Name 'direct incomplete execution status' -MessagePattern 'direct capability facts did not complete and parse successfully' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativeNone -DirectFacts $directIncompleteExecution | Out-Null
}
$directIncompleteParse = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$directIncompleteParse.ParseStatus = 'not-run-capability-unavailable'
Assert-TestThrows -Name 'direct incomplete parse status' -MessagePattern 'direct capability facts did not complete and parse successfully' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativeNone -DirectFacts $directIncompleteParse | Out-Null
}

Assert-TestThrows -Name 'non-zero response facts' -MessagePattern 'exited with code 7' -Script {
    Get-EcsCapabilityDecision -NativeFacts (New-TestFacts -RespondingHops 0 -PublicRespondingHops 0 -ExitCode 7) -DirectFacts $directNone | Out-Null
}
Assert-TestThrows -Name 'direct non-zero response facts' -MessagePattern 'direct capability facts exited with code 7' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nativeNone -DirectFacts (New-TestFacts -RespondingHops 0 -PublicRespondingHops 0 -ExitCode 7) | Out-Null
}

$invalidHopSlots = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$invalidHopSlots.HopSlots = 0
Assert-TestThrows -Name 'invalid hop slot count' -MessagePattern 'native capability facts.HopSlots is outside 1..12' -Script {
    Get-EcsCapabilityDecision -NativeFacts $invalidHopSlots -DirectFacts $directNone | Out-Null
}
$negativeRespondingHops = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$negativeRespondingHops.RespondingHops = -1
Assert-TestThrows -Name 'negative responding hops' -MessagePattern 'native capability facts.RespondingHops is outside the hop-slot range' -Script {
    Get-EcsCapabilityDecision -NativeFacts $negativeRespondingHops -DirectFacts $directNone | Out-Null
}
$invalidPublicRespondingHops = New-TestFacts -RespondingHops 1 -PublicRespondingHops 1
$invalidPublicRespondingHops.PublicRespondingHops = 2
Assert-TestThrows -Name 'public responding hops exceed responding hops' -MessagePattern 'native capability facts.PublicRespondingHops is outside the responding-hop range' -Script {
    Get-EcsCapabilityDecision -NativeFacts $invalidPublicRespondingHops -DirectFacts $directNone | Out-Null
}
$nanFacts = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$nanFacts.HopSlots = [double]::NaN
Assert-TestThrows -Name 'NaN-like hop fact' -MessagePattern 'native capability facts.HopSlots must be an integer' -Script {
    Get-EcsCapabilityDecision -NativeFacts $nanFacts -DirectFacts $directNone | Out-Null
}
$infiniteFacts = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$infiniteFacts.RespondingHops = [double]::PositiveInfinity
Assert-TestThrows -Name 'infinite-like hop fact' -MessagePattern 'native capability facts.RespondingHops must be an integer' -Script {
    Get-EcsCapabilityDecision -NativeFacts $infiniteFacts -DirectFacts $directNone | Out-Null
}
$malformedFacts = [pscustomobject]@{
    ExecutionStatus = 'completed'
    ParseStatus = 'parsed'
    ExitCode = 0
    HopSlots = 1
    RespondingHops = 0
}
Assert-TestThrows -Name 'malformed capability facts' -MessagePattern 'native capability facts has an invalid field set' -Script {
    Get-EcsCapabilityDecision -NativeFacts $malformedFacts -DirectFacts $directNone | Out-Null
}
$extraFact = New-TestFacts -RespondingHops 0 -PublicRespondingHops 0
$extraFact | Add-Member -NotePropertyName Unexpected -NotePropertyValue 1
Assert-TestThrows -Name 'extra capability fact' -MessagePattern 'native capability facts has an invalid field set' -Script {
    Get-EcsCapabilityDecision -NativeFacts $extraFact -DirectFacts $directNone | Out-Null
}

$hash = ('a' * 64) -join ''
Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -MaxHopsValue 12

Assert-TestThrows -Name 'max hops mismatch' -MessagePattern 'max_hops must remain 12' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -MaxHopsValue 20
}
Assert-TestThrows -Name 'staged NextTrace path mismatch' -MessagePattern 'invalid staged NextTrace path' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath (Join-Path ([IO.Path]::GetTempPath()) 'nexttrace.exe') -MaxHopsValue 12
}
Assert-TestThrows -Name 'target mismatch' -MessagePattern 'target does not match' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '8.8.8.8' -NextTraceFullPath $testNextTracePath -MaxHopsValue 12
}
Assert-TestThrows -Name 'family/target mismatch' -MessagePattern 'target does not match the canonical ipv6 capability target' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv6 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -MaxHopsValue 12
}
Assert-TestThrows -Name 'schema mismatch' -MessagePattern 'schema must be' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'wrong.schema/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -MaxHopsValue 12
}

$testEvidenceRoot = Join-Path ([IO.Path]::GetTempPath()) ("ecs-nexttrace-evidence-test-" + [guid]::NewGuid().ToString('N'))
$script:TestTracertPath = Join-Path $testEvidenceRoot 'system32/tracert.exe'
function Get-EcsCanonicalNativeExecutablePath { return $script:TestTracertPath }
$testEvidencePath = Join-Path $testEvidenceRoot 'capability.json'
$testRawDirectory = Join-Path $testEvidenceRoot 'capability.json.raw-test'
New-Item -ItemType Directory -Force -Path $testRawDirectory | Out-Null
foreach ($rawName in @('native.stdout', 'native.stderr', 'direct.stdout', 'direct.stderr')) {
    Set-Content -LiteralPath (Join-Path $testRawDirectory $rawName) -Value ''
}
$testNextTracePath = Join-Path $testEvidenceRoot 'nexttrace-tiny.exe'
$trustedNextTraceHash = ('a' * 64) -join ''

function New-TestCapabilityEvidence {
    param(
        [string]$FamilyName = 'ipv4',
        [string]$Decision = 'available',
        [switch]$Unavailable,
        [int]$RespondingHops = 1,
        [int]$PublicRespondingHops = 1
    )
    $familyFlag = if ($FamilyName -ceq 'ipv4') { '-4' } else { '-6' }
    $target = if ($FamilyName -ceq 'ipv4') { '1.1.1.1' } else { '2606:4700:4700::1111' }
    $status = if ($Unavailable) { 'not-run-capability-unavailable' } else { 'completed' }
    $parseStatus = if ($Unavailable) { 'not-run-capability-unavailable' } else { 'parsed' }
    $exitCode = if ($Unavailable) { $null } else { 0 }
    $hopSlots = if ($Unavailable) { 0 } else { 1 }
    if ($Unavailable) {
        $Decision = 'ipv6-unavailable'
        $RespondingHops = 0
        $PublicRespondingHops = 0
    }
    $native = [ordered]@{
        executable_path = $script:TestTracertPath
        arguments = @('-d', $familyFlag, '-h', '12', $target)
        raw_stdout_path = Join-Path $script:TestRawDirectory 'native.stdout'
        raw_stderr_path = Join-Path $script:TestRawDirectory 'native.stderr'
        exit_code = $exitCode
        execution_status = $status
        parse_status = $parseStatus
        hop_slots = $hopSlots
        responding_hops = $RespondingHops
        public_responding_hops = $PublicRespondingHops
    }
    $direct = [ordered]@{
        executable_path = $script:TestNextTracePath
        arguments = @($familyFlag, '--no-color', '--json', '-M', '--max-hops', '12', '--queries', '1', '--parallel-requests', '1', '--timeout', '1000', $target)
        raw_stdout_path = Join-Path $script:TestRawDirectory 'direct.stdout'
        raw_stderr_path = Join-Path $script:TestRawDirectory 'direct.stderr'
        exit_code = $exitCode
        execution_status = $status
        parse_status = $parseStatus
        hop_slots = $hopSlots
        responding_hops = $RespondingHops
        public_responding_hops = $PublicRespondingHops
    }
    $zero = if ($Unavailable) { 0 } else { $RespondingHops }
    $public = if ($Unavailable) { 0 } else { $PublicRespondingHops }
    return [pscustomobject]@{
        schema_version = 'ecs.windows.nexttrace.capability/v1'
        evidence_path = $script:TestEvidencePath
        raw_output_directory = $script:TestRawDirectory
        family = $FamilyName
        target = $target
        max_hops = 12
        decision = $Decision
        live_network_not_proven = ($Decision -ne 'available')
        reason = 'deterministic synthetic capability evidence'
        nexttrace_path = $script:TestNextTracePath
        nexttrace_expected_sha256 = $script:TrustedNextTraceHash
        nexttrace_sha256 = $script:TrustedNextTraceHash
        native_tracert = [pscustomobject]$native
        direct_nexttrace = [pscustomobject]$direct
        observed = [pscustomobject]@{
            native_hop_slots = $hopSlots
            native_responding_hops = $zero
            native_public_responding_hops = $public
            direct_hop_slots = $hopSlots
            direct_responding_hops = $zero
            direct_public_responding_hops = $public
        }
    }
}

$script:TestEvidenceRoot = $testEvidenceRoot
$script:TestEvidencePath = $testEvidencePath
$script:TestRawDirectory = $testRawDirectory
$script:TestNextTracePath = $testNextTracePath
$script:TrustedNextTraceHash = $trustedNextTraceHash
$validCapabilityEvidence = New-TestCapabilityEvidence
$validatedCapabilityEvidence = Assert-EcsCapabilityEvidence -Evidence $validCapabilityEvidence `
    -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
    -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash
Assert-TestEqual -Actual $validatedCapabilityEvidence.Decision -Expected 'available' -Name 'validated capability decision'
Assert-TestEqual -Actual $validatedCapabilityEvidence.Sha256 -Expected $trustedNextTraceHash -Name 'trusted capability digest'
$uppercaseTrustedNextTraceHash = $trustedNextTraceHash.ToUpperInvariant()
$producerValidatedCapabilityEvidence = Assert-EcsCapabilityEvidence -Evidence $validCapabilityEvidence `
    -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
    -ExpectedFamilyName ipv4 -ExpectedSha256 $uppercaseTrustedNextTraceHash -ActualSha256 $trustedNextTraceHash
Assert-TestEqual -Actual $producerValidatedCapabilityEvidence.Sha256 -Expected $trustedNextTraceHash -Name 'producer publish boundary accepts uppercase trusted pin'
$consumerValidatedCapabilityEvidence = Assert-EcsCapabilityEvidence -Evidence $validCapabilityEvidence `
    -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
    -ExpectedFamilyName ipv4 -ExpectedSha256 $uppercaseTrustedNextTraceHash -ActualSha256 $uppercaseTrustedNextTraceHash
Assert-TestEqual -Actual $consumerValidatedCapabilityEvidence.Sha256 -Expected $trustedNextTraceHash -Name 'consumer accepts uppercase lock pin'

$wrongPin = ('b' * 64) -join ''
Assert-TestThrows -Name 'capability evidence rejects wrong external pin' -MessagePattern 'trusted pin and observed file digest' -Script {
    Assert-EcsCapabilityEvidence -Evidence $validCapabilityEvidence `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $wrongPin -ActualSha256 $wrongPin | Out-Null
}
$tamperedActual = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedActual.nexttrace_sha256 = $wrongPin
Assert-TestThrows -Name 'capability evidence rejects tampered actual digest' -MessagePattern 'trusted pin and observed file digest' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedActual `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedExpected = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedExpected.nexttrace_expected_sha256 = $wrongPin
Assert-TestThrows -Name 'capability evidence rejects tampered expected digest' -MessagePattern 'trusted pin and observed file digest' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedExpected `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedRunPath = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedRunPath.evidence_path = Join-Path $testEvidenceRoot 'other-run.json'
Assert-TestThrows -Name 'capability evidence rejects another run path' -MessagePattern 'paths do not match this run scope' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedRunPath `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedRawScope = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedRawScope.raw_output_directory = Join-Path $testEvidenceRoot 'other-run.raw-x'
Assert-TestThrows -Name 'capability evidence rejects another raw-output scope' -MessagePattern 'paths do not match this run scope' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedRawScope `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedFamily = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedFamily.family = 'ipv6'
Assert-TestThrows -Name 'capability evidence rejects wrong family' -MessagePattern 'canonical ipv6 capability target' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedFamily `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedTarget = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedTarget.target = '8.8.8.8'
Assert-TestThrows -Name 'capability evidence rejects wrong target' -MessagePattern 'canonical ipv4 capability target' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedTarget `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$tamperedWindowsFact = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$tamperedWindowsFact.native_tracert.executable_path = Join-Path $testEvidenceRoot 'other/tracert.exe'
Assert-TestThrows -Name 'capability evidence rejects wrong Windows executable fact' -MessagePattern 'canonical System32 path' -Script {
    Assert-EcsCapabilityEvidence -Evidence $tamperedWindowsFact `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$unexpectedDllFact = $validCapabilityEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$unexpectedDllFact | Add-Member -NotePropertyName dll_facts -NotePropertyValue @()
Assert-TestThrows -Name 'capability evidence rejects uncontracted DLL facts' -MessagePattern 'invalid field set' -Script {
    Assert-EcsCapabilityEvidence -Evidence $unexpectedDllFact `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv4 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
$ipv6AvailableEvidence = New-TestCapabilityEvidence -FamilyName ipv6
$ipv6Available = Assert-EcsCapabilityEvidence -Evidence $ipv6AvailableEvidence `
    -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
    -ExpectedFamilyName ipv6 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash
Assert-TestEqual -Actual $ipv6Available.Decision -Expected 'available' -Name 'IPv6 available capability evidence'
$ipv6UnavailableEvidence = New-TestCapabilityEvidence -FamilyName ipv6 -Unavailable
$ipv6Unavailable = Assert-EcsCapabilityEvidence -Evidence $ipv6UnavailableEvidence `
    -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
    -ExpectedFamilyName ipv6 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash
Assert-TestEqual -Actual $ipv6Unavailable.Decision -Expected 'ipv6-unavailable' -Name 'IPv6 unavailable capability evidence'
$ipv6WrongUnavailable = $ipv6UnavailableEvidence | ConvertTo-Json -Depth 10 | ConvertFrom-Json
$ipv6WrongUnavailable.direct_nexttrace.execution_status = 'completed'
Assert-TestThrows -Name 'IPv6 unavailable evidence rejects executed facts' -MessagePattern 'contains executed probe facts' -Script {
    Assert-EcsCapabilityEvidence -Evidence $ipv6WrongUnavailable `
        -ExpectedEvidencePath $testEvidencePath -ExpectedNextTracePath $testNextTracePath `
        -ExpectedFamilyName ipv6 -ExpectedSha256 $trustedNextTraceHash -ActualSha256 $trustedNextTraceHash | Out-Null
}
Assert-TestThrows -Name 'invalid JSON' -MessagePattern 'invalid JSON' -Script {
    Parse-EcsNextTraceOutput -Output '{' -FamilyName ipv4 -HopLimit 12 | Out-Null
}
$nativeHeader = 'Tracing route to 1.1.1.1 over a maximum of 12 hops'
$nativeHop = '  1    <1 ms    <1 ms    <1 ms  1.1.1.1'
$validNative = Parse-EcsNativeTracertOutput -Output "$nativeHeader`n$nativeHop`nTrace complete." -FamilyName ipv4 -HopLimit 12 -Target '1.1.1.1'
Assert-TestEqual -Actual $validNative.HopSlots -Expected 1 -Name 'native parser valid hop slots'
Assert-TestEqual -Actual $validNative.PublicRespondingHops -Expected 1 -Name 'native parser valid public response'
Assert-TestThrows -Name 'native output target text cannot spoof header' -MessagePattern 'canonical tracert header' -Script {
    Parse-EcsNativeTracertOutput -Output "$nativeHop`nTrace complete." -FamilyName ipv4 -HopLimit 12 -Target '1.1.1.1' | Out-Null
}
Assert-TestThrows -Name 'native output truncated before completion' -MessagePattern 'Trace complete' -Script {
    Parse-EcsNativeTracertOutput -Output "$nativeHeader`n$nativeHop" -FamilyName ipv4 -HopLimit 12 -Target '1.1.1.1' | Out-Null
}
Assert-TestThrows -Name 'Success=true without address' -MessagePattern 'no address' -Script {
    Get-EcsNextTraceProbeAddresses -Probe ([pscustomobject]@{ Success = $true }) -Context 'test probe' | Out-Null
}
Assert-TestThrows -Name 'unknown probe object' -MessagePattern 'not a recognized NextTrace probe object' -Script {
    Get-EcsNextTraceProbeAddresses -Probe ([pscustomobject]@{ Unknown = 'value' }) -Context 'test probe' | Out-Null
}
Assert-TestThrows -Name 'invalid evidence field set' -MessagePattern 'invalid field set' -Script {
    Assert-EcsExactPropertyNames -Object ([pscustomobject]@{ schema_version = 'ecs.windows.nexttrace.capability/v1' }) -Expected @('schema_version', 'family') -Context 'test evidence'
}

$nextTraceReportAssertPath = Join-Path $PSScriptRoot 'windows_nexttrace_report_assert.ps1'
. $nextTraceReportAssertPath

function New-TestBacktraceResult {
    param(
        [string]$Status = 'error',
        [int]$Valid = 0,
        [string]$FailureTarget = '1.1.1.1',
        [string]$FailureCategory = 'unknown',
        [bool]$FailureRetryable = $false,
        [int]$FailureCount = 1,
        [switch]$FailureMessage,
        [switch]$MissingTrace
    )
    $failureValues = [ordered]@{
        category = $FailureCategory
        stage = 'trace'
        target = $FailureTarget
        retryable = $FailureRetryable
        count = $FailureCount
    }
    if ($FailureMessage) {
        $failureValues.message = 'unexpected diagnostic'
    }
    $failure = [pscustomobject]$failureValues
    $arguments = '--no-color --json -4 -M --max-hops 20 --queries 1 --parallel-requests 1 --timeout 1000'
    $trace = [pscustomobject]@{
        engine = 'nexttrace-tiny'
        adapter = 'nexttrace-json-v1'
        family = 'ipv4'
        target = '1.1.1.1'
        hops = @(
            for ($hop = 1; $hop -le 12; $hop++) {
                [pscustomobject]@{ hop = $hop; responded = $false; ip = $null }
            }
        )
    }
    $result = [pscustomobject]@{
        id = 'backtrace'
        status = $Status
        evidence = [pscustomobject]@{ valid = $Valid; expected = 1 }
        failures = @($failure)
        fields = @(
            [pscustomobject]@{ key = 'engine'; value = [pscustomobject]@{ raw = 'nexttrace-tiny' } }
            [pscustomobject]@{ key = 'version'; value = [pscustomobject]@{ raw = 'nexttrace-tiny v1.7.1' } }
            [pscustomobject]@{ key = 'arguments'; value = [pscustomobject]@{ raw = $arguments } }
        )
        methodology = [pscustomobject]@{
            parameters = [pscustomobject]@{
                adapter = 'nexttrace-json-v1'
                ip_version = '4'
                arguments = $arguments
                targets = '1.1.1.1'
                max_hops = '20'
            }
        }
        sources = @([pscustomobject]@{ name = 'probe.route.source.nexttrace.name'; url = 'https://github.com/nxtrace/NTrace-core' })
        text_blocks = @(
            [pscustomobject]@{
                title = 'probe.backtrace.normalized_trace_json'
                language = 'json'
                content = ($trace | ConvertTo-Json -Depth 8 -Compress)
            }
        )
    }
    if ($MissingTrace) {
        $result.text_blocks = @()
    }
    return $result
}

$noResponsePredicateCases = @(
    [pscustomobject]@{ Name = 'exact validated not-testable'; Result = (New-TestBacktraceResult); Decision = 'not-testable'; Live = $true; Expected = $true }
    [pscustomobject]@{ Name = 'available capability'; Result = (New-TestBacktraceResult); Decision = 'available'; Live = $false; Expected = $false }
    [pscustomobject]@{ Name = 'failure has message'; Result = (New-TestBacktraceResult -FailureMessage); Decision = 'not-testable'; Live = $true; Expected = $false }
    [pscustomobject]@{ Name = 'failure has error category'; Result = (New-TestBacktraceResult -FailureCategory 'permission_denied'); Decision = 'not-testable'; Live = $true; Expected = $false }
    [pscustomobject]@{ Name = 'failure has parser category'; Result = (New-TestBacktraceResult -FailureCategory 'parse_error'); Decision = 'not-testable'; Live = $true; Expected = $false }
    [pscustomobject]@{ Name = 'failure has wrong target'; Result = (New-TestBacktraceResult -FailureTarget '8.8.8.8'); Decision = 'not-testable'; Live = $true; Expected = $false }
    [pscustomobject]@{ Name = 'evidence is valid'; Result = (New-TestBacktraceResult -Valid 1); Decision = 'not-testable'; Live = $true; Expected = $false }
)
foreach ($case in $noResponsePredicateCases) {
    $decision = Test-EcsValidatedBacktraceNoResponseFailure -Result $case.Result -Target '1.1.1.1' -CapabilityDecision $case.Decision -CapabilityLiveNetworkNotProven $case.Live
    Assert-TestEqual -Actual $decision -Expected $case.Expected -Name "backtrace no-response predicate: $($case.Name)"
}

$exactBacktraceReport = [pscustomobject]@{
    schema_version = 'ecs.report/v1'
    results = @((New-TestBacktraceResult))
}
$null = Assert-EcsCanonicalTraceReport -Report $exactBacktraceReport -Module 'backtrace' -Family '4' -FamilyName 'ipv4' -MaxHops 20 -Target '1.1.1.1' -CapabilityDecision 'not-testable' -CapabilityLiveNetworkNotProven $true
Assert-TestThrows -Name 'available capability cannot accept backtrace no-response error' -MessagePattern 'invalid gate status' -Script {
    Assert-EcsCanonicalTraceReport -Report $exactBacktraceReport -Module 'backtrace' -Family '4' -FamilyName 'ipv4' -MaxHops 20 -Target '1.1.1.1' -CapabilityDecision 'available' -CapabilityLiveNetworkNotProven $false | Out-Null
}
$missingTraceReport = [pscustomobject]@{
    schema_version = 'ecs.report/v1'
    results = @((New-TestBacktraceResult -MissingTrace))
}
Assert-TestThrows -Name 'backtrace no-response requires canonical trace' -MessagePattern 'no unique canonical normalized trace JSON' -Script {
    Assert-EcsCanonicalTraceReport -Report $missingTraceReport -Module 'backtrace' -Family '4' -FamilyName 'ipv4' -MaxHops 20 -Target '1.1.1.1' -CapabilityDecision 'not-testable' -CapabilityLiveNetworkNotProven $true | Out-Null
}

$routeResult = New-TestBacktraceResult -Status 'ok' -Valid 1
$routeResult.id = 'route'
$routeResult.failures = @()
$routeArguments = '--no-color --json -4 -M --max-hops 12 --queries 1 --parallel-requests 1 --timeout 1000'
$routeResult.methodology.parameters.arguments = $routeArguments
$routeResult.methodology.parameters.max_hops = '12'
foreach ($field in $routeResult.fields) {
    if ($field.key -ceq 'arguments') { $field.value.raw = $routeArguments }
}
$routeTrace = [string]$routeResult.text_blocks[0].content | ConvertFrom-Json
$routeTrace.hops[0].responded = $true
$routeTrace.hops[0].ip = '1.1.1.1'
$routeResult.text_blocks[0].title = 'probe.route.normalized_trace_json'
$routeResult.text_blocks[0].content = $routeTrace | ConvertTo-Json -Depth 8 -Compress
$routeReport = [pscustomobject]@{
    schema_version = 'ecs.report/v1'
    results = @($routeResult)
}
$routeAssertion = Assert-EcsCanonicalTraceReport -Report $routeReport -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target '1.1.1.1'
Assert-TestEqual -Actual $routeAssertion.RespondingHopCount -Expected 1 -Name 'canonical route responding hop'
$wrongRouteReport = $routeReport | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$wrongRouteTrace = [string]$wrongRouteReport.results[0].text_blocks[0].content | ConvertFrom-Json
$wrongRouteTrace.target = '8.8.8.8'
$wrongRouteReport.results[0].text_blocks[0].content = $wrongRouteTrace | ConvertTo-Json -Depth 8 -Compress
Assert-TestThrows -Name 'route report rejects wrong target' -MessagePattern 'canonical trace facts are incomplete' -Script {
    Assert-EcsCanonicalTraceReport -Report $wrongRouteReport -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target '1.1.1.1' | Out-Null
}
$noResponseRouteReport = $routeReport | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$noResponseRouteTrace = [string]$noResponseRouteReport.results[0].text_blocks[0].content | ConvertFrom-Json
$noResponseRouteTrace.hops[0].responded = $false
$noResponseRouteTrace.hops[0].ip = $null
$noResponseRouteReport.results[0].text_blocks[0].content = $noResponseRouteTrace | ConvertTo-Json -Depth 8 -Compress
Assert-TestThrows -Name 'route report requires real response when capability is available' -MessagePattern 'no actual responding hop' -Script {
    Assert-EcsCanonicalTraceReport -Report $noResponseRouteReport -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target '1.1.1.1' -CapabilityDecision available | Out-Null
}
$zeroRouteAssertion = Assert-EcsCanonicalTraceReport -Report $noResponseRouteReport -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target '1.1.1.1' -CapabilityDecision not-testable -CapabilityLiveNetworkNotProven $true
Assert-TestEqual -Actual $zeroRouteAssertion.AllowZeroRespondingHops -Expected $true -Name 'route zero response is capability-gated'

Remove-Item -LiteralPath $testEvidenceRoot -Recurse -Force

Write-Output 'windows_nexttrace_capability deterministic classifier/validation tests passed'
