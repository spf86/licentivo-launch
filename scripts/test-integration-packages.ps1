[CmdletBinding()]
param(
    [ValidateSet('go','python','csharp','java','javascript','rust','ruby','c','cpp')]
    [string[]]$Languages = @('go','python','csharp','java','javascript','rust','ruby','c','cpp'),
    [string]$Python = 'python',
    [string]$DotNet = 'dotnet',
    [string]$Node = 'node',
    [string]$Ruby = 'ruby',
    [string]$Cargo = 'cargo',
    [string]$JavaHome,
    [string]$CMake = 'cmake',
    [string]$NativeBuildDirectory,
    [string[]]$NativeRuntimeDirectories = @(),
    [string]$Output
)
$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskOutput = if ($Output) { [IO.Path]::GetFullPath($Output) } else { Join-Path $taskRoot ('artifacts/integration-checks/' + [guid]::NewGuid().ToString('N')) }
if (-not $taskOutput.StartsWith($taskRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Evidence output must be inside this workspace.' }
$taskAncestor = $taskOutput
while ($taskAncestor -and $taskAncestor -ne $taskRoot) {
    if ((Test-Path -LiteralPath $taskAncestor) -and ((Get-Item -LiteralPath $taskAncestor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Evidence output cannot use a reparse-point directory.' }
    $taskAncestor = Split-Path -Parent $taskAncestor
}
if (Test-Path -LiteralPath $taskOutput) { throw 'Choose a new evidence directory; existing evidence is never overwritten.' }
New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
$taskLanguages = @($Languages | Select-Object -Unique)
$taskResults = [Collections.Generic.List[object]]::new()
$taskNativeOutcome = $null
$taskVersion = (Get-Content -LiteralPath (Join-Path $taskRoot 'sdk/UPSTREAM_VERSION') -Raw).Trim()
$taskStarted = [DateTimeOffset]::UtcNow.ToString('o')
function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Command failed ($LASTEXITCODE): $Program" }
}
function Run-Language([string]$Language, [scriptblock]$Action) {
    $taskLog = Join-Path $taskOutput ($Language + '.log')
    $taskResult = 'passed'
    try { & $Action *>&1 | Tee-Object -FilePath $taskLog | Out-Host }
    catch { $taskResult = 'failed'; $_.Exception.Message | Add-Content -LiteralPath $taskLog; Write-Warning "$Language failed; inspect $taskLog" }
    $taskResults.Add([ordered]@{language=$Language;result=$taskResult;log=[IO.Path]::GetFileName($taskLog)})
}
Push-Location -LiteralPath $taskRoot
try {
    foreach ($taskLanguage in $taskLanguages) {
        switch ($taskLanguage) {
            'go' { Run-Language 'go' {
                Invoke-Checked 'go' @('version')
                Invoke-Checked 'go' @('test','./pkg/launchclient','-count=1')
                Push-Location examples/go
                try { Invoke-Checked 'go' @('test','./...','-count=1') } finally { Pop-Location }
                Push-Location examples/web-backend
                try { Invoke-Checked 'go' @('test','./...','-count=1') } finally { Pop-Location }
            } }
            'python' { Run-Language 'python' {
                Invoke-Checked $Python @('--version')
                $taskPreviousPythonPath = $env:PYTHONPATH
                try {
                    $env:PYTHONPATH = Join-Path $taskRoot 'sdk/python/src'
                    Invoke-Checked $Python @('-m','unittest','discover','-s','sdk/python/tests','-v')
                    Invoke-Checked $Python @('scripts/test_acceptance.py')
                } finally { $env:PYTHONPATH = $taskPreviousPythonPath }
            } }
            'csharp' { Run-Language 'csharp' {
                Invoke-Checked $DotNet @('--version')
                Invoke-Checked $DotNet @('build','sdk/csharp-tests/Launch.Tests.csproj','-m:1','-p:UseSharedCompilation=false')
                Invoke-Checked $DotNet @('sdk/csharp-tests/bin/Debug/net8.0/Launch.Tests.dll')
                Invoke-Checked $DotNet @('build','sdk/csharp-runtime-tests/Launch.RuntimeTests.csproj','-m:1','-p:UseSharedCompilation=false')
                Invoke-Checked $DotNet @('sdk/csharp-runtime-tests/bin/Debug/net8.0/Launch.RuntimeTests.dll')
            } }
            'javascript' { Run-Language 'javascript' {
                Invoke-Checked $Node @('--version')
                $taskTests = @(Get-ChildItem -LiteralPath 'sdk/javascript/test' -Filter '*.test.mjs' | ForEach-Object { $_.FullName })
                if (-not $taskTests.Count) { throw 'JavaScript tests missing.' }
                Invoke-Checked $Node (@('--test') + $taskTests)
            } }
            'java' { Run-Language 'java' {
                $taskJavac = if ($JavaHome) { Join-Path $JavaHome 'bin/javac' } else { 'javac' }
                $taskJava = if ($JavaHome) { Join-Path $JavaHome 'bin/java' } else { 'java' }
                Invoke-Checked $taskJava @('-version')
                $taskClasses = Join-Path $taskOutput 'java-classes'
                New-Item -ItemType Directory -Path $taskClasses -Force | Out-Null
                $taskSources = @('sdk/java/src','sdk/java/tests') | ForEach-Object { Get-ChildItem -LiteralPath $_ -Recurse -Filter '*.java' | ForEach-Object { $_.FullName } }
                Invoke-Checked $taskJavac (@('-encoding','UTF-8','--release','17','-d',$taskClasses) + $taskSources)
                Invoke-Checked $taskJava @('-ea','-cp',$taskClasses,'LaunchTests')
                Invoke-Checked $taskJava @('-ea','-cp',$taskClasses,'OfficialSDKTests')
            } }
            'rust' { Run-Language 'rust' {
                Invoke-Checked $Cargo @('--version')
                Invoke-Checked $Cargo @('test','--manifest-path','sdk/rust/Cargo.toml','--locked')
                Invoke-Checked $Cargo @('check','--manifest-path','examples/rust/Cargo.toml','--locked')
            } }
            'ruby' { Run-Language 'ruby' {
                Invoke-Checked $Ruby @('--version')
                Invoke-Checked $Ruby @('sdk/ruby/test/test_client.rb')
                Invoke-Checked $Ruby @('sdk/ruby/test/test_runtime.rb')
            } }
            { $_ -in @('c','cpp') } {
              if ($taskNativeOutcome) {
                $taskResults.Add([ordered]@{language=$taskLanguage;result=$taskNativeOutcome.result;log=$taskNativeOutcome.log;suite='shared_native_c_and_cpp'})
              } else { Run-Language $taskLanguage {
                if (-not $NativeBuildDirectory) { throw 'Set -NativeBuildDirectory to a configured native build with tests enabled. See sdk/native/README.md.' }
                $taskPreviousNativePath = $env:PATH
                try {
                if ($NativeRuntimeDirectories.Count) { $env:PATH = ($NativeRuntimeDirectories -join [IO.Path]::PathSeparator) + [IO.Path]::PathSeparator + $env:PATH }
                Invoke-Checked $CMake @('--version')
                Invoke-Checked $CMake @('--build',$NativeBuildDirectory,'--config','Release')
                $taskNativeBinaryRoot = Join-Path $NativeBuildDirectory 'launch-account'
                if (Test-Path -LiteralPath (Join-Path $taskNativeBinaryRoot 'Release')) { $taskNativeBinaryRoot = Join-Path $taskNativeBinaryRoot 'Release' }
                $taskExeSuffix = if ([Environment]::OSVersion.Platform -eq 'Win32NT') { '.exe' } else { '' }
                Invoke-Checked $Python @('sdk/native/tests/run.py','--c',(Join-Path $taskNativeBinaryRoot ('launch_native_test'+$taskExeSuffix)),'--cpp',(Join-Path $taskNativeBinaryRoot ('launch_cpp_test'+$taskExeSuffix)))
                } finally { $env:PATH = $taskPreviousNativePath }
              }
                $taskNativeOutcome = $taskResults[$taskResults.Count-1]
              }
            }
        }
    }
} finally { Pop-Location }
$taskPassed = @($taskResults | Where-Object { $_.result -eq 'passed' }).Count
$taskReport = [ordered]@{schema_version=1;scope='local_automated_checks';client_version=(Get-Content -LiteralPath (Join-Path $taskRoot 'sdk/VERSION') -Raw).Trim();sdk_version=$taskVersion;started_at=$taskStarted;completed_at=[DateTimeOffset]::UtcNow.ToString('o');requested_languages=$taskLanguages;results=$taskResults.ToArray();all_nine_passed=($taskPassed -eq 9 -and $taskResults.Count -eq 9);external_acceptance_complete=$false}
$taskReport | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $taskOutput 'report.json') -Encoding utf8
Write-Output ("Local language checks: $taskPassed/$($taskResults.Count). Evidence: $taskOutput")
if ($taskPassed -ne $taskResults.Count) { exit 1 }
