Set-StrictMode -Version Latest

function ConvertTo-EcsMsysPath {
    param([Parameter(Mandatory)][string]$Path)

    $full = [IO.Path]::GetFullPath($Path)
    if ($full -notmatch '^(?<drive>[A-Za-z]):\\(?<rest>.*)$') {
        throw "MSYS2 paths must be absolute Windows paths: $Path"
    }
    return "/$($Matches.drive.ToLowerInvariant())/$($Matches.rest -replace '\\', '/')"
}

function ConvertTo-EcsBashLiteral {
    param([Parameter(Mandatory)][AllowEmptyString()][string]$Value)

    return "'$($Value.Replace("'", "'\''"))'"
}

function Invoke-EcsWindowsBash {
    param(
        [Parameter(Mandatory)][pscustomobject]$Context,
        [Parameter(Mandatory)][string]$Script,
        [switch]$CaptureOutput
    )

    $previousMSYSTEM = $env:MSYSTEM
    $previousCHERE = $env:CHERE_INVOKING
    $previousPathType = $env:MSYS2_PATH_TYPE
    try {
        $env:MSYSTEM = 'UCRT64'
        $env:CHERE_INVOKING = '1'
        $env:MSYS2_PATH_TYPE = 'strict'
        if ($CaptureOutput) {
            $output = & $Context.Bash --noprofile --norc -c $Script 2>&1
        } else {
            & $Context.Bash --noprofile --norc -c $Script
            $output = @()
        }
        if ($LASTEXITCODE -ne 0) {
            throw "MSYS2 bash failed with exit code $LASTEXITCODE"
        }
        if ($CaptureOutput) {
            return @($output)
        }
    } finally {
        if ($null -eq $previousMSYSTEM) { Remove-Item Env:MSYSTEM -ErrorAction SilentlyContinue } else { $env:MSYSTEM = $previousMSYSTEM }
        if ($null -eq $previousCHERE) { Remove-Item Env:CHERE_INVOKING -ErrorAction SilentlyContinue } else { $env:CHERE_INVOKING = $previousCHERE }
        if ($null -eq $previousPathType) { Remove-Item Env:MSYS2_PATH_TYPE -ErrorAction SilentlyContinue } else { $env:MSYS2_PATH_TYPE = $previousPathType }
    }
}

function Invoke-EcsWindowsNative {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [Parameter(Mandatory)][string[]]$ArgumentList,
        [string]$WorkingDirectory
    )

    if ($WorkingDirectory) {
        Push-Location $WorkingDirectory
    }
    try {
        & $FilePath @ArgumentList
        if ($LASTEXITCODE -ne 0) {
            throw "$FilePath failed with exit code $LASTEXITCODE"
        }
    } finally {
        if ($WorkingDirectory) { Pop-Location }
    }
}

function Save-EcsVerifiedDownload {
    param(
        [Parameter(Mandatory)][string]$Uri,
        [Parameter(Mandatory)][string]$Sha256,
        [Parameter(Mandatory)][string]$Destination,
        [string]$Description = 'download'
    )

    $parent = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    $lastError = $null
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination
            $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash.ToLowerInvariant()
            if ($actual -eq $Sha256.ToLowerInvariant()) {
                return
            }
            $lastError = "SHA-256 mismatch: expected $Sha256, got $actual"
        } catch {
            $lastError = $_.Exception.Message
        }
        Remove-Item -Force -LiteralPath $Destination -ErrorAction SilentlyContinue
    }
    throw "$Description failed after three attempts: $lastError"
}

function Get-EcsLockedTool {
    param(
        [Parameter(Mandatory)][pscustomobject]$Lock,
        [Parameter(Mandatory)][string]$Name
    )

    $tool = @($Lock.tools | Where-Object { $_.name -eq $Name })
    if ($tool.Count -ne 1) {
        throw "tools lock must contain exactly one $Name entry"
    }
    return $tool[0]
}

function Get-EcsGitSourceUrl {
    param([Parameter(Mandatory)][pscustomobject]$Tool)

    if (-not $Tool.repository -or -not $Tool.commit) {
        throw "tools lock has no canonical repository/commit for $($Tool.name)"
    }
    return "https://github.com/$($Tool.repository).git"
}

function Invoke-EcsGitCloneAtCommit {
    param(
        [Parameter(Mandatory)][pscustomobject]$Tool,
        [Parameter(Mandatory)][string]$Destination
    )

    Invoke-EcsWindowsNative -FilePath 'git.exe' -ArgumentList @(
        '-c', 'advice.detachedHead=false', 'clone', '--depth', '1', '--branch', [string]$Tool.tag,
        (Get-EcsGitSourceUrl $Tool), $Destination
    )
    $actual = (& git.exe -C $Destination rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $actual -ne [string]$Tool.commit) {
        throw "$($Tool.name) resolved to $actual, expected $($Tool.commit)"
    }
}
