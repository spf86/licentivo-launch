# Copy only the public, explicitly listed multilingual guides into release staging.
param(
    [Parameter(Mandatory)][string]$SourceRoot,
    [Parameter(Mandatory)][string]$DestinationRoot
)
$ErrorActionPreference = 'Stop'
$taskSourceRoot = [IO.Path]::GetFullPath($SourceRoot)
$taskDestinationRoot = [IO.Path]::GetFullPath($DestinationRoot)
$taskManifest = Get-Content -LiteralPath (Join-Path $taskSourceRoot 'docs/http-testing/locales.json') -Raw | ConvertFrom-Json
$taskLocales = @($taskManifest.locales)
if ($taskManifest.schema_version -ne 1 -or $taskLocales.Count -ne 14 -or @($taskLocales.code | Sort-Object -Unique).Count -ne 14) {
    throw 'HTTP testing documentation requires exactly 14 languages.'
}
$taskFiles = @($taskLocales | ForEach-Object {
    if ($_.file -cnotmatch '^http-testing(?:\.[A-Za-z-]+)?\.md$') { throw 'Invalid HTTP documentation filename.' }
    'docs/' + $_.file
}) + @('docs/http-testing/README.md','docs/http-testing/locales.json','docs/http-testing/translations.json')
foreach ($taskFile in $taskFiles) {
    $taskSource = Join-Path $taskSourceRoot $taskFile
    if (-not (Test-Path -LiteralPath $taskSource -PathType Leaf)) { throw ('Missing HTTP documentation: ' + $taskFile) }
}
foreach ($taskFile in $taskFiles) {
    $taskDestination = Join-Path $taskDestinationRoot $taskFile
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $taskDestination) | Out-Null
    Copy-Item -LiteralPath (Join-Path $taskSourceRoot $taskFile) -Destination $taskDestination
}
Write-Output ('Included HTTP testing documentation in all ' + $taskLocales.Count + ' languages.')
