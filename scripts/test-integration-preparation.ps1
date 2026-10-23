$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskTest = Join-Path $taskRoot ('.cache/prepare-tests-' + [guid]::NewGuid().ToString('N'))
$taskPrepare = Join-Path $PSScriptRoot 'prepare-examples.ps1'
$taskSource = Join-Path $taskTest 'source'
$taskDestination = Join-Path $taskTest 'installed'
function Expect-Failure([scriptblock]$Operation, [string]$Label) {
    $failed = $false
    try { & $Operation | Out-Null } catch { $failed = $true }
    if (-not $failed) { throw ('Expected rejection: ' + $Label) }
}
& $taskPrepare -Source (Join-Path $taskRoot 'examples/vendor/sdk') -Destination $taskSource | Out-Null
& $taskPrepare -Source $taskSource -VerifyOnly -RequireManifest | Out-Null
$taskVersion = Get-Content -LiteralPath (Join-Path $taskSource 'VERSION') -Raw
Set-Content -LiteralPath (Join-Path $taskSource 'VERSION') -Value '9.99.0' -Encoding ascii
Expect-Failure { & $taskPrepare -Source $taskSource -Destination $taskDestination } 'incompatible version'
if (Test-Path -LiteralPath $taskDestination) { throw 'Failed preflight left a destination behind' }
Set-Content -LiteralPath (Join-Path $taskSource 'VERSION') -Value $taskVersion -NoNewline -Encoding ascii
$taskRequired = Join-Path $taskSource 'csharp/DeviceCredential.cs'
Move-Item -LiteralPath $taskRequired -Destination ($taskRequired + '.saved')
Expect-Failure { & $taskPrepare -Source $taskSource -Destination $taskDestination } 'missing source component'
Move-Item -LiteralPath ($taskRequired + '.saved') -Destination $taskRequired
& $taskPrepare -Source $taskSource -Destination $taskDestination | Out-Null
Expect-Failure { & $taskPrepare -Source $taskSource -Destination $taskDestination } 'replacement requires explicit switch'
$taskTampered = Join-Path $taskDestination 'csharp/DeviceCredential.cs'
$taskOriginal = [IO.File]::ReadAllBytes($taskTampered)
Add-Content -LiteralPath $taskTampered -Value '// modified'
Expect-Failure { & $taskPrepare -Source $taskDestination -VerifyOnly -RequireManifest } 'hash mismatch'
[IO.File]::WriteAllBytes($taskTampered, $taskOriginal)
$taskExtra = Join-Path $taskDestination 'csharp/Unexpected.cs'
Set-Content -LiteralPath $taskExtra -Value '// extra source'
Expect-Failure { & $taskPrepare -Source $taskDestination -VerifyOnly -RequireManifest } 'unrecorded source file'
Remove-Item -LiteralPath $taskExtra
$taskManifestPath = Join-Path $taskDestination 'launch-sdk-manifest.json'
$taskManifestBytes = [IO.File]::ReadAllBytes($taskManifestPath)
$taskManifest = Get-Content -LiteralPath $taskManifestPath -Raw | ConvertFrom-Json
$taskManifest.files = @($taskManifest.files | Select-Object -Skip 1)
$taskManifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $taskManifestPath -Encoding utf8
Expect-Failure { & $taskPrepare -Source $taskDestination -VerifyOnly -RequireManifest } 'omitted manifest source'
[IO.File]::WriteAllBytes($taskManifestPath, $taskManifestBytes)
Set-Content -LiteralPath (Join-Path $taskDestination 'retain-me.txt') -Value 'previous SDK'
& $taskPrepare -Source $taskSource -Destination $taskDestination -Replace | Out-Null
$taskBackup = Get-ChildItem -LiteralPath $taskTest -Directory -Filter 'installed.backup-*' | Select-Object -First 1
if (-not $taskBackup -or -not (Test-Path -LiteralPath (Join-Path $taskBackup.FullName 'retain-me.txt'))) { throw 'Replacement discarded previous SDK' }
& $taskPrepare -Source $taskDestination -VerifyOnly -RequireManifest | Out-Null
Write-Output 'SDK preparation passed: version/missing files, no partial install, full manifest tamper/extra/omitted checks, explicit replacement and retained backup.'
