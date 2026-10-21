param([Parameter(Mandatory = $true)][string]$Source)
$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskSource = (Resolve-Path -LiteralPath $Source).Path
if (-not (Test-Path -LiteralPath (Join-Path $taskSource 'c\licentivo.h')) -or -not (Test-Path -LiteralPath (Join-Path $taskSource 'go\go.mod.template'))) { throw 'Source must be the Licentivo SDK directory, not the server root.' }
$taskDestination = [IO.Path]::GetFullPath((Join-Path $taskRoot 'examples\vendor\sdk'))
if (-not $taskDestination.StartsWith($taskRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'SDK destination escaped the project.' }
if (Test-Path -LiteralPath $taskDestination) { throw 'SDK destination already exists. Use the existing prepared SDK, or review it before replacing it.' }
New-Item -ItemType Directory -Path $taskDestination -Force | Out-Null
foreach ($taskEntry in @('c','cpp','csharp','python','go','CMakeLists.txt','LicentivoConfig.cmake.in','LICENSE','NOTICE','VERSION')) {
    $taskItem = Join-Path $taskSource $taskEntry
    if (-not (Test-Path -LiteralPath $taskItem)) { throw ('Missing SDK component: ' + $taskEntry) }
    Copy-Item -LiteralPath $taskItem -Destination $taskDestination -Recurse
}
Copy-Item -LiteralPath (Join-Path $taskDestination 'go\go.mod.template') -Destination (Join-Path $taskDestination 'go\go.mod')
Write-Output ('Prepared local SDK ' + (Get-Content -LiteralPath (Join-Path $taskDestination 'VERSION') -Raw).Trim())
