param([switch]$NoBuild)

$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskConfig = $env:LAUNCH_CONFIG_FILE
if (-not $taskConfig) {
    $taskConfig = Join-Path $taskRoot 'config.yaml'
}
$taskBinary = Join-Path $taskRoot 'bin\launch.exe'

if (-not (Test-Path -LiteralPath $taskConfig -PathType Leaf)) {
    throw 'Missing private configuration. Run launch init and edit config.yaml.'
}

Push-Location -LiteralPath $taskRoot
try {
    if (-not $NoBuild) {
        $taskPreviousGoCache = $env:GOCACHE
        try {
            $env:GOCACHE = Join-Path $taskRoot '.cache\go-build'
            New-Item -ItemType Directory -Path $env:GOCACHE -Force | Out-Null
            go build -o $taskBinary ./cmd/launch
            if ($LASTEXITCODE -ne 0) { throw 'Local development build failed.' }
        } finally {
            $env:GOCACHE = $taskPreviousGoCache
        }
    }
    if (-not (Test-Path -LiteralPath $taskBinary -PathType Leaf)) {
        throw 'The application binary is missing; run without -NoBuild once.'
    }
    & $taskBinary dev --config $taskConfig
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
