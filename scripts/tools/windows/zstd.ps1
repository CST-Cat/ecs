Set-StrictMode -Version Latest

function Build-EcsWindowsZstd {
    param([Parameter(Mandatory)][pscustomobject]$Context)

    $tool = $Context.Tools['zstd']
    $source = ConvertTo-EcsMsysPath $Context.Sources['zstd']
    $output = ConvertTo-EcsMsysPath $Context.Binaries['zstd']
    $flags = @($Context.Toolchain.build_flags.c) + @('-DZSTD_NODICT', '-DZSTD_NOTRACE', 'HAVE_ZLIB=0', 'HAVE_LZMA=0', 'HAVE_LZ4=0', 'ZSTD_LEGACY_SUPPORT=0')
    $moreFlags = @('-DZSTD_NODICT', '-DZSTD_NOTRACE')
    $script = @"
$($Context.Preamble)
set -eu
cd $(ConvertTo-EcsBashLiteral "$source/programs")
make -j$($Context.Jobs) zstd-release \
  CC=gcc \
  MOREFLAGS=$(ConvertTo-EcsBashLiteral ($moreFlags -join ' ')) \
  HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
cp zstd.exe $(ConvertTo-EcsBashLiteral $output)
test -s $(ConvertTo-EcsBashLiteral $output)
"@
    Invoke-EcsWindowsBash -Context $Context -Script $script
    $Context.BuildFacts['zstd'] = [ordered]@{
        build_flags = @($flags)
        source_commit = [string]$tool.commit
        license = 'BSD-3-Clause OR GPL-2.0-only'
    }
}
