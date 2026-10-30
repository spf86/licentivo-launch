param([Parameter(Mandatory=$true,Position=0)][string]$LaunchConfig,[Parameter(Mandatory=$true,Position=1)][string]$ProductConfig,[Parameter(Position=2)][string]$Action,[string]$JdkHome=$env:JAVA_HOME)
$ErrorActionPreference = 'Stop'
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$javac = if ($JdkHome) { Join-Path $JdkHome 'bin/javac.exe' } else { 'javac' }
$java = if ($JdkHome) { Join-Path $JdkHome 'bin/java.exe' } else { 'java' }
$output = Join-Path $repository '.cache/java-launch'
New-Item -ItemType Directory -Force -Path $output | Out-Null
$sources = @(Get-ChildItem -LiteralPath (Join-Path $repository 'sdk/java/src') -Recurse -Filter '*.java' | ForEach-Object FullName)
$sources += Join-Path $PSScriptRoot 'Main.java'
& $javac --release 17 -encoding UTF-8 -d $output @sources
if ($LASTEXITCODE -ne 0) { throw 'Java compilation failed; JDK 17+ and the Launch SDK sources with embedded SDK 0.11.7 are required.' }
$arguments = @('-cp',$output,'Main',$LaunchConfig,$ProductConfig)
if ($Action) { $arguments += $Action }
& $java @arguments
exit $LASTEXITCODE
