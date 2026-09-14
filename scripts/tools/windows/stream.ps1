Set-StrictMode -Version Latest

function Build-EcsWindowsStream {
    param([Parameter(Mandatory)][pscustomobject]$Context)

    $tool = $Context.Tools['stream']
    $source = ConvertTo-EcsMsysPath $Context.Sources['stream']
    $output = ConvertTo-EcsMsysPath $Context.Binaries['stream']
    $revisionLine = @(Get-Content -LiteralPath $Context.Sources['stream'] | Where-Object { $_ -match '^/\* Revision: \$Id: stream\.c,v [^ ]+ [0-9/]{10}.*\*/$' } | Select-Object -First 1)
    if ($revisionLine.Count -ne 1 -or $revisionLine[0] -notmatch '^/\* Revision: \$Id: stream\.c,v (?<version>[^ ]+) (?<date>[0-9/]{10}).*\*/$') {
        throw 'STREAM source has no canonical RCS revision header'
    }
    $streamVersion = $Matches['version']
    $streamRevision = "$($Matches['version'])-$($Matches['date'])"
    $flags = @($Context.Toolchain.build_flags.c) + @('-fopenmp', "-DSTREAM_ARRAY_SIZE=$($Context.Stream.array_size)", "-DNTIMES=$($Context.Stream.ntimes)")
    $flagString = $flags -join ' '
    $script = @"
$($Context.Preamble)
set -eu
gcc $flagString $(ConvertTo-EcsBashLiteral $source) -o $(ConvertTo-EcsBashLiteral $output)
test -s $(ConvertTo-EcsBashLiteral $output)
"@
    Invoke-EcsWindowsBash -Context $Context -Script $script
    $Context.BuildFacts['stream'] = [ordered]@{
        build_flags = @($flags)
        source_sha256 = [string]$tool.source_sha256
        stream_version = $streamVersion
        stream_revision = $streamRevision
        array_size = [int]$Context.Stream.array_size
        ntimes = [int]$Context.Stream.ntimes
        license = 'STREAM-custom'
    }
}
