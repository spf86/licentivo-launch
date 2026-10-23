[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Source,
    [string]$Destination,
    [switch]$Replace,
    [switch]$VerifyOnly,
    [switch]$RequireManifest
)
$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskSource = (Resolve-Path -LiteralPath $Source).Path
$taskExpected = (Get-Content -LiteralPath (Join-Path $taskRoot 'sdk/UPSTREAM_VERSION') -Raw).Trim()
$taskDestination = if ($Destination) { [IO.Path]::GetFullPath($Destination) } else { Join-Path $taskRoot 'examples/vendor/sdk' }
function Assert-InWorkspace([string]$Path) {
    $resolved = [IO.Path]::GetFullPath($Path)
    if (-not $resolved.StartsWith($taskRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Destination escaped the project workspace.' }
    $cursor = $resolved
    while ($cursor -and $cursor -ne $taskRoot) {
        if ((Test-Path -LiteralPath $cursor) -and ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Reparse-point destination is not supported.' }
        $cursor = Split-Path -Parent $cursor
    }
}
Assert-InWorkspace $taskDestination
$taskEntries = @('c','cpp','csharp','python','go','java','javascript','rust','ruby','CMakeLists.txt','LicentivoConfig.cmake.in','LICENSE','NOTICE','VERSION')
$taskRequired = @(Get-Content -LiteralPath (Join-Path $taskRoot ('sdk/upstream-files-' + $taskExpected + '.txt')) | Where-Object { $_.Trim() -ne '' })
foreach ($taskEntry in ($taskEntries + $taskRequired)) {
    if (-not (Test-Path -LiteralPath (Join-Path $taskSource $taskEntry))) { throw ('Missing SDK component: ' + $taskEntry) }
}
$taskVersion = (Get-Content -LiteralPath (Join-Path $taskSource 'VERSION') -Raw).Trim()
if ($taskVersion -ne $taskExpected) { throw "Expected compatible SDK $taskExpected; found $taskVersion. Update and test the integration version lock before upgrading." }
$taskFiles = @()
foreach ($taskEntry in $taskEntries) {
    $taskItem = Get-Item -LiteralPath (Join-Path $taskSource $taskEntry) -Force
    if ($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'SDK source must not contain links.' }
    $taskCandidates = if ($taskItem.PSIsContainer) { Get-ChildItem -LiteralPath $taskItem.FullName -Recurse -Force } else { @($taskItem) }
    foreach ($taskFile in $taskCandidates) {
        if ($taskFile.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'SDK source must not contain links.' }
        if ($taskFile.PSIsContainer) { continue }
        $taskRelative = $taskFile.FullName.Substring($taskSource.TrimEnd('\','/').Length + 1).Replace('\','/')
        if ($taskRelative -match '(^|/)(bin|obj|build|dist|target|node_modules|__pycache__|\.git|[^/]+\.egg-info)(/|$)' -or $taskRelative -match '\.(pyc|pyo|class|jar|gem|dll|so|o|a)$' -or $taskRelative -eq 'go/go.mod') { continue }
        $taskFiles += [pscustomobject]@{ relative_path=$taskRelative; full_path=$taskFile.FullName; sha256=(Get-FileHash -LiteralPath $taskFile.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
    }
}
if ($VerifyOnly) {
    $taskManifestPath = Join-Path $taskSource 'launch-sdk-manifest.json'
    if (Test-Path -LiteralPath $taskManifestPath) {
        $taskManifest = Get-Content -LiteralPath $taskManifestPath -Raw | ConvertFrom-Json
        if ($taskManifest.sdk_version -ne $taskExpected -or $taskManifest.schema_version -ne 1) { throw 'SDK manifest version mismatch.' }
        $taskExpectedPaths = @($taskFiles.relative_path) + @('go/go.mod')
        $taskRecordedPaths = @($taskManifest.files | ForEach-Object { $_.path })
        if (($taskRecordedPaths | Select-Object -Unique).Count -ne $taskRecordedPaths.Count -or @($taskExpectedPaths).Count -ne $taskRecordedPaths.Count -or (Compare-Object ($taskExpectedPaths | Sort-Object) ($taskRecordedPaths | Sort-Object))) { throw 'SDK manifest file set does not match the prepared source.' }
        foreach ($taskRecorded in $taskManifest.files) {
            $taskCheck = [IO.Path]::GetFullPath((Join-Path $taskSource $taskRecorded.path))
            if (-not $taskCheck.StartsWith($taskSource + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Invalid SDK manifest path.' }
            if (-not (Test-Path -LiteralPath $taskCheck) -or (Get-FileHash -LiteralPath $taskCheck -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskRecorded.sha256) { throw ('SDK manifest hash mismatch: ' + $taskRecorded.path) }
        }
        Write-Output "Verified compatible SDK $taskVersion and complete SHA-256 manifest ($($taskFiles.Count) source files)."
    } elseif ($RequireManifest) { throw 'Prepared SDK manifest required. Prepare a new snapshot from your reviewed SDK source first.' }
    else { Write-Output "SDK $taskVersion passed version/component preflight only; legacy directory has no hash manifest. Prepare a snapshot before packaging." }
    return
}
if ($taskSource -eq $taskDestination -or $taskSource.StartsWith($taskDestination + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Use a separate upstream SDK source directory; verification uses -VerifyOnly.' }
if ((Test-Path -LiteralPath $taskDestination) -and -not $Replace) { throw 'SDK destination exists. Use -VerifyOnly to inspect it, or -Replace to stage a replacement and retain a backup.' }
$taskParent = Split-Path -Parent $taskDestination
New-Item -ItemType Directory -Path $taskParent -Force | Out-Null
$taskStage = Join-Path $taskParent ('.sdk-stage-' + [guid]::NewGuid().ToString('N'))
$taskBackup = $taskDestination + '.backup-' + [guid]::NewGuid().ToString('N')
Assert-InWorkspace $taskStage
Assert-InWorkspace $taskBackup
try {
    New-Item -ItemType Directory -Path $taskStage | Out-Null
    $taskManifestFiles = @()
    foreach ($taskFile in $taskFiles) {
        $taskTarget = Join-Path $taskStage $taskFile.relative_path
        New-Item -ItemType Directory -Path (Split-Path -Parent $taskTarget) -Force | Out-Null
        Copy-Item -LiteralPath $taskFile.full_path -Destination $taskTarget
        $taskHash = (Get-FileHash -LiteralPath $taskTarget -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($taskHash -ne $taskFile.sha256) { throw ('Source changed while preparing: ' + $taskFile.relative_path) }
        $taskManifestFiles += [ordered]@{ path=$taskFile.relative_path; sha256=$taskHash }
    }
    Copy-Item -LiteralPath (Join-Path $taskStage 'go/go.mod.template') -Destination (Join-Path $taskStage 'go/go.mod')
    $taskManifestFiles += [ordered]@{path='go/go.mod'; sha256=(Get-FileHash -LiteralPath (Join-Path $taskStage 'go/go.mod') -Algorithm SHA256).Hash.ToLowerInvariant()}
    [ordered]@{schema_version=1; sdk_version=$taskVersion; files=@($taskManifestFiles | Sort-Object { $_['path'] })} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $taskStage 'launch-sdk-manifest.json') -Encoding utf8
    if (Test-Path -LiteralPath $taskDestination) { Move-Item -LiteralPath $taskDestination -Destination $taskBackup }
    try { Move-Item -LiteralPath $taskStage -Destination $taskDestination }
    catch {
        if ((Test-Path -LiteralPath $taskBackup) -and -not (Test-Path -LiteralPath $taskDestination)) { Move-Item -LiteralPath $taskBackup -Destination $taskDestination }
        throw
    }
    Write-Output "Prepared local SDK $taskVersion with SHA-256 manifest."
    if (Test-Path -LiteralPath $taskBackup) { Write-Output "Previous SDK retained at $taskBackup" }
}
finally {
    if (Test-Path -LiteralPath $taskStage) { Assert-InWorkspace $taskStage; Remove-Item -LiteralPath $taskStage -Recurse -Force }
}
