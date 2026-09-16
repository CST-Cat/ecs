[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Setup', 'Cleanup')][string]$Action,
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9-]+$')][string]$Label,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{32}$')][string]$OwnerToken,
    [switch]$CapabilityAwareIPv6
)

# Workflow-only runner prerequisite for the official NextTrace Windows socket
# method. This file is never called by ecs.exe or production probe code. The
# ECS no-admin-operation checks remain separate and unchanged.

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$StateSchema = 'ecs.phase6.icmp/v1'
$runID = [string]$env:GITHUB_RUN_ID
$runAttempt = [string]$env:GITHUB_RUN_ATTEMPT
$runnerTemp = [string]$env:RUNNER_TEMP
if ([string]::IsNullOrWhiteSpace($runID) -or [string]::IsNullOrWhiteSpace($runAttempt) -or
    [string]::IsNullOrWhiteSpace($runnerTemp)) {
    throw 'windows-icmp-prerequisite: GitHub runner identity/temp variables are missing'
}
if ($runID -notmatch '^[0-9]+$' -or $runAttempt -notmatch '^[0-9]+$') {
    throw 'windows-icmp-prerequisite: GitHub runner identity is not numeric'
}

$rulePrefix = "ECS-Phase6-NextTrace-$Label-$runID-$runAttempt-$OwnerToken"
$statePath = Join-Path $runnerTemp "ecs-phase6-nexttrace-icmp-$Label-$runID-$runAttempt-$OwnerToken.json"
$statePattern = "ecs-phase6-nexttrace-icmp-$Label-$runID-$runAttempt-*.json"

function Stop-EcsIcmpPrerequisite {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-icmp-prerequisite: $Message"
}

function Test-EcsGlobalIPv6Capability {
    try {
        $addresses = @(
            Get-NetIPAddress -AddressFamily IPv6 -AddressState Preferred -ErrorAction Stop |
                Where-Object {
                    $address = [string]$_.IPAddress
                    $address -notmatch '^(?i:fe80:|fc|fd|::1$|::$)'
                }
        )
        $routes = @(
            Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction Stop |
                Where-Object { [string]$_.State -notin @('Dead', 'Invalid', 'Unreachable') }
        )
    } catch {
        return $false
    }
    return $addresses.Count -gt 0 -and $routes.Count -gt 0
}

function Get-EcsFirewallRuleDefinitions {
    param([Parameter(Mandatory)][bool]$IncludeIPv6)
    $definitions = @(
        [pscustomobject]@{
            Name = "$rulePrefix-ICMPv4"
            DisplayName = "$rulePrefix-ICMPv4"
            Enabled = 'True'
            Direction = 'Inbound'
            Action = 'Allow'
            Profile = 'Any'
            Protocol = 'ICMPv4'
            IcmpType = 'Any'
        }
    )
    if ($IncludeIPv6) {
        $definitions += [pscustomobject]@{
            Name = "$rulePrefix-ICMPv6"
            DisplayName = "$rulePrefix-ICMPv6"
            Enabled = 'True'
            Direction = 'Inbound'
            Action = 'Allow'
            Profile = 'Any'
            Protocol = 'ICMPv6'
            IcmpType = 'Any'
        }
    }
    return $definitions
}

function Get-EcsFirewallRuleSnapshot {
    param([Parameter(Mandatory)][string]$Name)
    $matches = @(
        Get-NetFirewallRule -PolicyStore ActiveStore -ErrorAction Stop |
            Where-Object { [string]$_.Name -eq $Name }
    )
    if ($matches.Count -gt 1) {
        Stop-EcsIcmpPrerequisite "firewall rule name is ambiguous: $Name"
    }
    if ($matches.Count -eq 0) {
        return $null
    }
    $portFilters = @(
        Get-NetFirewallPortFilter -AssociatedNetFirewallRule $matches[0] -PolicyStore ActiveStore -ErrorAction Stop
    )
    if ($portFilters.Count -ne 1) {
        Stop-EcsIcmpPrerequisite "firewall rule has unexpected filter count: $Name ($($portFilters.Count))"
    }
    return [pscustomobject]@{
        Name = [string]$matches[0].Name
        DisplayName = [string]$matches[0].DisplayName
        Enabled = [string]$matches[0].Enabled
        Direction = [string]$matches[0].Direction
        Action = [string]$matches[0].Action
        Profile = [string]$matches[0].Profile
        Protocol = [string]$portFilters[0].Protocol
        IcmpType = [string]$portFilters[0].IcmpType
    }
}

function Assert-EcsFirewallRuleSnapshot {
    param(
        [Parameter(Mandatory)][object]$Actual,
        [Parameter(Mandatory)][object]$Expected
    )
    foreach ($property in @('Name', 'DisplayName', 'Enabled', 'Direction', 'Action', 'Profile', 'Protocol', 'IcmpType')) {
        if ([string]$Actual.$property -cne [string]$Expected.$property) {
            Stop-EcsIcmpPrerequisite ("firewall rule verification mismatch for {0}: {1}={2}, expected {3}" -f
                $Expected.Name, $property, $Actual.$property, $Expected.$property)
        }
    }
}

function Write-EcsFirewallState {
    param(
        [Parameter(Mandatory)][object[]]$Definitions,
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$OwnedNames
    )
    $state = [ordered]@{
        schema_version = $StateSchema
        label = $Label
        owner_token = $OwnerToken
        rules = @(
            foreach ($definition in $Definitions) {
                [ordered]@{
                    Name = [string]$definition.Name
                    DisplayName = [string]$definition.DisplayName
                    Enabled = 'True'
                    Direction = 'Inbound'
                    Action = 'Allow'
                    Profile = 'Any'
                    Protocol = [string]$definition.Protocol
                    IcmpType = [string]$definition.IcmpType
                    Owned = [bool](@($OwnedNames) -contains [string]$definition.Name)
                }
            }
        )
    }
    $state | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $statePath -Encoding utf8 -NoNewline
}

function Read-EcsFirewallState {
    if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) {
        return $null
    }
    try {
        $state = Get-Content -Raw -LiteralPath $statePath | ConvertFrom-Json
    } catch {
        Stop-EcsIcmpPrerequisite "firewall state is not valid JSON: $statePath"
    }
    if ([string]$state.schema_version -cne $StateSchema -or [string]$state.label -cne $Label -or
        [string]$state.owner_token -cne $OwnerToken) {
        Stop-EcsIcmpPrerequisite "firewall state identity mismatch: $statePath"
    }
    $rules = @($state.rules)
    if ($rules.Count -lt 1) {
        Stop-EcsIcmpPrerequisite "firewall state contains no rules: $statePath"
    }
    $expectedDefinitions = @(Get-EcsFirewallRuleDefinitions -IncludeIPv6 $true)
    $seenNames = @()
    foreach ($rule in $rules) {
        $name = [string]$rule.Name
        $expectedMatches = @($expectedDefinitions | Where-Object { [string]$_.Name -eq $name })
        if ($expectedMatches.Count -ne 1) {
            Stop-EcsIcmpPrerequisite "firewall state contains an unexpected rule: $name"
        }
        if (@($seenNames | Where-Object { $_ -eq $name }).Count -ne 0) {
            Stop-EcsIcmpPrerequisite "firewall state contains a duplicate rule: $name"
        }
        $seenNames += $name
        $ownedProperties = @($rule.PSObject.Properties | Where-Object { $_.Name -ceq 'Owned' })
        if ($ownedProperties.Count -ne 1 -or $rule.Owned -isnot [bool]) {
            Stop-EcsIcmpPrerequisite "firewall state ownership marker is invalid: $name"
        }
        Assert-EcsFirewallRuleSnapshot -Actual $rule -Expected $expectedMatches[0]
    }
    return $rules
}

function Invoke-EcsFirewallSetup {
    $priorStates = @(Get-ChildItem -LiteralPath $runnerTemp -Filter $statePattern -File -ErrorAction Stop)
    if ($priorStates.Count -ne 0) {
        Stop-EcsIcmpPrerequisite "firewall state already exists for this runner attempt: $($priorStates.FullName -join ', ')"
    }
    if (Test-Path -LiteralPath $statePath) {
        Stop-EcsIcmpPrerequisite "firewall state already exists: $statePath"
    }
    $includeIPv6 = $CapabilityAwareIPv6 -and (Test-EcsGlobalIPv6Capability)
    $definitions = @(Get-EcsFirewallRuleDefinitions -IncludeIPv6 $includeIPv6)
    foreach ($definition in $definitions) {
        $existing = Get-EcsFirewallRuleSnapshot -Name $definition.Name
        if ($null -ne $existing) {
            Stop-EcsIcmpPrerequisite "refusing to overwrite existing firewall rule: $($definition.Name)"
        }
    }
    $ownedNames = @()
    Write-EcsFirewallState -Definitions $definitions -OwnedNames $ownedNames
    foreach ($definition in $definitions) {
        New-NetFirewallRule -PolicyStore PersistentStore -Name $definition.Name -DisplayName $definition.DisplayName -Enabled True -Direction Inbound -Action Allow -Profile Any -Protocol $definition.Protocol -IcmpType Any | Out-Null
        $actual = Get-EcsFirewallRuleSnapshot -Name $definition.Name
        if ($null -eq $actual) {
            Stop-EcsIcmpPrerequisite "created firewall rule cannot be found: $($definition.Name)"
        }
        Assert-EcsFirewallRuleSnapshot -Actual $actual -Expected $definition
        $ownedNames += [string]$definition.Name
        Write-EcsFirewallState -Definitions $definitions -OwnedNames $ownedNames
    }
    Write-Output ("temporary runner ICMP prerequisite passed: label={0}; rules={1}" -f $Label, (@($definitions.Name) -join ','))
}

function Invoke-EcsFirewallCleanup {
    $stateFiles = @(Get-ChildItem -LiteralPath $runnerTemp -Filter $statePattern -File -ErrorAction Stop)
    $foreignStateFiles = @($stateFiles | Where-Object { [string]$_.FullName -ne [string]$statePath })
    if ($foreignStateFiles.Count -ne 0) {
        Stop-EcsIcmpPrerequisite "cleanup found state for another ownership token; refusing to delete: $($foreignStateFiles.FullName -join ', ')"
    }
    $stateRules = Read-EcsFirewallState
    if ($null -eq $stateRules) {
        $unownedRules = @(
            foreach ($definition in @(Get-EcsFirewallRuleDefinitions -IncludeIPv6 $true)) {
                $existing = Get-EcsFirewallRuleSnapshot -Name $definition.Name
                if ($null -ne $existing) { $existing }
            }
        )
        if ($unownedRules.Count -ne 0) {
            Stop-EcsIcmpPrerequisite ("cleanup found same-name rules without owned state; refusing to delete: {0}" -f
                (@($unownedRules.Name) -join ','))
        }
        Write-Output "temporary runner ICMP cleanup passed: no owned rules"
        return
    }

    $ownedRules = @($stateRules | Where-Object { $_.Owned })
    $unownedRules = @($stateRules | Where-Object { -not $_.Owned })
    foreach ($stateRule in $unownedRules) {
        $existing = Get-EcsFirewallRuleSnapshot -Name ([string]$stateRule.Name)
        if ($null -ne $existing) {
            Stop-EcsIcmpPrerequisite "same-name firewall rule is present without ownership; refusing to delete: $($stateRule.Name)"
        }
    }

    $existingRules = @()
    foreach ($stateRule in $ownedRules) {
        $existing = Get-EcsFirewallRuleSnapshot -Name ([string]$stateRule.Name)
        if ($null -ne $existing) {
            Assert-EcsFirewallRuleSnapshot -Actual $existing -Expected $stateRule
            $existingRules += $existing
        }
    }
    foreach ($existing in $existingRules) {
        Remove-NetFirewallRule -PolicyStore PersistentStore -Name ([string]$existing.Name) -Confirm:$false -ErrorAction Stop
    }
    foreach ($stateRule in $ownedRules) {
        $remaining = Get-EcsFirewallRuleSnapshot -Name ([string]$stateRule.Name)
        if ($null -ne $remaining) {
            Stop-EcsIcmpPrerequisite "firewall rule remains after cleanup: $($stateRule.Name)"
        }
    }
    Remove-Item -LiteralPath $statePath -Force -ErrorAction Stop
    if (Test-Path -LiteralPath $statePath) {
        Stop-EcsIcmpPrerequisite "firewall state remains after cleanup: $statePath"
    }
    Write-Output ("temporary runner ICMP cleanup passed: label={0}; rules={1}" -f $Label, (@($stateRules.Name) -join ','))
}

if ($Action -ceq 'Setup') {
    Invoke-EcsFirewallSetup
} else {
    Invoke-EcsFirewallCleanup
}
