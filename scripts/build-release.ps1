#Requires -Version 7.4
param(
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.2.0-beta.1',
    [string]$OutputDirectory = ''
)

$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Push-Location -LiteralPath $taskRoot
$taskPreviousEnvironment = @{GOOS=$env:GOOS;GOARCH=$env:GOARCH;CGO_ENABLED=$env:CGO_ENABLED;GOCACHE=$env:GOCACHE}
try {
    $taskStatus = git status --porcelain
    if ($LASTEXITCODE -ne 0 -or $taskStatus) { throw 'Release builds require a clean, committed working tree.' }
    $taskCommit = (git rev-parse HEAD).Trim()
    if (-not $OutputDirectory) { $OutputDirectory = Join-Path $taskRoot ('.cache/releases/' + $Version) }
    $taskOutput = [IO.Path]::GetFullPath($OutputDirectory)
    if (Test-Path -LiteralPath $taskOutput) { throw 'Choose a new output directory; release artifacts are never overwritten.' }
    New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
    $taskStageRoot = Join-Path $taskOutput '_staging'
    New-Item -ItemType Directory -Path $taskStageRoot -Force | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOCACHE = Join-Path $taskRoot '.cache/go-build'
    $taskChecksums = [Collections.Generic.List[string]]::new()
    Add-Type -AssemblyName System.Formats.Tar
    foreach ($taskOS in @('windows','linux')) {
        foreach ($taskArch in @('amd64','arm64')) {
            $taskName = "licentivo-launch_${Version}_${taskOS}_${taskArch}"
            $taskStage = Join-Path $taskStageRoot $taskName
            New-Item -ItemType Directory -Path $taskStage -Force | Out-Null
            $taskExecutable = if ($taskOS -eq 'windows') { 'launch.exe' } else { 'launch' }
            $env:GOOS = $taskOS
            $env:GOARCH = $taskArch
            Write-Output "Building $taskOS/$taskArch at $taskCommit"
            go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $taskStage $taskExecutable) ./cmd/launch
            if ($LASTEXITCODE -ne 0) { throw "Build failed: $taskOS/$taskArch" }
            Copy-Item -LiteralPath (Join-Path $taskRoot 'docs/binary-installation.md') -Destination (Join-Path $taskStage 'README.md')
            Copy-Item -LiteralPath (Join-Path $taskRoot 'docs/binary-installation.md') -Destination (Join-Path $taskStage 'INSTALL.md')
            Copy-Item -LiteralPath (Join-Path $taskRoot 'LICENSE') -Destination (Join-Path $taskStage 'LICENSE')
            Copy-Item -LiteralPath (Join-Path $taskRoot 'third_party') -Destination (Join-Path $taskStage 'third_party') -Recurse
            New-Item -ItemType Directory -Path (Join-Path $taskStage 'config') -Force | Out-Null
            Copy-Item -LiteralPath (Join-Path $taskRoot 'config/launch-config.example.json') -Destination (Join-Path $taskStage 'config/launch-config.example.json')
            [IO.File]::WriteAllText((Join-Path $taskStage 'VERSION.txt'), "$Version`n", [Text.UTF8Encoding]::new($false))
            [IO.File]::WriteAllText((Join-Path $taskStage 'BUILD-INFO.txt'), "Version: $Version`nSource commit: $taskCommit`nOS: $taskOS`nArchitecture: $taskArch`nCGO_ENABLED: 0`n", [Text.UTF8Encoding]::new($false))
            if ($taskOS -eq 'windows') {
                $taskArchive = Join-Path $taskOutput ($taskName + '.zip')
                Compress-Archive -LiteralPath $taskStage -DestinationPath $taskArchive -CompressionLevel Optimal
            } else {
                $taskArchive = Join-Path $taskOutput ($taskName + '.tar.gz')
                $taskStream = [IO.File]::Create($taskArchive)
                try {
                    $taskGzip = [IO.Compression.GZipStream]::new($taskStream, [IO.Compression.CompressionLevel]::Optimal, $true)
                    try {
                        $taskTar = [System.Formats.Tar.TarWriter]::new($taskGzip, [System.Formats.Tar.TarEntryFormat]::Pax, $true)
                        try {
                            $taskDirectoryEntry = [System.Formats.Tar.PaxTarEntry]::new([System.Formats.Tar.TarEntryType]::Directory, $taskName + '/')
                            $taskDirectoryEntry.Mode = [IO.UnixFileMode]493
                            $taskTar.WriteEntry($taskDirectoryEntry)
                            foreach ($taskItem in (Get-ChildItem -LiteralPath $taskStage -Recurse | Sort-Object FullName)) {
                                $taskRelative = $taskItem.FullName.Substring($taskStage.Length).TrimStart([char[]]@('\','/')).Replace('\','/')
                                $taskEntryName = $taskName + '/' + $taskRelative
                                if ($taskItem.PSIsContainer) {
                                    $taskEntry = [System.Formats.Tar.PaxTarEntry]::new([System.Formats.Tar.TarEntryType]::Directory, $taskEntryName + '/')
                                    $taskEntry.Mode = [IO.UnixFileMode]493
                                    $taskTar.WriteEntry($taskEntry)
                                } else {
                                    $taskEntry = [System.Formats.Tar.PaxTarEntry]::new([System.Formats.Tar.TarEntryType]::RegularFile, $taskEntryName)
                                    $taskEntry.Mode = [IO.UnixFileMode]$(if ($taskRelative -eq 'launch') { 493 } else { 420 })
                                    $taskInput = [IO.File]::OpenRead($taskItem.FullName)
                                    try { $taskEntry.DataStream = $taskInput; $taskTar.WriteEntry($taskEntry) }
                                    finally { $taskInput.Dispose() }
                                }
                            }
                        } finally { $taskTar.Dispose() }
                    } finally { $taskGzip.Dispose() }
                } finally { $taskStream.Dispose() }
            }
            $taskHash = (Get-FileHash -LiteralPath $taskArchive -Algorithm SHA256).Hash.ToLowerInvariant()
            $taskChecksums.Add($taskHash + '  ' + [IO.Path]::GetFileName($taskArchive))
            Write-Output ('Packaged ' + [IO.Path]::GetFileName($taskArchive))
        }
    }
    [IO.File]::WriteAllText((Join-Path $taskOutput 'SHA256SUMS.txt'), ($taskChecksums -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Release artifacts: $taskOutput"
} finally {
    foreach ($taskKey in $taskPreviousEnvironment.Keys) { [Environment]::SetEnvironmentVariable($taskKey, $taskPreviousEnvironment[$taskKey], 'Process') }
    Pop-Location
}
