param([string]$OfficialRoot=(Join-Path (Split-Path -Parent $PSScriptRoot) '../Licenova'),[string]$OutputRoot='.cache/n09-probes')
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $taskRoot
$taskOutput=Join-Path $taskRoot $OutputRoot
New-Item -ItemType Directory -Path $taskOutput,(Join-Path $taskOutput 'native'),(Join-Path $taskOutput 'java'),(Join-Path $taskOutput 'csharp-src'),(Join-Path $taskRoot 'pkg/launchclient/.local/n09-proof') -Force | Out-Null
function Checked {param([string]$Exe,[string[]]$Arguments) & $Exe @Arguments; if($LASTEXITCODE -ne 0){throw "Embedded probe build failed: $Exe"}}
$taskMingw=Join-Path $OfficialRoot '.local/toolchains/mingw-modern/mingw64/bin'
$taskCurl=(Get-ChildItem -LiteralPath (Join-Path $OfficialRoot '.local/toolchains/curl') -Directory | Select-Object -First 1).FullName
$taskJava=(Get-ChildItem -LiteralPath (Join-Path $OfficialRoot '.local/toolchains/java') -Directory | Select-Object -First 1).FullName
$taskRuby=(Get-ChildItem -LiteralPath (Join-Path $OfficialRoot '.local/toolchains/ruby') -Directory | ForEach-Object {Join-Path $_.FullName 'bin/ruby.exe'} | Where-Object {Test-Path -LiteralPath $_} | Select-Object -First 1)
$taskDotnet=Join-Path $OfficialRoot '.local/toolchains/dotnet/dotnet.exe'
$env:PATH=$taskMingw+';'+(Join-Path $taskCurl 'bin')+';'+$env:PATH
$env:CC=(Join-Path $taskMingw 'gcc.exe').Replace('\','/')
$env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER=$env:CC
$env:CARGO_HOME=Join-Path $OfficialRoot '.local/toolchains/rust/cargo'
$env:RUSTUP_HOME=Join-Path $OfficialRoot '.local/toolchains/rust/rustup'
$env:LICENTIVO_CURL_ROOT=$taskCurl
$env:DOTNET_CLI_HOME=Join-Path $taskRoot '.cache/dotnet-home'
$env:NUGET_PACKAGES=Join-Path $OfficialRoot '.local/nuget'
$env:APPDATA=Join-Path $taskOutput 'user-roaming'
$env:LOCALAPPDATA=Join-Path $taskOutput 'user-local'
$env:GOCACHE=Join-Path $OfficialRoot '.local/n04-n09-go-cache'
$env:GOTMPDIR=Join-Path $taskRoot '.cache/n09-go-tmp'
New-Item -ItemType Directory -Path $env:APPDATA,$env:LOCALAPPDATA,$env:GOTMPDIR -Force | Out-Null
$taskProbe=Join-Path $OfficialRoot 'internal/httpapi/testdata'
$taskGoSource=(Get-Content -LiteralPath (Join-Path $taskProbe 'metered_renewal.go') -Raw).Replace('licentivo/sdk/go','licentivo-launch/pkg/launchclient/internal/licentivo')
$taskGoPath=Join-Path $taskRoot 'pkg/launchclient/.local/n09-proof/main.go'
$taskGoSource | Set-Content -LiteralPath $taskGoPath -Encoding utf8
Checked 'go' @('build','-o',(Join-Path $taskOutput 'go.exe'),$taskGoPath)
Checked (Join-Path $taskJava 'bin/javac.exe') (@('-encoding','UTF-8','-d',(Join-Path $taskOutput 'java'))+(Get-ChildItem -LiteralPath 'sdk/java/src' -Filter '*.java' -Recurse).FullName+@((Join-Path $taskProbe 'MeteredRenewal.java')))
$taskCsReference=(Join-Path $taskRoot 'sdk/csharp/Licentivo.Launch.csproj').Replace('\','/')
@"
<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable></PropertyGroup><ItemGroup><ProjectReference Include="$taskCsReference" /></ItemGroup></Project>
"@ | Set-Content -LiteralPath (Join-Path $taskOutput 'csharp-src/MeteredRenewal.csproj') -Encoding utf8
Copy-Item -LiteralPath (Join-Path $taskProbe 'metered-renewal-csharp/Program.cs') -Destination (Join-Path $taskOutput 'csharp-src/Program.cs')
'<configuration><packageSources><clear /></packageSources></configuration>' | Set-Content -LiteralPath (Join-Path $taskOutput 'NuGet.Config') -Encoding utf8
Checked $taskDotnet @('build',(Join-Path $taskOutput 'csharp-src/MeteredRenewal.csproj'),'--nologo',('-p:RestoreConfigFile='+(Join-Path $taskOutput 'NuGet.Config')),'-o',(Join-Path $taskOutput 'csharp'))
$taskCore=Join-Path $taskRoot 'sdk/native/vendor/sdk/c'
$taskObjects=@()
foreach($taskSource in @('licentivo','vendor/cJSON','vendor/monocypher','vendor/monocypher-ed25519')){
 $taskObject=Join-Path $taskOutput ('native/'+(Split-Path -Leaf $taskSource)+'.o')
 Checked $env:CC @('-std=c11','-O2',('-I'+$taskCore),('-I'+(Join-Path $taskCurl 'include')),'-c',(Join-Path $taskCore ($taskSource+'.c')),'-o',$taskObject)
 $taskObjects+=$taskObject
}
$taskLibraries=@((Join-Path $taskCurl 'lib/libcurl.dll.a'),'-lbcrypt','-ladvapi32')
Checked $env:CC (@('-std=c11',('-I'+$taskCore),(Join-Path $taskProbe 'metered_renewal.c'))+$taskObjects+$taskLibraries+@('-o',(Join-Path $taskOutput 'c.exe')))
Checked (Join-Path $taskMingw 'g++.exe') (@('-std=c++17',('-I'+(Join-Path $taskRoot 'sdk/native/vendor/sdk/cpp')),(Join-Path $taskProbe 'metered_renewal.cpp'))+$taskObjects+$taskLibraries+@('-o',(Join-Path $taskOutput 'cpp.exe')))
Checked (Join-Path $env:CARGO_HOME 'bin/cargo.exe') @('build','--offline','--manifest-path','sdk/rust/Cargo.toml','--example','metered_renewal_proof','--target-dir',(Join-Path $taskOutput 'rust-target'))
$taskRubyCore=Join-Path $taskRoot 'sdk/ruby/vendor/c'
$taskRubyNative=Join-Path $taskRoot 'sdk/ruby/lib/licentivo_launch/vendor/licentivo/native'
New-Item -ItemType Directory -Path $taskRubyNative -Force | Out-Null
Checked $env:CC (@('-std=c11','-O2','-shared','-Wl,--export-all-symbols',('-I'+$taskRubyCore),('-I'+(Join-Path $taskCurl 'include')))+(@('licentivo.c','vendor/cJSON.c','vendor/monocypher.c','vendor/monocypher-ed25519.c') | ForEach-Object {Join-Path $taskRubyCore $_})+$taskLibraries+@('-o',(Join-Path $taskRubyNative 'licentivo_native.dll')))
Get-ChildItem -LiteralPath (Join-Path $taskCurl 'bin') -Filter '*.dll' | ForEach-Object {Copy-Item -LiteralPath $_.FullName -Destination $taskRubyNative}
$taskJsUri=([Uri](Join-Path $taskRoot 'sdk/javascript/src/vendor/licentivo.mjs')).AbsoluteUri
(Get-Content -LiteralPath (Join-Path $taskProbe 'metered_renewal.mjs') -Raw).Replace('../../../sdk/javascript/licentivo.mjs',$taskJsUri) | Set-Content -LiteralPath (Join-Path $taskOutput 'proof.mjs') -Encoding utf8
$taskRbPath=(Join-Path $taskRoot 'sdk/ruby/lib/licentivo_launch/vendor/licentivo.rb').Replace('\','/')
(Get-Content -LiteralPath (Join-Path $taskProbe 'metered_renewal.rb') -Raw).Replace("File.expand_path('../../../sdk/ruby/lib/licentivo', __dir__)","'$taskRbPath'") | Set-Content -LiteralPath (Join-Path $taskOutput 'proof.rb') -Encoding utf8
$taskPrograms=@(
 @{name='Go';exe=(Join-Path $taskOutput 'go.exe');args=@()},
 @{name='JavaScript';exe=(Get-Command node).Source;args=@((Join-Path $taskOutput 'proof.mjs'))},
 @{name='Java';exe=(Join-Path $taskJava 'bin/java.exe');args=@('-cp',(Join-Path $taskOutput 'java'),'licentivo.MeteredRenewal')},
 @{name='CSharp';exe=$taskDotnet;args=@((Join-Path $taskOutput 'csharp/MeteredRenewal.dll'))},
 @{name='C';exe=(Join-Path $taskOutput 'c.exe');args=@()},
 @{name='Cpp';exe=(Join-Path $taskOutput 'cpp.exe');args=@()},
 @{name='Rust';exe=(Join-Path $taskOutput 'rust-target/debug/examples/metered_renewal_proof.exe');args=@()},
 @{name='Ruby';exe=$taskRuby;args=@((Join-Path $taskOutput 'proof.rb'))}
)
@{environment=@{PATH=$env:PATH;LICENTIVO_CURL_ROOT=$taskCurl};programs=$taskPrograms} | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $taskOutput 'programs.json') -Encoding utf8
Write-Output ('Built all eight embedded SDK probes: '+(Join-Path $taskOutput 'programs.json'))
