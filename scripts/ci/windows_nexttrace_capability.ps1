[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('IPv4', 'IPv6')][string]$Family,
    [Parameter(Mandatory)][string]$Target,
    [Parameter(Mandatory)][string]$NextTracePath,
    [Parameter(Mandatory)][ValidatePattern('^[0-9A-Fa-f]{64}$')][string]$ExpectedSha256,
    [Parameter(Mandatory)][Alias('OutputPath')][string]$EvidencePath,
    [int]$MaxHops = 12
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$CapabilitySchema = 'ecs.windows.nexttrace.capability/v1'
$CanonicalIPv4Target = '1.1.1.1'
$CanonicalIPv6Target = '2606:4700:4700::1111'
$CanonicalMaxHops = 12
# Dot-sourcing defines validators for the deterministic test; direct invocation always runs the Windows probe.
$DotSourced = $MyInvocation.InvocationName -eq '.'

function Stop-EcsWindowsNextTraceCapability {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-nexttrace-capability: $Message"
}

function Assert-EcsIntegerValue {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Value,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Value -or $Value -is [bool] -or
        ($Value -isnot [byte] -and $Value -isnot [sbyte] -and
         $Value -isnot [int16] -and $Value -isnot [uint16] -and
         $Value -isnot [int32] -and $Value -isnot [uint32] -and
         $Value -isnot [int64] -and $Value -isnot [uint64])) {
        Stop-EcsWindowsNextTraceCapability "$Context must be an integer"
    }
    return [int64]$Value
}

function Assert-EcsStringValue {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Value,
        [Parameter(Mandatory)][string]$Context
    )
    if ($Value -isnot [string] -or [string]::IsNullOrWhiteSpace([string]$Value)) {
        Stop-EcsWindowsNextTraceCapability "$Context must be a non-empty string"
    }
    return [string]$Value
}

function Get-EcsObjectPropertyNames {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Object,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Object) {
        Stop-EcsWindowsNextTraceCapability "$Context must be an object"
    }
    if ($Object -is [System.Collections.IDictionary]) {
        return @($Object.Keys | ForEach-Object { [string]($_) })
    }
    if ($Object -isnot [System.Management.Automation.PSCustomObject]) {
        Stop-EcsWindowsNextTraceCapability "$Context must be an object"
    }
    return @($Object.PSObject.Properties | ForEach-Object { [string]($_.Name) })
}

function Get-EcsObjectPropertyValue {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Object,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Context
    )
    $names = @(Get-EcsObjectPropertyNames -Object $Object -Context $Context)
    $matches = @($names | Where-Object { $_ -ceq $Name })
    if ($matches.Count -ne 1) {
        Stop-EcsWindowsNextTraceCapability "$Context must contain exactly one '$Name' property"
    }
    if ($Object -is [System.Collections.IDictionary]) {
        $value = $Object[$matches[0]]
        return ,$value
    }
    $value = $Object.PSObject.Properties[$matches[0]].Value
    return ,$value
}

function Assert-EcsExactPropertyNames {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Object,
        [Parameter(Mandatory)][string[]]$Expected,
        [Parameter(Mandatory)][string]$Context
    )
    $actual = @(Get-EcsObjectPropertyNames -Object $Object -Context $Context)
    $expectedSorted = @($Expected | Sort-Object)
    $actualSorted = @($actual | Sort-Object)
    if ($expectedSorted.Count -ne $actualSorted.Count -or
        (($expectedSorted -join ([char]0)) -cne ($actualSorted -join ([char]0)))) {
        Stop-EcsWindowsNextTraceCapability "$Context has an invalid field set (expected: $($expectedSorted -join ', '); got: $($actualSorted -join ', '))"
    }
}

function Resolve-EcsAbsolutePath {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Description
    )
    if ([string]::IsNullOrWhiteSpace($Path) -or -not [IO.Path]::IsPathRooted($Path)) {
        Stop-EcsWindowsNextTraceCapability "$Description must be an absolute path"
    }
    try {
        return [IO.Path]::GetFullPath($Path)
    } catch {
        Stop-EcsWindowsNextTraceCapability "$Description is not a valid path: $($_.Exception.Message)"
    }
}

function Get-EcsCanonicalNativeExecutablePath {
    $systemRootText = [string]$env:SystemRoot
    if ([string]::IsNullOrWhiteSpace($systemRootText)) {
        Stop-EcsWindowsNextTraceCapability 'SystemRoot environment variable is missing'
    }
    $systemRoot = Resolve-EcsAbsolutePath -Path $systemRootText -Description 'SystemRoot'
    if (-not (Test-Path -LiteralPath $systemRoot -PathType Container)) {
        Stop-EcsWindowsNextTraceCapability "SystemRoot directory does not exist: $systemRoot"
    }
    $system32 = Resolve-EcsAbsolutePath -Path (Join-Path $systemRoot 'System32') -Description 'System32 directory'
    if (-not (Test-Path -LiteralPath $system32 -PathType Container)) {
        Stop-EcsWindowsNextTraceCapability "System32 directory does not exist: $system32"
    }
    $nativePath = Resolve-EcsAbsolutePath -Path (Join-Path $system32 'tracert.exe') -Description 'System32 tracert.exe'
    if (-not (Test-Path -LiteralPath $nativePath -PathType Leaf)) {
        Stop-EcsWindowsNextTraceCapability "absolute System32 tracert.exe does not exist: $nativePath"
    }
    return $nativePath
}

function Write-EcsUtf8Text {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][AllowEmptyString()][string]$Content
    )
    try {
        $encoding = [System.Text.UTF8Encoding]::new($false)
        [IO.File]::WriteAllText($Path, $Content, $encoding)
    } catch {
        Stop-EcsWindowsNextTraceCapability "cannot write evidence text '$Path': $($_.Exception.Message)"
    }
}

function Get-EcsIPAddressToken {
    param([Parameter(Mandatory)][string]$Token)

    $candidate = $Token.Trim()
    $candidate = $candidate -replace '^[\[\(]+', ''
    $candidate = $candidate -replace '[\]\),;]+$', ''
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        return $null
    }
    if ($candidate -notmatch '^\d{1,3}(?:\.\d{1,3}){3}$' -and
        $candidate -notmatch '^[0-9A-Fa-f:.]+(?:%[0-9A-Za-z_.-]+)?$') {
        return $null
    }
    $address = $null
    if (-not [System.Net.IPAddress]::TryParse($candidate, [ref]$address)) {
        return $null
    }
    return $address
}

function Test-EcsAddressFamily {
    param(
        [Parameter(Mandatory)][System.Net.IPAddress]$Address,
        [Parameter(Mandatory)][ValidateSet('ipv4', 'ipv6')][string]$FamilyName
    )
    if ($FamilyName -ceq 'ipv4') {
        return $Address.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork
    }
    return $Address.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetworkV6
}

function Test-EcsPublicIPAddress {
    param([Parameter(Mandatory)][System.Net.IPAddress]$Address)

    if ($Address.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork) {
        $bytes = $Address.GetAddressBytes()
        $first = [int]$bytes[0]
        $second = [int]$bytes[1]
        if ($first -eq 0 -or $first -eq 10 -or $first -eq 127 -or $first -ge 224) { return $false }
        if ($first -eq 100 -and $second -ge 64 -and $second -le 127) { return $false }
        if ($first -eq 169 -and $second -eq 254) { return $false }
        if ($first -eq 172 -and $second -ge 16 -and $second -le 31) { return $false }
        if ($first -eq 192 -and $second -eq 168) { return $false }
        if ($first -eq 192 -and $second -eq 0) { return $false }
        if ($first -eq 192 -and $second -eq 2) { return $false }
        if ($first -eq 192 -and $second -eq 88 -and $bytes[2] -eq 99) { return $false }
        if ($first -eq 198 -and $second -ge 18 -and $second -le 19) { return $false }
        if ($first -eq 198 -and $second -eq 51 -and $bytes[2] -eq 100) { return $false }
        if ($first -eq 203 -and $second -eq 0 -and $bytes[2] -eq 113) { return $false }
        return $true
    }

    if ($Address.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetworkV6) {
        return $false
    }
    if ($Address.IsIPv6LinkLocal -or $Address.IsIPv6SiteLocal -or $Address.IsIPv6Multicast -or
        [System.Net.IPAddress]::IsLoopback($Address) -or $Address.Equals([System.Net.IPAddress]::IPv6Any)) {
        return $false
    }
    $bytes = $Address.GetAddressBytes()
    if (($bytes[0] -band 0xFE) -eq 0xFC -or $bytes[0] -eq 0xFF) { return $false }
    if ($bytes[0] -eq 0x20 -and $bytes[1] -eq 0x01 -and $bytes[2] -eq 0x0D -and $bytes[3] -eq 0xB8) {
        return $false
    }
    $isMapped = $true
    for ($index = 0; $index -lt 10; $index++) {
        if ($bytes[$index] -ne 0) { $isMapped = $false; break }
    }
    if ($isMapped -and $bytes[10] -eq 0xFF -and $bytes[11] -eq 0xFF) { return $false }
    return $true
}

function Invoke-EcsCapturedProcess {
    param(
        [Parameter(Mandatory)][string]$ExecutablePath,
        [Parameter(Mandatory)][string[]]$Arguments,
        [Parameter(Mandatory)][string]$WorkingDirectory,
        [Parameter(Mandatory)][string]$StdoutPath,
        [Parameter(Mandatory)][string]$StderrPath,
        [Parameter(Mandatory)][string]$Description
    )

    Write-EcsUtf8Text -Path $StdoutPath -Content ''
    Write-EcsUtf8Text -Path $StderrPath -Content ''
    $process = $null
    try {
        $process = Start-Process -FilePath $ExecutablePath -ArgumentList $Arguments -WorkingDirectory $WorkingDirectory -RedirectStandardOutput $StdoutPath -RedirectStandardError $StderrPath -NoNewWindow -Wait -PassThru -ErrorAction Stop
    } catch {
        Write-EcsUtf8Text -Path $StderrPath -Content ("process start failed: {0}" -f $_.Exception.Message)
        Stop-EcsWindowsNextTraceCapability "$Description could not start '$ExecutablePath': $($_.Exception.Message)"
    }
    if ($null -eq $process) {
        Stop-EcsWindowsNextTraceCapability "$Description returned no process object"
    }
    try {
        $exitCode = [int]$process.ExitCode
        $stdout = [IO.File]::ReadAllText($StdoutPath)
        $stderr = [IO.File]::ReadAllText($StderrPath)
    } catch {
        Stop-EcsWindowsNextTraceCapability "$Description output could not be read: $($_.Exception.Message)"
    }
    return [pscustomobject]@{
        ExitCode = $exitCode
        Stdout = $stdout
        Stderr = $stderr
    }
}

function Parse-EcsNativeTracertOutput {
    param(
        [Parameter(Mandatory)][string]$Output,
        [Parameter(Mandatory)][ValidateSet('ipv4', 'ipv6')][string]$FamilyName,
        [Parameter(Mandatory)][int]$HopLimit,
        [Parameter(Mandatory)][string]$Target
    )

    $hops = @()
    $expectedHopNumber = 1
    $sawCanonicalHeader = $false
    $sawHopLine = $false
    $sawTraceComplete = $false
    $escapedTarget = [regex]::Escape($Target)
    $headerPattern = "^\s*Tracing\s+route\s+to\s+$escapedTarget\s+over\s+a\s+maximum\s+of\s+$HopLimit\s+hops\s*[.:]?\s*$"
    $completionPattern = '^\s*Trace\s+complete\.\s*$'
    $lines = @($Output -split "`r?`n")
    foreach ($line in $lines) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        if (-not $sawCanonicalHeader) {
            $headerMatch = [regex]::Match($line, $headerPattern, [System.Text.RegularExpressions.RegexOptions]::CultureInvariant)
            if (-not $headerMatch.Success) {
                Stop-EcsWindowsNextTraceCapability 'native tracert output is missing the canonical tracert header before hop lines'
            }
            $sawCanonicalHeader = $true
            continue
        }
        if ($sawTraceComplete) {
            Stop-EcsWindowsNextTraceCapability 'native tracert output contains text after Trace complete.'
        }
        $match = [regex]::Match($line, '^\s*(?<number>[0-9]{1,3})\s+(?<rest>.+?)\s*$')
        if (-not $match.Success) {
            if ($sawHopLine -and [regex]::IsMatch($line, $completionPattern, [System.Text.RegularExpressions.RegexOptions]::CultureInvariant)) {
                $sawTraceComplete = $true
                continue
            }
            if (-not $sawHopLine) {
                Stop-EcsWindowsNextTraceCapability 'native tracert output contains unexpected text before hop lines'
            }
            Stop-EcsWindowsNextTraceCapability 'native tracert output contains unexpected text after hop lines'
        }
        if ($sawTraceComplete) {
            Stop-EcsWindowsNextTraceCapability 'native tracert output contains hop lines after completion'
        }
        $sawHopLine = $true
        $hopNumber = [int]$match.Groups['number'].Value
        if ($hopNumber -lt 1 -or $hopNumber -gt $HopLimit) {
            Stop-EcsWindowsNextTraceCapability "native tracert output has hop number outside 1..${HopLimit}: $hopNumber"
        }
        if ($hopNumber -ne $expectedHopNumber) {
            Stop-EcsWindowsNextTraceCapability "native tracert output has non-contiguous hop lines: expected $expectedHopNumber, got $hopNumber"
        }
        $rest = $match.Groups['rest'].Value
        $addresses = @()
        foreach ($token in @($rest -split '\s+')) {
            $address = Get-EcsIPAddressToken -Token $token
            if ($null -eq $address) { continue }
            if (-not (Test-EcsAddressFamily -Address $address -FamilyName $FamilyName)) {
                Stop-EcsWindowsNextTraceCapability "native tracert output contains an address from the wrong family at hop $hopNumber"
            }
            $addresses += $address
        }
        if ($addresses.Count -eq 0 -and $rest -notmatch '(?i)^\s*(?:\*\s*){3}(?:request\s+timed\s+out\.)?\s*$') {
            Stop-EcsWindowsNextTraceCapability "native tracert hop $hopNumber has neither a valid address nor a strict timeout line"
        }
        if ($addresses.Count -gt 0 -and $rest -notmatch '(?i)\bms\b') {
            Stop-EcsWindowsNextTraceCapability "native tracert hop $hopNumber has an address but no millisecond measurement"
        }
        $responded = $addresses.Count -gt 0
        $publicResponded = $false
        foreach ($address in $addresses) {
            if (Test-EcsPublicIPAddress -Address $address) {
                $publicResponded = $true
                break
            }
        }
        $hops += [pscustomobject]@{
            hop = $hopNumber
            responded = $responded
            public_responded = $publicResponded
        }
        $expectedHopNumber++
    }
    if ($hops.Count -eq 0) {
        Stop-EcsWindowsNextTraceCapability 'native tracert output contains no parseable hop lines'
    }
    if (-not $sawCanonicalHeader) {
        Stop-EcsWindowsNextTraceCapability 'native tracert output is missing the canonical tracert header'
    }
    if (-not $sawTraceComplete) {
        Stop-EcsWindowsNextTraceCapability 'native tracert output is missing Trace complete. after the hop lines'
    }
    $respondingCount = @($hops | Where-Object { $_.responded }).Count
    $publicRespondingCount = @($hops | Where-Object { $_.public_responded }).Count
    return [pscustomobject]@{
        HopSlots = [int]$hops.Count
        RespondingHops = [int]$respondingCount
        PublicRespondingHops = [int]$publicRespondingCount
    }
}

function Test-EcsJsonArray {
    param([Parameter(Mandatory)][AllowNull()][object]$Value)
    if ($null -eq $Value -or $Value -is [string] -or $Value -is [System.Collections.IDictionary] -or
        $Value -is [System.Management.Automation.PSCustomObject]) {
        return $false
    }
    return $Value -is [System.Collections.IEnumerable]
}

function Convert-EcsStrictTraceAddress {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Value,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Value) { return $null }
    if ($Value -is [string]) {
        if ([string]::IsNullOrWhiteSpace([string]$Value)) { return $null }
        $address = Get-EcsIPAddressToken -Token ([string]$Value)
        if ($null -eq $address) {
            Stop-EcsWindowsNextTraceCapability "$Context contains an invalid IP address"
        }
        return $address
    }
    if ($Value -is [System.Collections.IEnumerable] -and
        $Value -isnot [System.Collections.IDictionary] -and
        $Value -isnot [System.Management.Automation.PSCustomObject]) {
        Stop-EcsWindowsNextTraceCapability "$Context has an unexpected array value"
    }
    if ($Value -isnot [System.Collections.IDictionary] -and
        $Value -isnot [System.Management.Automation.PSCustomObject]) {
        Stop-EcsWindowsNextTraceCapability "$Context is neither an IP string nor an address object"
    }
    $properties = @($Value.PSObject.Properties)
    if ($properties.Count -eq 0) {
        Stop-EcsWindowsNextTraceCapability "$Context is an empty address object"
    }
    $known = @('IP', 'Ip', 'Address', 'Addr', 'Host', 'Hostname', 'PTR')
    if (@($properties | Where-Object { $_.Name -in $known }).Count -eq 0) {
        Stop-EcsWindowsNextTraceCapability "$Context has an unrecognized address object"
    }
    $addressProperties = @($properties | Where-Object { $_.Name -in @('IP', 'Ip', 'Address', 'Addr') })
    $addressNames = @($addressProperties | ForEach-Object { $_.Name.ToLowerInvariant() } | Sort-Object -Unique)
    if ($addressNames.Count -gt 1 -or ($addressNames.Count -eq 1 -and $addressProperties.Count -ne 1)) {
        Stop-EcsWindowsNextTraceCapability "$Context has multiple address properties"
    }
    foreach ($property in $addressProperties) {
        $address = Convert-EcsStrictTraceAddress -Value $property.Value -Context "$Context.$($property.Name)"
        if ($null -ne $address) { return $address }
    }
    return $null
}

function Get-EcsNextTraceProbeAddresses {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Probe,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Probe) {
        Stop-EcsWindowsNextTraceCapability "$Context is null"
    }
    if ($Probe -is [string]) {
        $address = Convert-EcsStrictTraceAddress -Value $Probe -Context $Context
        if ($null -eq $address) {
            Stop-EcsWindowsNextTraceCapability "$Context is not a valid IP address"
        }
        return @($address)
    }
    if ($Probe -is [System.Collections.IEnumerable] -and
        $Probe -isnot [System.Collections.IDictionary] -and
        $Probe -isnot [System.Management.Automation.PSCustomObject]) {
        $items = @($Probe)
        if ($items.Count -eq 0) {
            Stop-EcsWindowsNextTraceCapability "$Context contains an empty nested array"
        }
        Stop-EcsWindowsNextTraceCapability "$Context contains an unexpected nested array"
    }
    $properties = @($Probe.PSObject.Properties)
    if ($properties.Count -eq 0) {
        Stop-EcsWindowsNextTraceCapability "$Context is an empty probe object"
    }
    $known = @('Success', 'Address', 'IP', 'Ip', 'RTT', 'Latency', 'Delay', 'Time', 'AvgRTT', 'ASN', 'ASNumber', 'AS', 'ASNO', 'Asnumber', 'Geo', 'Network', 'ASName', 'Organization', 'Org', 'ISP', 'Isp', 'Owner', 'Host', 'Hostname', 'PTR')
    if (@($properties | Where-Object { $_.Name -in $known }).Count -eq 0) {
        Stop-EcsWindowsNextTraceCapability "$Context is not a recognized NextTrace probe object"
    }
    $successProperties = @($properties | Where-Object { $_.Name -ieq 'Success' })
    if ($successProperties.Count -gt 1) {
        Stop-EcsWindowsNextTraceCapability "$Context has multiple Success properties"
    }
    if ($successProperties.Count -eq 1 -and $successProperties[0].Value -isnot [bool]) {
        Stop-EcsWindowsNextTraceCapability "$Context.Success is not boolean"
    }
    $hasSuccess = $successProperties.Count -eq 1
    $success = if ($hasSuccess) { [bool]$successProperties[0].Value } else { $null }
    $addresses = @()
    $addressProperties = @($properties | Where-Object { $_.Name -in @('Address', 'IP', 'Ip') })
    $addressNames = @($addressProperties | ForEach-Object { $_.Name.ToLowerInvariant() } | Sort-Object -Unique)
    if ($addressNames.Count -gt 1 -or ($addressNames.Count -eq 1 -and $addressProperties.Count -ne 1)) {
        Stop-EcsWindowsNextTraceCapability "$Context has multiple address properties"
    }
    foreach ($property in $addressProperties) {
        $address = Convert-EcsStrictTraceAddress -Value $property.Value -Context "$Context.$($property.Name)"
        if ($null -ne $address) { $addresses += $address }
    }
    if ($addresses.Count -gt 0 -and $hasSuccess -and -not $success) {
        Stop-EcsWindowsNextTraceCapability "$Context reports Success=false with a responding address"
    }
    if ($addresses.Count -eq 0 -and (-not $hasSuccess -or $success)) {
        Stop-EcsWindowsNextTraceCapability "$Context has no address and no explicit Success=false response"
    }
    return @($addresses)
}

function Parse-EcsNextTraceOutput {
    param(
        [Parameter(Mandatory)][string]$Output,
        [Parameter(Mandatory)][ValidateSet('ipv4', 'ipv6')][string]$FamilyName,
        [Parameter(Mandatory)][int]$HopLimit
    )
    if ([string]::IsNullOrWhiteSpace($Output)) {
        Stop-EcsWindowsNextTraceCapability 'direct NextTrace stdout is empty'
    }
    try {
        $payload = ConvertFrom-Json -InputObject $Output -ErrorAction Stop
    } catch {
        Stop-EcsWindowsNextTraceCapability "direct NextTrace stdout is invalid JSON: $($_.Exception.Message)"
    }
    if ($null -eq $payload -or
        ($payload -isnot [System.Collections.IDictionary] -and
         $payload -isnot [System.Management.Automation.PSCustomObject])) {
        Stop-EcsWindowsNextTraceCapability 'direct NextTrace JSON root is not an object'
    }
    $hopProperties = @($payload.PSObject.Properties | Where-Object { $_.Name -ieq 'Hops' })
    if ($hopProperties.Count -ne 1 -or -not (Test-EcsJsonArray -Value $hopProperties[0].Value)) {
        Stop-EcsWindowsNextTraceCapability 'direct NextTrace JSON has no array-valued Hops property'
    }
    $slots = @($hopProperties[0].Value)
    if ($slots.Count -eq 0 -or $slots.Count -gt $HopLimit) {
        Stop-EcsWindowsNextTraceCapability "direct NextTrace JSON has invalid hop slot count: $($slots.Count)"
    }
    $respondingCount = 0
    $publicRespondingCount = 0
    for ($slotIndex = 0; $slotIndex -lt $slots.Count; $slotIndex++) {
        $slot = $slots[$slotIndex]
        if (-not (Test-EcsJsonArray -Value $slot)) {
            Stop-EcsWindowsNextTraceCapability "direct NextTrace Hops[$slotIndex] is not an array"
        }
        $slotResponded = $false
        $slotPublicResponded = $false
        $probes = @($slot)
        for ($probeIndex = 0; $probeIndex -lt $probes.Count; $probeIndex++) {
            $addresses = @(Get-EcsNextTraceProbeAddresses -Probe $probes[$probeIndex] -Context "direct NextTrace Hops[$slotIndex][$probeIndex]")
            foreach ($address in $addresses) {
                if (-not (Test-EcsAddressFamily -Address $address -FamilyName $FamilyName)) {
                    Stop-EcsWindowsNextTraceCapability "direct NextTrace Hops[$slotIndex][$probeIndex] contains an address from the wrong family"
                }
                $slotResponded = $true
                if (Test-EcsPublicIPAddress -Address $address) {
                    $slotPublicResponded = $true
                }
            }
        }
        if ($slotResponded) { $respondingCount++ }
        if ($slotPublicResponded) { $publicRespondingCount++ }
    }
    return [pscustomobject]@{
        HopSlots = [int]$slots.Count
        RespondingHops = [int]$respondingCount
        PublicRespondingHops = [int]$publicRespondingCount
    }
}

function Assert-EcsCapabilityEnvelope {
    param(
        [Parameter(Mandatory)][string]$SchemaVersion,
        [Parameter(Mandatory)][ValidateSet('ipv4', 'ipv6')][string]$FamilyName,
        [Parameter(Mandatory)][string]$TargetValue,
        [Parameter(Mandatory)][string]$NextTraceFullPath,
        [Parameter(Mandatory)][string]$ExpectedHash,
        [Parameter(Mandatory)][string]$ActualHash,
        [Parameter(Mandatory)][int]$MaxHopsValue
    )
    if ($SchemaVersion -cne $CapabilitySchema) {
        Stop-EcsWindowsNextTraceCapability "capability evidence schema must be $CapabilitySchema"
    }
    $canonicalTarget = if ($FamilyName -ceq 'ipv4') { $CanonicalIPv4Target } else { $CanonicalIPv6Target }
    if ($TargetValue -cne $canonicalTarget) {
        Stop-EcsWindowsNextTraceCapability "target does not match the canonical $FamilyName capability target $canonicalTarget"
    }
    if ([string]::IsNullOrWhiteSpace($NextTraceFullPath) -or -not [IO.Path]::IsPathRooted($NextTraceFullPath) -or
        [IO.Path]::GetFileName($NextTraceFullPath) -ine 'nexttrace-tiny.exe') {
        Stop-EcsWindowsNextTraceCapability 'capability evidence has an invalid staged NextTrace path'
    }
    if ($ExpectedHash -notmatch '^[0-9A-Fa-f]{64}$') {
        Stop-EcsWindowsNextTraceCapability 'capability evidence has an invalid expected SHA-256'
    }
    if ($ActualHash -notmatch '^[0-9a-f]{64}$') {
        Stop-EcsWindowsNextTraceCapability 'capability evidence has an invalid actual SHA-256'
    }
    if ($ActualHash -cne $ExpectedHash.ToLowerInvariant()) {
        Stop-EcsWindowsNextTraceCapability "capability evidence SHA-256 mismatch: expected $($ExpectedHash.ToLowerInvariant()), got $ActualHash"
    }
    if ($MaxHopsValue -ne $CanonicalMaxHops) {
        Stop-EcsWindowsNextTraceCapability "capability evidence max_hops must remain $CanonicalMaxHops"
    }
}

function Assert-EcsStringArrayExact {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Value,
        [Parameter(Mandatory)][string[]]$Expected,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Value -or $Value -is [string] -or
        $Value -is [System.Collections.IDictionary] -or
        $Value -is [System.Management.Automation.PSCustomObject] -or
        $Value -isnot [System.Collections.IEnumerable]) {
        Stop-EcsWindowsNextTraceCapability "$Context must be an array of strings"
    }
    $actual = @($Value)
    if ($actual.Count -ne $Expected.Count) {
        Stop-EcsWindowsNextTraceCapability "$Context has the wrong number of arguments"
    }
    for ($index = 0; $index -lt $Expected.Count; $index++) {
        if ($actual[$index] -isnot [string] -or $actual[$index] -cne $Expected[$index]) {
            Stop-EcsWindowsNextTraceCapability "$Context does not match the canonical argument vector"
        }
    }
}

function Assert-EcsCapabilityProbeFacts {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Facts,
        [Parameter(Mandatory)][string]$Context
    )
    $expected = @('ExecutionStatus', 'ParseStatus', 'ExitCode', 'HopSlots', 'RespondingHops', 'PublicRespondingHops')
    Assert-EcsExactPropertyNames -Object $Facts -Expected $expected -Context $Context
    $executionStatus = [string](Get-EcsObjectPropertyValue -Object $Facts -Name 'ExecutionStatus' -Context $Context)
    $parseStatus = [string](Get-EcsObjectPropertyValue -Object $Facts -Name 'ParseStatus' -Context $Context)
    if ($executionStatus -cne 'completed' -or $parseStatus -cne 'parsed') {
        Stop-EcsWindowsNextTraceCapability "$Context did not complete and parse successfully"
    }
    $exitCode = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $Facts -Name 'ExitCode' -Context $Context) -Context "$Context.ExitCode"
    if ($exitCode -ne 0) {
        Stop-EcsWindowsNextTraceCapability "$Context exited with code $exitCode"
    }
    $hopSlots = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $Facts -Name 'HopSlots' -Context $Context) -Context "$Context.HopSlots"
    $respondingHops = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $Facts -Name 'RespondingHops' -Context $Context) -Context "$Context.RespondingHops"
    $publicRespondingHops = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $Facts -Name 'PublicRespondingHops' -Context $Context) -Context "$Context.PublicRespondingHops"
    if ($hopSlots -lt 1 -or $hopSlots -gt $CanonicalMaxHops) {
        Stop-EcsWindowsNextTraceCapability "$Context.HopSlots is outside 1..$CanonicalMaxHops"
    }
    if ($respondingHops -lt 0 -or $respondingHops -gt $hopSlots) {
        Stop-EcsWindowsNextTraceCapability "$Context.RespondingHops is outside the hop-slot range"
    }
    if ($publicRespondingHops -lt 0 -or $publicRespondingHops -gt $respondingHops) {
        Stop-EcsWindowsNextTraceCapability "$Context.PublicRespondingHops is outside the responding-hop range"
    }
    return [pscustomobject]@{
        ExecutionStatus = $executionStatus
        ParseStatus = $parseStatus
        ExitCode = [int]$exitCode
        HopSlots = [int]$hopSlots
        RespondingHops = [int]$respondingHops
        PublicRespondingHops = [int]$publicRespondingHops
    }
}

function Get-EcsCapabilityDecision {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$NativeFacts,
        [Parameter(Mandatory)][AllowNull()][object]$DirectFacts
    )
    $native = Assert-EcsCapabilityProbeFacts -Facts $NativeFacts -Context 'native capability facts'
    $direct = Assert-EcsCapabilityProbeFacts -Facts $DirectFacts -Context 'direct capability facts'
    $nativeResponded = $native.RespondingHops -gt 0
    $directResponded = $direct.RespondingHops -gt 0
    $nativePublic = $native.PublicRespondingHops -gt 0
    $directPublic = $direct.PublicRespondingHops -gt 0

    if ($nativeResponded -and -not $directResponded) {
        Stop-EcsWindowsNextTraceCapability 'native tracert observed responding hops but direct staged NextTrace observed none'
    }
    if ($nativePublic -and -not $directPublic) {
        Stop-EcsWindowsNextTraceCapability 'native tracert observed a public responding hop but direct staged NextTrace observed no legal public responding hop'
    }
    if ($directPublic) {
        return [pscustomobject]@{
            Decision = 'available'
            LiveNetworkNotProven = $false
            Reason = "public-hop-observed: direct staged NextTrace observed a legal public responding hop (native_public=$($native.PublicRespondingHops); direct_public=$($direct.PublicRespondingHops))"
        }
    }
    return [pscustomobject]@{
        Decision = 'not-testable'
        LiveNetworkNotProven = $true
        Reason = "live-network-not-proven: both probes executed and parsed successfully but neither observed a legal public responding hop (native responding=$($native.RespondingHops); direct responding=$($direct.RespondingHops))"
    }
}

function Assert-EcsCapabilityEvidence {
    param(
        [Parameter(Mandatory)][AllowNull()][object]$Evidence,
        [Parameter(Mandatory)][string]$ExpectedEvidencePath,
        [Parameter(Mandatory)][string]$ExpectedRawDirectory,
        [Parameter(Mandatory)][string]$ExpectedNextTracePath
    )
    $topFields = @('schema_version', 'evidence_path', 'raw_output_directory', 'family', 'target', 'max_hops', 'decision', 'live_network_not_proven', 'reason', 'nexttrace_path', 'nexttrace_expected_sha256', 'nexttrace_sha256', 'native_tracert', 'direct_nexttrace', 'observed')
    Assert-EcsExactPropertyNames -Object $Evidence -Expected $topFields -Context 'capability evidence'
    $schemaVersion = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'schema_version' -Context 'capability evidence') -Context 'capability evidence.schema_version'
    $familyName = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'family' -Context 'capability evidence') -Context 'capability evidence.family'
    $targetValue = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'target' -Context 'capability evidence') -Context 'capability evidence.target'
    $maxHopsValue = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'max_hops' -Context 'capability evidence') -Context 'capability evidence.max_hops'
    $reasonValue = Get-EcsObjectPropertyValue -Object $Evidence -Name 'reason' -Context 'capability evidence'
    if ($reasonValue -isnot [string] -or [string]::IsNullOrWhiteSpace([string]$reasonValue)) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence reason must be a non-empty string'
    }
    $nextTracePath = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'nexttrace_path' -Context 'capability evidence') -Context 'capability evidence.nexttrace_path'
    $expectedHash = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'nexttrace_expected_sha256' -Context 'capability evidence') -Context 'capability evidence.nexttrace_expected_sha256'
    $actualHash = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'nexttrace_sha256' -Context 'capability evidence') -Context 'capability evidence.nexttrace_sha256'
    Assert-EcsCapabilityEnvelope -SchemaVersion $schemaVersion -FamilyName $familyName -TargetValue $targetValue -NextTraceFullPath $nextTracePath -ExpectedHash $expectedHash -ActualHash $actualHash -MaxHopsValue ([int]$maxHopsValue)
    $canonicalNativePath = Get-EcsCanonicalNativeExecutablePath
    if ($nextTracePath -cne $ExpectedNextTracePath) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence nexttrace_path does not match the staged executable path'
    }
    $evidencePath = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'evidence_path' -Context 'capability evidence') -Context 'capability evidence.evidence_path'
    $rawDirectory = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'raw_output_directory' -Context 'capability evidence') -Context 'capability evidence.raw_output_directory'
    if ($evidencePath -cne $ExpectedEvidencePath -or $rawDirectory -cne $ExpectedRawDirectory) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence path fields do not match the captured evidence paths'
    }

    $nativeEvidence = Get-EcsObjectPropertyValue -Object $Evidence -Name 'native_tracert' -Context 'capability evidence'
    $directEvidence = Get-EcsObjectPropertyValue -Object $Evidence -Name 'direct_nexttrace' -Context 'capability evidence'
    $probeFields = @('executable_path', 'arguments', 'raw_stdout_path', 'raw_stderr_path', 'exit_code', 'execution_status', 'parse_status', 'hop_slots', 'responding_hops', 'public_responding_hops')
    Assert-EcsExactPropertyNames -Object $nativeEvidence -Expected $probeFields -Context 'capability evidence.native_tracert'
    Assert-EcsExactPropertyNames -Object $directEvidence -Expected $probeFields -Context 'capability evidence.direct_nexttrace'
    $nativeExecutablePath = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'executable_path' -Context 'capability evidence.native_tracert') -Context 'capability evidence.native_tracert.executable_path'
    $directExecutablePath = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $directEvidence -Name 'executable_path' -Context 'capability evidence.direct_nexttrace') -Context 'capability evidence.direct_nexttrace.executable_path'
    if ($nativeExecutablePath -cne $canonicalNativePath) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence native executable path is not the canonical System32 path'
    }
    if ($directExecutablePath -cne $ExpectedNextTracePath) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence direct executable path mismatch'
    }
    $familyFlag = if ($familyName -ceq 'ipv4') { '-4' } else { '-6' }
    $canonicalNativeArguments = @('-d', $familyFlag, '-h', [string]$maxHopsValue, $targetValue)
    $canonicalDirectArguments = @($familyFlag, '--no-color', '--json', '-M', '--max-hops', [string]$maxHopsValue, '--queries', '1', '--parallel-requests', '1', '--timeout', '1000', $targetValue)
    Assert-EcsStringArrayExact -Value (Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'arguments' -Context 'capability evidence.native_tracert') -Expected $canonicalNativeArguments -Context 'capability evidence.native_tracert.arguments'
    Assert-EcsStringArrayExact -Value (Get-EcsObjectPropertyValue -Object $directEvidence -Name 'arguments' -Context 'capability evidence.direct_nexttrace') -Expected $canonicalDirectArguments -Context 'capability evidence.direct_nexttrace.arguments'

    foreach ($probe in @($nativeEvidence, $directEvidence)) {
        foreach ($field in @('raw_stdout_path', 'raw_stderr_path')) {
            $rawPath = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $probe -Name $field -Context 'capability evidence probe') -Context "capability evidence probe.$field"
            if ([string]::IsNullOrWhiteSpace($rawPath) -or -not [IO.Path]::IsPathRooted($rawPath) -or
                -not (Test-Path -LiteralPath $rawPath -PathType Leaf)) {
                Stop-EcsWindowsNextTraceCapability "capability evidence raw output path is missing: $rawPath"
            }
        }
    }

    $observed = Get-EcsObjectPropertyValue -Object $Evidence -Name 'observed' -Context 'capability evidence'
    $observedFields = @('native_hop_slots', 'native_responding_hops', 'native_public_responding_hops', 'direct_hop_slots', 'direct_responding_hops', 'direct_public_responding_hops')
    Assert-EcsExactPropertyNames -Object $observed -Expected $observedFields -Context 'capability evidence.observed'
    $observedPairs = @(
        [pscustomobject]@{ Observed = 'native_hop_slots'; Probe = 'native_tracert'; Field = 'hop_slots' }
        [pscustomobject]@{ Observed = 'native_responding_hops'; Probe = 'native_tracert'; Field = 'responding_hops' }
        [pscustomobject]@{ Observed = 'native_public_responding_hops'; Probe = 'native_tracert'; Field = 'public_responding_hops' }
        [pscustomobject]@{ Observed = 'direct_hop_slots'; Probe = 'direct_nexttrace'; Field = 'hop_slots' }
        [pscustomobject]@{ Observed = 'direct_responding_hops'; Probe = 'direct_nexttrace'; Field = 'responding_hops' }
        [pscustomobject]@{ Observed = 'direct_public_responding_hops'; Probe = 'direct_nexttrace'; Field = 'public_responding_hops' }
    )
    foreach ($pair in $observedPairs) {
        $observedValue = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $observed -Name $pair.Observed -Context 'capability evidence.observed') -Context "capability evidence.observed.$($pair.Observed)"
        $probeObject = if ($pair.Probe -ceq 'native_tracert') { $nativeEvidence } else { $directEvidence }
        $probeValue = Assert-EcsIntegerValue -Value (Get-EcsObjectPropertyValue -Object $probeObject -Name $pair.Field -Context "capability evidence.$($pair.Probe)") -Context "capability evidence.$($pair.Probe).$($pair.Field)"
        if ($observedValue -ne $probeValue) {
            Stop-EcsWindowsNextTraceCapability "capability evidence observed.$($pair.Observed) does not match $($pair.Probe).$($pair.Field)"
        }
    }
    $nativeFacts = [pscustomobject]@{
        ExecutionStatus = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'execution_status' -Context 'capability evidence.native_tracert') -Context 'capability evidence.native_tracert.execution_status'
        ParseStatus = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'parse_status' -Context 'capability evidence.native_tracert') -Context 'capability evidence.native_tracert.parse_status'
        ExitCode = Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'exit_code' -Context 'capability evidence.native_tracert'
        HopSlots = Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'hop_slots' -Context 'capability evidence.native_tracert'
        RespondingHops = Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'responding_hops' -Context 'capability evidence.native_tracert'
        PublicRespondingHops = Get-EcsObjectPropertyValue -Object $nativeEvidence -Name 'public_responding_hops' -Context 'capability evidence.native_tracert'
    }
    $directFacts = [pscustomobject]@{
        ExecutionStatus = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $directEvidence -Name 'execution_status' -Context 'capability evidence.direct_nexttrace') -Context 'capability evidence.direct_nexttrace.execution_status'
        ParseStatus = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $directEvidence -Name 'parse_status' -Context 'capability evidence.direct_nexttrace') -Context 'capability evidence.direct_nexttrace.parse_status'
        ExitCode = Get-EcsObjectPropertyValue -Object $directEvidence -Name 'exit_code' -Context 'capability evidence.direct_nexttrace'
        HopSlots = Get-EcsObjectPropertyValue -Object $directEvidence -Name 'hop_slots' -Context 'capability evidence.direct_nexttrace'
        RespondingHops = Get-EcsObjectPropertyValue -Object $directEvidence -Name 'responding_hops' -Context 'capability evidence.direct_nexttrace'
        PublicRespondingHops = Get-EcsObjectPropertyValue -Object $directEvidence -Name 'public_responding_hops' -Context 'capability evidence.direct_nexttrace'
    }
    $decision = Assert-EcsStringValue -Value (Get-EcsObjectPropertyValue -Object $Evidence -Name 'decision' -Context 'capability evidence') -Context 'capability evidence.decision'
    $liveNetworkNotProven = Get-EcsObjectPropertyValue -Object $Evidence -Name 'live_network_not_proven' -Context 'capability evidence'
    if ($liveNetworkNotProven -isnot [bool]) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence live_network_not_proven must be boolean'
    }
    if ($familyName -ceq 'ipv6' -and $decision -ceq 'ipv6-unavailable') {
        if ($nativeFacts.ExitCode -ne $null -or $directFacts.ExitCode -ne $null -or
            $nativeFacts.ExecutionStatus -cne 'not-run-capability-unavailable' -or
            $directFacts.ExecutionStatus -cne 'not-run-capability-unavailable' -or
            $nativeFacts.ParseStatus -cne 'not-run-capability-unavailable' -or
            $directFacts.ParseStatus -cne 'not-run-capability-unavailable') {
            Stop-EcsWindowsNextTraceCapability 'IPv6-unavailable evidence contains executed probe facts'
        }
        if ([bool]$liveNetworkNotProven -ne $true) {
            Stop-EcsWindowsNextTraceCapability 'IPv6-unavailable evidence must mark live_network_not_proven=true'
        }
        foreach ($value in @(
            (Assert-EcsIntegerValue -Value $nativeFacts.HopSlots -Context 'IPv6-unavailable native hop_slots'),
            (Assert-EcsIntegerValue -Value $nativeFacts.RespondingHops -Context 'IPv6-unavailable native responding_hops'),
            (Assert-EcsIntegerValue -Value $nativeFacts.PublicRespondingHops -Context 'IPv6-unavailable native public_responding_hops'),
            (Assert-EcsIntegerValue -Value $directFacts.HopSlots -Context 'IPv6-unavailable direct hop_slots'),
            (Assert-EcsIntegerValue -Value $directFacts.RespondingHops -Context 'IPv6-unavailable direct responding_hops'),
            (Assert-EcsIntegerValue -Value $directFacts.PublicRespondingHops -Context 'IPv6-unavailable direct public_responding_hops')
        )) {
            if ($value -ne 0) {
                Stop-EcsWindowsNextTraceCapability 'IPv6-unavailable evidence must contain zero probe facts'
            }
        }
        return
    }
    if ($decision -notin @('available', 'not-testable') -or $familyName -notin @('ipv4', 'ipv6')) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence has an invalid decision or family'
    }
    if ($familyName -eq 'ipv6' -and $decision -eq 'ipv6-unavailable') {
        Stop-EcsWindowsNextTraceCapability 'IPv6-unavailable is only valid with an explicit unavailable probe result'
    }
    $classified = Get-EcsCapabilityDecision -NativeFacts $nativeFacts -DirectFacts $directFacts
    if ($classified.Decision -cne $decision -or [bool]$classified.LiveNetworkNotProven -ne [bool]$liveNetworkNotProven) {
        Stop-EcsWindowsNextTraceCapability 'capability evidence decision does not match the validated probe facts'
    }
}

function Get-EcsIPv6Capability {
    function Test-EcsWindowsObjectRecord {
        param([Parameter(Mandatory)][AllowNull()][object]$Value)
        if ($null -eq $Value) { return $false }
        if ($Value -is [System.Collections.IDictionary] -or $Value -is [System.Management.Automation.PSCustomObject]) {
            return $true
        }
        # NetTCPIP returns Microsoft.Management.Infrastructure.CimInstance values
        # for both Get-NetIPAddress and Get-NetRoute on Windows.
        return $Value.GetType().FullName -ceq 'Microsoft.Management.Infrastructure.CimInstance'
    }

    try {
        $addressItems = @(Get-NetIPAddress -AddressFamily IPv6 -AddressState Preferred -ErrorAction Stop)
        $routeItems = @(Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction Stop)
    } catch {
        Stop-EcsWindowsNextTraceCapability "IPv6 capability probe failed: $($_.Exception.Message)"
    }
    $globalAddressCount = 0
    foreach ($item in $addressItems) {
        if (-not (Test-EcsWindowsObjectRecord -Value $item)) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a non-object address'
        }
        $properties = @($item.PSObject.Properties | Where-Object { $_.Name -ieq 'IPAddress' })
        if ($properties.Count -ne 1) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned an address without a unique IPAddress property'
        }
        $addressText = [string]$properties[0].Value
        if ([string]::IsNullOrWhiteSpace($addressText)) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned an empty IPAddress'
        }
        $address = Get-EcsIPAddressToken -Token $addressText
        if ($null -eq $address -or $address.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetworkV6) {
            Stop-EcsWindowsNextTraceCapability "IPv6 capability probe returned an invalid IPv6 address: $addressText"
        }
        if (Test-EcsPublicIPAddress -Address $address) { $globalAddressCount++ }
    }
    $defaultRouteCount = 0
    foreach ($route in $routeItems) {
        if (-not (Test-EcsWindowsObjectRecord -Value $route)) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a non-object default route'
        }
        $destinationProperties = @($route.PSObject.Properties | Where-Object { $_.Name -ieq 'DestinationPrefix' })
        if ($destinationProperties.Count -ne 1 -or [string]$destinationProperties[0].Value -cne '::/0') {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a route with the wrong destination prefix'
        }
        $interfaceProperties = @($route.PSObject.Properties | Where-Object { $_.Name -ieq 'InterfaceIndex' })
        if ($interfaceProperties.Count -ne 1) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a default route without a unique InterfaceIndex property'
        }
        $interfaceIndex = Assert-EcsIntegerValue -Value $interfaceProperties[0].Value -Context 'IPv6 capability default route InterfaceIndex'
        if ($interfaceIndex -le 0) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a default route with an invalid InterfaceIndex'
        }
        $nextHopProperties = @($route.PSObject.Properties | Where-Object { $_.Name -ieq 'NextHop' })
        if ($nextHopProperties.Count -ne 1) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a default route without a unique NextHop property'
        }
        $nextHopText = [string]$nextHopProperties[0].Value
        $nextHop = Get-EcsIPAddressToken -Token $nextHopText
        if ($null -eq $nextHop -or $nextHop.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetworkV6) {
            Stop-EcsWindowsNextTraceCapability "IPv6 capability probe returned an invalid default-route NextHop: $nextHopText"
        }
        $stateProperties = @($route.PSObject.Properties | Where-Object { $_.Name -ieq 'State' })
        if ($stateProperties.Count -ne 1) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a default route without a unique State property'
        }
        $state = [string]$stateProperties[0].Value
        if ([string]::IsNullOrWhiteSpace($state)) {
            Stop-EcsWindowsNextTraceCapability 'IPv6 capability probe returned a default route with an empty State'
        }
        if ($state -notin @('Alive', 'Permanent', 'Dead', 'Invalid', 'Probe', 'Unreachable')) {
            Stop-EcsWindowsNextTraceCapability "IPv6 capability probe returned an unknown default-route State: $state"
        }
        # Match the existing gate: Probe is a usable route state; only states
        # explicitly treated as unusable by the gate are unavailable. Unknown
        # states remain a hard failure above instead of being silently ignored.
        if ($state -notin @('Dead', 'Invalid', 'Unreachable')) {
            $defaultRouteCount++
        }
    }
    return [pscustomobject]@{
        GlobalAddresses = [int]$globalAddressCount
        DefaultRoutes = [int]$defaultRouteCount
        Available = $globalAddressCount -gt 0 -and $defaultRouteCount -gt 0
    }
}

function Invoke-EcsWindowsNextTraceCapability {
try {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'windows-nexttrace-capability: Windows host required; refusing to emulate Windows probes'
    }

    if ($MaxHops -ne $CanonicalMaxHops) {
        Stop-EcsWindowsNextTraceCapability "MaxHops must remain the canonical value $CanonicalMaxHops"
    }

    $familyName = if ($Family -ieq 'IPv4') { 'ipv4' } else { 'ipv6' }
    $familyFlag = if ($familyName -ceq 'ipv4') { '-4' } else { '-6' }
    $canonicalTarget = if ($familyName -ceq 'ipv4') { $CanonicalIPv4Target } else { $CanonicalIPv6Target }
    if ($Target -cne $canonicalTarget) {
        Stop-EcsWindowsNextTraceCapability "target does not match the canonical $familyName capability target $canonicalTarget"
    }
    $targetAddress = Get-EcsIPAddressToken -Token $Target
    if ($null -eq $targetAddress -or -not (Test-EcsAddressFamily -Address $targetAddress -FamilyName $familyName)) {
        Stop-EcsWindowsNextTraceCapability "target does not match family $Family"
    }

    $nextTraceFull = Resolve-EcsAbsolutePath -Path $NextTracePath -Description 'staged NextTrace executable'
    if ([IO.Path]::GetFileName($nextTraceFull) -ine 'nexttrace-tiny.exe') {
        Stop-EcsWindowsNextTraceCapability "staged NextTrace executable must be named nexttrace-tiny.exe: $nextTraceFull"
    }
    if (-not (Test-Path -LiteralPath $nextTraceFull -PathType Leaf)) {
        Stop-EcsWindowsNextTraceCapability "staged NextTrace executable does not exist: $nextTraceFull"
    }
    $expectedHash = $ExpectedSha256.ToLowerInvariant()
    try {
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $nextTraceFull -ErrorAction Stop).Hash.ToLowerInvariant()
    } catch {
        Stop-EcsWindowsNextTraceCapability "cannot hash staged NextTrace executable: $($_.Exception.Message)"
    }
    if ($actualHash -cne $expectedHash) {
        Stop-EcsWindowsNextTraceCapability "staged NextTrace SHA-256 mismatch: expected $expectedHash, got $actualHash"
    }
    Assert-EcsCapabilityEnvelope -SchemaVersion $CapabilitySchema -FamilyName $familyName -TargetValue $Target -NextTraceFullPath $nextTraceFull -ExpectedHash $ExpectedSha256 -ActualHash $actualHash -MaxHopsValue $MaxHops

    $evidenceFull = Resolve-EcsAbsolutePath -Path $EvidencePath -Description 'evidence path'
    if ([string]::IsNullOrWhiteSpace([IO.Path]::GetFileName($evidenceFull))) {
        Stop-EcsWindowsNextTraceCapability 'evidence path must name a file'
    }
    if (Test-Path -LiteralPath $evidenceFull -PathType Container) {
        Stop-EcsWindowsNextTraceCapability "evidence path is a directory: $evidenceFull"
    }
    if (Test-Path -LiteralPath $evidenceFull -PathType Leaf) {
        Stop-EcsWindowsNextTraceCapability "evidence path already exists; refusing to overwrite: $evidenceFull"
    }
    $evidenceDirectory = [IO.Path]::GetDirectoryName($evidenceFull)
    if ([string]::IsNullOrWhiteSpace($evidenceDirectory)) {
        Stop-EcsWindowsNextTraceCapability 'evidence path has no parent directory'
    }
    New-Item -ItemType Directory -Force -Path $evidenceDirectory | Out-Null

    $tracertFull = Get-EcsCanonicalNativeExecutablePath

    $rawDirectory = Join-Path $evidenceDirectory ("{0}.raw-{1}" -f [IO.Path]::GetFileName($evidenceFull), [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $rawDirectory | Out-Null
    $nativeStdoutPath = Join-Path $rawDirectory 'native_tracert.stdout.txt'
    $nativeStderrPath = Join-Path $rawDirectory 'native_tracert.stderr.txt'
    $directStdoutPath = Join-Path $rawDirectory 'direct_nexttrace.stdout.txt'
    $directStderrPath = Join-Path $rawDirectory 'direct_nexttrace.stderr.txt'
    Write-EcsUtf8Text -Path $nativeStdoutPath -Content ''
    Write-EcsUtf8Text -Path $nativeStderrPath -Content ''
    Write-EcsUtf8Text -Path $directStdoutPath -Content ''
    Write-EcsUtf8Text -Path $directStderrPath -Content ''

    $nativeArguments = @('-d', $familyFlag, '-h', [string]$MaxHops, $Target)
    $directArguments = @($familyFlag, '--no-color', '--json', '-M', '--max-hops', [string]$MaxHops, '--queries', '1', '--parallel-requests', '1', '--timeout', '1000', $Target)

    $runProbes = $true
    $ipv6Capability = $null
    if ($familyName -ceq 'ipv6') {
        $ipv6Capability = Get-EcsIPv6Capability
        $runProbes = [bool]$ipv6Capability.Available
    }
    if (-not $runProbes) {
        $reason = "explicit IPv6 capability probe found no global IPv6 address or usable default route (global_addresses=$($ipv6Capability.GlobalAddresses); default_routes=$($ipv6Capability.DefaultRoutes))"
        Write-EcsUtf8Text -Path $nativeStdoutPath -Content "not run: $reason"
        Write-EcsUtf8Text -Path $nativeStderrPath -Content ''
        Write-EcsUtf8Text -Path $directStdoutPath -Content "not run: $reason"
        Write-EcsUtf8Text -Path $directStderrPath -Content ''
        $nativeEvidence = [ordered]@{
            executable_path = $tracertFull
            arguments = @($nativeArguments)
            raw_stdout_path = $nativeStdoutPath
            raw_stderr_path = $nativeStderrPath
            exit_code = $null
            execution_status = 'not-run-capability-unavailable'
            parse_status = 'not-run-capability-unavailable'
            hop_slots = 0
            responding_hops = 0
            public_responding_hops = 0
        }
        $directEvidence = [ordered]@{
            executable_path = $nextTraceFull
            arguments = @($directArguments)
            raw_stdout_path = $directStdoutPath
            raw_stderr_path = $directStderrPath
            exit_code = $null
            execution_status = 'not-run-capability-unavailable'
            parse_status = 'not-run-capability-unavailable'
            hop_slots = 0
            responding_hops = 0
            public_responding_hops = 0
        }
        $observed = [ordered]@{
            native_hop_slots = 0
            native_responding_hops = 0
            native_public_responding_hops = 0
            direct_hop_slots = 0
            direct_responding_hops = 0
            direct_public_responding_hops = 0
        }
        $evidence = [ordered]@{
            schema_version = $CapabilitySchema
            evidence_path = $evidenceFull
            raw_output_directory = $rawDirectory
            family = $familyName
            target = $Target
            max_hops = $MaxHops
            decision = 'ipv6-unavailable'
            live_network_not_proven = $true
            reason = $reason
            nexttrace_path = $nextTraceFull
            nexttrace_expected_sha256 = $expectedHash
            nexttrace_sha256 = $actualHash
            native_tracert = $nativeEvidence
            direct_nexttrace = $directEvidence
            observed = $observed
        }
    } else {
        $nativeRun = Invoke-EcsCapturedProcess -ExecutablePath $tracertFull -Arguments $nativeArguments -WorkingDirectory $rawDirectory -StdoutPath $nativeStdoutPath -StderrPath $nativeStderrPath -Description 'native tracert probe'
        $directRun = Invoke-EcsCapturedProcess -ExecutablePath $nextTraceFull -Arguments $directArguments -WorkingDirectory $rawDirectory -StdoutPath $directStdoutPath -StderrPath $directStderrPath -Description 'direct staged NextTrace probe'
        if ($nativeRun.ExitCode -ne 0) { Stop-EcsWindowsNextTraceCapability "native tracert exited with code $($nativeRun.ExitCode)" }
        if ($directRun.ExitCode -ne 0) { Stop-EcsWindowsNextTraceCapability "direct staged NextTrace exited with code $($directRun.ExitCode)" }
        if (-not [string]::IsNullOrWhiteSpace($nativeRun.Stderr)) {
            Stop-EcsWindowsNextTraceCapability 'native tracert wrote unexpected stderr despite a successful exit'
        }
        if (-not [string]::IsNullOrWhiteSpace($directRun.Stderr)) {
            Stop-EcsWindowsNextTraceCapability 'direct staged NextTrace wrote unexpected stderr despite a successful exit'
        }
        $nativeFacts = Parse-EcsNativeTracertOutput -Output $nativeRun.Stdout -FamilyName $familyName -HopLimit $MaxHops -Target $Target
        $directFacts = Parse-EcsNextTraceOutput -Output $directRun.Stdout -FamilyName $familyName -HopLimit $MaxHops
        $nativeEvidence = [ordered]@{
            executable_path = $tracertFull
            arguments = @($nativeArguments)
            raw_stdout_path = $nativeStdoutPath
            raw_stderr_path = $nativeStderrPath
            exit_code = [int]$nativeRun.ExitCode
            execution_status = 'completed'
            parse_status = 'parsed'
            hop_slots = $nativeFacts.HopSlots
            responding_hops = $nativeFacts.RespondingHops
            public_responding_hops = $nativeFacts.PublicRespondingHops
        }
        $directEvidence = [ordered]@{
            executable_path = $nextTraceFull
            arguments = @($directArguments)
            raw_stdout_path = $directStdoutPath
            raw_stderr_path = $directStderrPath
            exit_code = [int]$directRun.ExitCode
            execution_status = 'completed'
            parse_status = 'parsed'
            hop_slots = $directFacts.HopSlots
            responding_hops = $directFacts.RespondingHops
            public_responding_hops = $directFacts.PublicRespondingHops
        }
        $observed = [ordered]@{
            native_hop_slots = $nativeFacts.HopSlots
            native_responding_hops = $nativeFacts.RespondingHops
            native_public_responding_hops = $nativeFacts.PublicRespondingHops
            direct_hop_slots = $directFacts.HopSlots
            direct_responding_hops = $directFacts.RespondingHops
            direct_public_responding_hops = $directFacts.PublicRespondingHops
        }
        $nativeClassifierFacts = [pscustomobject]@{
            ExecutionStatus = $nativeEvidence.execution_status
            ParseStatus = $nativeEvidence.parse_status
            ExitCode = $nativeEvidence.exit_code
            HopSlots = $nativeEvidence.hop_slots
            RespondingHops = $nativeEvidence.responding_hops
            PublicRespondingHops = $nativeEvidence.public_responding_hops
        }
        $directClassifierFacts = [pscustomobject]@{
            ExecutionStatus = $directEvidence.execution_status
            ParseStatus = $directEvidence.parse_status
            ExitCode = $directEvidence.exit_code
            HopSlots = $directEvidence.hop_slots
            RespondingHops = $directEvidence.responding_hops
            PublicRespondingHops = $directEvidence.public_responding_hops
        }
        $classified = Get-EcsCapabilityDecision -NativeFacts $nativeClassifierFacts -DirectFacts $directClassifierFacts
        $decision = $classified.Decision
        $liveNetworkNotProven = [bool]$classified.LiveNetworkNotProven
        $reason = $classified.Reason
        $evidence = [ordered]@{
            schema_version = $CapabilitySchema
            evidence_path = $evidenceFull
            raw_output_directory = $rawDirectory
            family = $familyName
            target = $Target
            max_hops = $MaxHops
            decision = $decision
            live_network_not_proven = $liveNetworkNotProven
            reason = $reason
            nexttrace_path = $nextTraceFull
            nexttrace_expected_sha256 = $expectedHash
            nexttrace_sha256 = $actualHash
            native_tracert = $nativeEvidence
            direct_nexttrace = $directEvidence
            observed = $observed
        }
    }

    Assert-EcsCapabilityEvidence -Evidence $evidence -ExpectedEvidencePath $evidenceFull -ExpectedRawDirectory $rawDirectory -ExpectedNextTracePath $nextTraceFull
    $json = $evidence | ConvertTo-Json -Depth 10
    try {
        $serializedEvidence = ConvertFrom-Json -InputObject $json -ErrorAction Stop
    } catch {
        Stop-EcsWindowsNextTraceCapability "serialized capability evidence is invalid JSON: $($_.Exception.Message)"
    }
    Assert-EcsCapabilityEvidence -Evidence $serializedEvidence -ExpectedEvidencePath $evidenceFull -ExpectedRawDirectory $rawDirectory -ExpectedNextTracePath $nextTraceFull
    $temporaryEvidencePath = Join-Path $evidenceDirectory (".{0}.tmp-{1}" -f [IO.Path]::GetFileName($evidenceFull), [guid]::NewGuid().ToString('N'))
    Write-EcsUtf8Text -Path $temporaryEvidencePath -Content $json
    try {
        Move-Item -LiteralPath $temporaryEvidencePath -Destination $evidenceFull -ErrorAction Stop
    } catch {
        Stop-EcsWindowsNextTraceCapability "cannot publish complete evidence file '$evidenceFull': $($_.Exception.Message)"
    }
    Write-Output ("NextTrace capability evidence written: schema={0}; family={1}; target={2}; decision={3}; evidence={4}" -f
        $CapabilitySchema, $familyName, $Target, $evidence.decision, $evidenceFull)
} catch {
    Write-Error $_.Exception.Message
    exit 1
}
}

if (-not $DotSourced) {
    Invoke-EcsWindowsNextTraceCapability
}
