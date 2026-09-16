Set-StrictMode -Version Latest

function Build-EcsWindowsNpb {
    param([Parameter(Mandatory)][pscustomobject]$Context)

    $tool = $Context.Tools['npb-ep']
    $sourceRoot = Join-Path $Context.Sources['npb'] 'NPB3.4-OMP'
    $source = ConvertTo-EcsMsysPath $sourceRoot
    $outputEP = ConvertTo-EcsMsysPath $Context.Binaries['npb-ep']
    $outputFT = ConvertTo-EcsMsysPath $Context.Binaries['npb-ft']
    $fortranFlags = [string]$Context.FortranFlags
    $fortranLinkerFlags = @($Context.FortranLinkerFlags) -join ' '
    $cLinkerFlags = @($Context.LinkerFlags) -join ' '
    $compileDate = $Context.CompileDate
    $makeDefinition = @"
FC = gfortran
FLINK = gfortran
F_LIB =
F_INC =
FFLAGS = $fortranFlags
FLINKFLAGS = $fortranLinkerFlags
CC = gcc
CLINK = gcc
C_LIB = -lm
C_INC =
CFLAGS = $($Context.CFlags)
CLINKFLAGS = $cLinkerFlags
UCC = gcc
BINDIR = ../bin
RAND = randi8
WTIME = wtime.c
"@
    Set-Content -LiteralPath (Join-Path $sourceRoot 'config\make.def') -Value $makeDefinition -Encoding ascii
    $prepareScript = @"
$($Context.Preamble)
set -eu
cd $(ConvertTo-EcsBashLiteral $source)
make -C sys all
"@
    Invoke-EcsWindowsBash -Context $Context -Script $prepareScript
    foreach ($benchmark in @('EP', 'FT')) {
        $lower = $benchmark.ToLowerInvariant()
        $params = Join-Path $sourceRoot "$benchmark\npbparams.h"
        $setparams = "cd $(ConvertTo-EcsBashLiteral "$source/$benchmark")`n../sys/setparams $lower A"
        Invoke-EcsWindowsBash -Context $Context -Script "$($Context.Preamble)`nset -eu`n$setparams"
        $contents = Get-Content -LiteralPath $params -Raw
        $contents = [regex]::Replace($contents, "parameter \(compiletime='[^']*'\)", "parameter (compiletime='$compileDate')")
        Set-Content -LiteralPath $params -Value $contents -Encoding ascii
    }

    $script = @"
$($Context.Preamble)
set -eu
cd $(ConvertTo-EcsBashLiteral $source)
make -j$($Context.Jobs) ep CLASS=A
make -j$($Context.Jobs) ft CLASS=A
copy_release() {
  local source_base=`$1 destination=`$2 candidate
  for candidate in "`$source_base.exe" "`$source_base"; do
    if [ -s "`$candidate" ]; then
      cp "`$candidate" "`$destination"
      return 0
    fi
  done
  echo "missing NPB output: `$source_base(.exe)" >&2
  return 1
}
copy_release bin/ep.A.x $(ConvertTo-EcsBashLiteral $outputEP)
copy_release bin/ft.A.x $(ConvertTo-EcsBashLiteral $outputFT)
test -s $(ConvertTo-EcsBashLiteral $outputEP)
test -s $(ConvertTo-EcsBashLiteral $outputFT)
"@
    Invoke-EcsWindowsBash -Context $Context -Script $script
    $buildFlags = @('gfortran') + @($fortranFlags -split ' ') + @('CLASS=A', 'RAND=randi8', 'OMP')
    $Context.BuildFacts['npb-ep'] = [ordered]@{
        build_flags = @($buildFlags)
        source_sha256 = [string]$tool.source_sha256
        source_tag = [string]$tool.tag
        npb_version = [string]$tool.version
        npb_tag = [string]$tool.tag
        license = 'NASA-NPB-permissive'
    }
    $Context.BuildFacts['npb-ft'] = [ordered]@{
        build_flags = @($buildFlags)
        source_sha256 = [string]$tool.source_sha256
        source_tag = [string]$tool.tag
        npb_version = [string]$tool.version
        npb_tag = [string]$tool.tag
        license = 'NASA-NPB-permissive'
    }
}
