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
Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 12

Assert-TestThrows -Name 'max hops mismatch' -MessagePattern 'max_hops must remain 12' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 20
}
Assert-TestThrows -Name 'staged NextTrace path mismatch' -MessagePattern 'invalid staged NextTrace path' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath (Join-Path ([IO.Path]::GetTempPath()) 'nexttrace.exe') -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 12
}
Assert-TestThrows -Name 'SHA mismatch' -MessagePattern 'SHA-256 mismatch' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash (('b' * 64) -join '') -MaxHopsValue 12
}
Assert-TestThrows -Name 'target mismatch' -MessagePattern 'target does not match' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv4 -TargetValue '8.8.8.8' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 12
}
Assert-TestThrows -Name 'family/target mismatch' -MessagePattern 'target does not match the canonical ipv6 capability target' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'ecs.windows.nexttrace.capability/v1' -FamilyName ipv6 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 12
}
Assert-TestThrows -Name 'schema mismatch' -MessagePattern 'schema must be' -Script {
    Assert-EcsCapabilityEnvelope -SchemaVersion 'wrong.schema/v1' -FamilyName ipv4 -TargetValue '1.1.1.1' -NextTraceFullPath $testNextTracePath -ExpectedHash $hash -ActualHash $hash -MaxHopsValue 12
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

function Get-TestProductionFunctionSource {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Name
    )
    $tokens = $null
    $errors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path -LiteralPath $Path), [ref]$tokens, [ref]$errors)
    if ($errors.Count -ne 0) {
        throw "production script has PowerShell parse errors: $Path"
    }
    $functions = @($ast.Find({
            param($node)
            $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $Name
        }, $true))
    if ($functions.Count -ne 1) {
        throw "production script does not contain exactly one $Name function: $Path"
    }
    return $functions[0]
}

function Import-TestProductionFunction {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Name,
        [string]$ImportedName = $Name
    )
    $functionAst = Get-TestProductionFunctionSource -Path $Path -Name $Name
    $body = $functionAst.Body.Extent.Text
    $body = $body.Substring(1, $body.Length - 2)
    Set-Item -Path ("Function:\global:$ImportedName") -Value ([scriptblock]::Create($body))
}

$nextTraceGatePath = Join-Path $PSScriptRoot 'windows_nexttrace_gate.ps1'
$nextTraceReportAssertPath = Join-Path $PSScriptRoot 'windows_nexttrace_report_assert.ps1'
$NextTraceAdapter = 'nexttrace-json-v1'
$NextTraceEngine = 'nexttrace-tiny'
foreach ($functionName in @(
        'Stop-EcsWindowsNextTraceGate',
        'Get-EcsPropertyValue',
        'Get-EcsRawField',
        'Get-EcsSingleResult',
        'Test-EcsActualRespondingHop',
        'Test-EcsValidatedBacktraceNoResponseFailure',
        'Assert-EcsCanonicalTraceReport'
    )) {
    Import-TestProductionFunction -Path $nextTraceGatePath -Name $functionName
}
Import-TestProductionFunction -Path $nextTraceReportAssertPath -Name 'Test-EcsValidatedBacktraceNoResponseFailure' -ImportedName 'Test-EcsReportValidatedBacktraceNoResponseFailure'

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
    $gateDecision = Test-EcsValidatedBacktraceNoResponseFailure -Result $case.Result -Target '1.1.1.1' -CapabilityDecision $case.Decision -CapabilityLiveNetworkNotProven $case.Live
    $reportDecision = Test-EcsReportValidatedBacktraceNoResponseFailure -Result $case.Result -Target '1.1.1.1' -CapabilityDecision $case.Decision -CapabilityLiveNetworkNotProven $case.Live
    Assert-TestEqual -Actual $gateDecision -Expected $case.Expected -Name "gate no-response predicate: $($case.Name)"
    Assert-TestEqual -Actual $reportDecision -Expected $case.Expected -Name "report no-response predicate: $($case.Name)"
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

Write-Output 'windows_nexttrace_capability deterministic classifier/validation tests passed'
