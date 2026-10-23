[CmdletBinding()]
param(
 [string]$Source,[string]$Python='python',[string]$DotNet='dotnet',[string]$Npm='npm.cmd',[string]$Ruby='ruby',[string]$JavaHome,
 [string]$Cargo='cargo',[string]$CMake='cmake',[string]$NativeBuildDir,[string]$Output,[string]$CurlRoot,[string]$CCompiler,
 [switch]$SkipPython,[switch]$SkipDotNet,[switch]$SkipJavaScript,[switch]$SkipRuby,[switch]$SkipJava,
 [switch]$SourceOnly,[switch]$BuildRustCrate,[switch]$BuildNative
)
$ErrorActionPreference='Stop'
$taskRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskSDK=if($Source){[IO.Path]::GetFullPath($Source)}else{Join-Path $taskRoot 'examples/vendor/sdk'}
& (Join-Path $PSScriptRoot 'prepare-examples.ps1') -Source $taskSDK -VerifyOnly
& node (Join-Path $PSScriptRoot 'prepare-managed-sdk.mjs') $taskSDK
if($LASTEXITCODE -ne 0){throw 'Managed SDK preparation failed.'}
$taskOutput=if($Output){[IO.Path]::GetFullPath($Output)}else{Join-Path $taskRoot 'artifacts/integration-packages-managed-0.1.2'}
if(-not $taskOutput.StartsWith($taskRoot+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Output must remain within the workspace.'}
if(Test-Path -LiteralPath $taskOutput){throw 'Choose a new package output directory; old artifacts are retained.'}
New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
$taskStage=Join-Path $taskRoot ('.cache/managed-package-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $taskStage -Force | Out-Null
$taskLanguages=[Collections.Generic.List[object]]::new()
$taskArtifacts=[Collections.Generic.List[string]]::new()
function Checked([string]$Program,[string[]]$Arguments){ & $Program @Arguments; if($LASTEXITCODE -ne 0){throw "Package command failed: $Program"} }
function Copy-Tree([string]$From,[string]$To){
 New-Item -ItemType Directory -Path $To -Force | Out-Null
 foreach($taskFile in (Get-ChildItem -LiteralPath $From -Recurse -File -Force)){
  $taskRel=$taskFile.FullName.Substring($From.Length+1).Replace('\','/')
  if($taskRel -match '(^|/)(\.git|__pycache__|node_modules|bin|obj|target|build|dist|[^/]+\.egg-info)(/|$)' -or $taskRel -match '\.(pyc|pyo|class|log|dpapi|secrets\.json)$' -or $taskFile.Name -in @('launch-config.json','product.json','licensed-report.txt')){continue}
  $taskDest=Join-Path $To $taskRel
  New-Item -ItemType Directory -Path (Split-Path $taskDest) -Force | Out-Null
  Copy-Item -LiteralPath $taskFile.FullName -Destination $taskDest
 }
}
function Curl([string]$To){
 if(-not $CurlRoot){return}
 foreach($taskPart in @('include')){Copy-Tree (Join-Path $CurlRoot $taskPart) (Join-Path $To $taskPart)}
 foreach($taskPart in @('lib','bin')){New-Item -ItemType Directory -Path (Join-Path $To $taskPart) -Force | Out-Null}
 Copy-Item -LiteralPath (Join-Path $CurlRoot 'lib/libcurl.dll.a') -Destination (Join-Path $To 'lib')
 Get-ChildItem -LiteralPath (Join-Path $CurlRoot 'bin') -Filter '*.dll' | ForEach-Object {Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $To 'bin')}
 Get-ChildItem -LiteralPath $CurlRoot -File | Where-Object Name -Match 'COPYING|LICENSE|NOTICE' | ForEach-Object {Copy-Item -LiteralPath $_.FullName -Destination $To}
 $taskDependencies=Join-Path $CurlRoot 'dep'
 if(Test-Path -LiteralPath $taskDependencies){
  Get-ChildItem -LiteralPath $taskDependencies -Recurse -File | Where-Object Name -Match 'COPYING|LICEN[CS]E|NOTICE' | ForEach-Object {
   $taskLicense=Join-Path $To ('licenses/'+$_.FullName.Substring($taskDependencies.Length+1))
   New-Item -ItemType Directory -Path (Split-Path $taskLicense) -Force | Out-Null
   Copy-Item -LiteralPath $_.FullName -Destination $taskLicense
  }
 }
}
function Zip([string]$From,[string]$Name){Compress-Archive -Path (Join-Path $From '*') -DestinationPath (Join-Path $taskOutput $Name);$taskArtifacts.Add($Name)}
function Language([string]$Name,[string]$Artifact,[string]$Archive,[string]$Kind){
 $taskLanguages.Add([ordered]@{language=$Name;version='0.1.2';upstream_version='0.11.1';artifact=$Artifact;source_archive=$Archive;artifact_type=$Kind;sdk_managed=$true;requires_separate_official_sdk=$false;initialization='direct_parameters';configuration_files_required=$false})
 if(-not $taskArtifacts.Contains($Artifact)){$taskArtifacts.Add($Artifact)}
}
foreach($taskName in @('python','csharp','javascript','ruby','java','rust','native')){Copy-Tree (Join-Path $taskRoot ('sdk/'+$taskName)) (Join-Path $taskStage $taskName)}
foreach($taskName in @('rust','ruby','native')){Curl (Join-Path $taskStage ($taskName+'/vendor/curl'))}
$taskGo=Join-Path $taskStage 'go'
Copy-Tree (Join-Path $taskRoot 'pkg/launchclient') (Join-Path $taskGo 'pkg/launchclient')
Copy-Item -LiteralPath (Join-Path $taskRoot 'LICENSE'),(Join-Path $taskRoot 'go.sum') -Destination $taskGo
@'
module github.com/spf86/licentivo-launch-sdk

go 1.26.0

require golang.org/x/sys v0.41.0
'@ | Set-Content -LiteralPath (Join-Path $taskGo 'go.mod') -Encoding utf8
foreach($taskFile in (Get-ChildItem -LiteralPath (Join-Path $taskGo 'pkg') -Recurse -Filter '*.go')){
 $taskCode=[IO.File]::ReadAllText($taskFile.FullName).Replace('"licentivo-launch/','"github.com/spf86/licentivo-launch-sdk/')
 [IO.File]::WriteAllText($taskFile.FullName,$taskCode,[Text.UTF8Encoding]::new($false))
}
Copy-Item -LiteralPath (Join-Path $taskRoot 'pkg/launchclient/README.md') -Destination (Join-Path $taskGo 'README.md')
Zip $taskGo 'licentivo-launch-go-0.1.2.zip'
Language 'Go' 'licentivo-launch-go-0.1.2.zip' 'licentivo-launch-go-0.1.2.zip' 'self_contained_go_module'
foreach($taskName in @('python','csharp','javascript','ruby','java','rust','native')){Zip (Join-Path $taskStage $taskName) ('licentivo-launch-'+$taskName+'-0.1.2-source.zip')}
if(-not $SourceOnly -and -not $SkipPython){
 Checked $Python @('-m','pip','wheel','--no-deps','--no-build-isolation','--wheel-dir',$taskOutput,(Join-Path $taskStage 'python'))
 Language 'Python' 'licentivo_launch-0.1.2-py3-none-any.whl' 'licentivo-launch-python-0.1.2-source.zip' 'self_contained_wheel'
}else{Language 'Python' 'licentivo-launch-python-0.1.2-source.zip' 'licentivo-launch-python-0.1.2-source.zip' 'self_contained_source'}
if(-not $SourceOnly -and -not $SkipDotNet){
 Checked $DotNet @('pack',(Join-Path $taskStage 'csharp/Licentivo.Launch.csproj'),'-c','Release','-o',$taskOutput,'-m:1','-p:UseSharedCompilation=false')
 Language 'C#' 'Licentivo.Launch.0.1.2.nupkg' 'licentivo-launch-csharp-0.1.2-source.zip' 'self_contained_nuget'
}else{Language 'C#' 'licentivo-launch-csharp-0.1.2-source.zip' 'licentivo-launch-csharp-0.1.2-source.zip' 'self_contained_source'}
if(-not $SourceOnly -and -not $SkipJavaScript){
 Checked $Npm @('pack',(Join-Path $taskStage 'javascript'),'--ignore-scripts','--pack-destination',$taskOutput)
 Language 'JavaScript/Node.js' 'licentivo-launch-0.1.2.tgz' 'licentivo-launch-javascript-0.1.2-source.zip' 'self_contained_npm'
}else{Language 'JavaScript/Node.js' 'licentivo-launch-javascript-0.1.2-source.zip' 'licentivo-launch-javascript-0.1.2-source.zip' 'self_contained_source'}
if(-not $SourceOnly -and -not $SkipJava){
 $taskJavac=if($JavaHome){Join-Path $JavaHome 'bin/javac.exe'}else{'javac'}
 $taskJar=if($JavaHome){Join-Path $JavaHome 'bin/jar.exe'}else{'jar'}
 $taskClasses=Join-Path $taskStage 'java-classes'
 New-Item -ItemType Directory -Path $taskClasses -Force | Out-Null
 $taskSources=@(Get-ChildItem -LiteralPath (Join-Path $taskStage 'java/src') -Recurse -Filter '*.java' | ForEach-Object FullName)
 Checked $taskJavac (@('--release','17','-encoding','UTF-8','-d',$taskClasses)+$taskSources)
 Copy-Tree (Join-Path $taskStage 'java/vendor') (Join-Path $taskClasses 'META-INF/licenses/licentivo')
 Copy-Item -LiteralPath (Join-Path $taskRoot 'LICENSE') -Destination (Join-Path $taskClasses 'META-INF/LICENSE')
 Checked $taskJar @('--create','--file',(Join-Path $taskOutput 'launch-client-0.1.2.jar'),'-C',$taskClasses,'.')
 Copy-Item -LiteralPath (Join-Path $taskStage 'java/pom.xml') -Destination (Join-Path $taskOutput 'launch-client-0.1.2.pom')
 $taskArtifacts.Add('launch-client-0.1.2.pom')
 Language 'Java' 'launch-client-0.1.2.jar' 'licentivo-launch-java-0.1.2-source.zip' 'self_contained_jar'
}else{Language 'Java' 'licentivo-launch-java-0.1.2-source.zip' 'licentivo-launch-java-0.1.2-source.zip' 'self_contained_source'}
if(-not $SourceOnly -and -not $SkipRuby){
 Push-Location (Join-Path $taskStage 'ruby')
 try{Checked $Ruby @('-e',"require 'rubygems/gem_runner'; Gem::GemRunner.new.run(ARGV)",'--','build','licentivo-launch.gemspec','--output',(Join-Path $taskOutput 'licentivo-launch-0.1.2.gem'))}finally{Pop-Location}
 $taskRubyArtifact='licentivo-launch-0.1.2.gem'
 if($CurlRoot -and $CCompiler){
  $taskNative=Join-Path $taskStage 'ruby/lib/licentivo_launch/vendor/licentivo/native'
  New-Item -ItemType Directory -Path $taskNative -Force | Out-Null
  $taskCore=Join-Path $taskStage 'ruby/vendor/c'
  $taskCFiles=@('licentivo.c','vendor/cJSON.c','vendor/monocypher.c','vendor/monocypher-ed25519.c') | ForEach-Object {Join-Path $taskCore $_}
  Checked $CCompiler (@('-std=c11','-O2','-shared','-Wl,--export-all-symbols',('-I'+$taskCore),('-I'+(Join-Path $CurlRoot 'include')))+$taskCFiles+@(('-L'+(Join-Path $CurlRoot 'lib')),'-lcurl','-lbcrypt','-ladvapi32','-o',(Join-Path $taskNative 'licentivo_native.dll')))
  Get-ChildItem -LiteralPath (Join-Path $CurlRoot 'bin') -Filter '*.dll' | ForEach-Object {Copy-Item -LiteralPath $_.FullName -Destination $taskNative}
  $taskPlatform=(& $Ruby -e 'print Gem::Platform.local').Trim()
  $taskRubyArtifact='licentivo-launch-0.1.2-'+$taskPlatform+'.gem'
  $taskPrior=$env:LAUNCH_GEM_PRECOMPILED
  Push-Location (Join-Path $taskStage 'ruby')
  try{$env:LAUNCH_GEM_PRECOMPILED='1';Checked $Ruby @('-e',"require 'rubygems/gem_runner'; Gem::GemRunner.new.run(ARGV)",'--','build','licentivo-launch.gemspec','--output',(Join-Path $taskOutput $taskRubyArtifact))}finally{Pop-Location;$env:LAUNCH_GEM_PRECOMPILED=$taskPrior}
  $taskArtifacts.Add('licentivo-launch-0.1.2.gem')
 }
 Language 'Ruby' $taskRubyArtifact 'licentivo-launch-ruby-0.1.2-source.zip' 'self_contained_gem'
}else{Language 'Ruby' 'licentivo-launch-ruby-0.1.2-source.zip' 'licentivo-launch-ruby-0.1.2-source.zip' 'self_contained_source'}
if(-not $SourceOnly -and $BuildRustCrate){
 $taskRust=Join-Path $taskStage 'rust/Cargo.toml'
 Checked $Cargo @('generate-lockfile','--manifest-path',$taskRust,'--offline')
 Checked $Cargo @('package','--manifest-path',$taskRust,'--allow-dirty','--no-verify','--offline','--target-dir',(Join-Path $taskStage 'rust-target'))
 Copy-Item -LiteralPath (Join-Path $taskStage 'rust-target/package/licentivo-launch-0.1.2.crate') -Destination $taskOutput
 Language 'Rust' 'licentivo-launch-0.1.2.crate' 'licentivo-launch-rust-0.1.2-source.zip' 'self_contained_crate'
}else{Language 'Rust' 'licentivo-launch-rust-0.1.2-source.zip' 'licentivo-launch-rust-0.1.2-source.zip' 'self_contained_source'}
$taskNativeArtifact='licentivo-launch-native-0.1.2-source.zip'
if(-not $SourceOnly -and $BuildNative){
 $taskBuild=if($NativeBuildDir){$NativeBuildDir}else{Join-Path $taskStage 'native-build'}
 $taskPrefix=Join-Path $taskStage 'native-install'
 $taskCMakeArgs=@('-S',(Join-Path $taskStage 'native'),'-B',$taskBuild,'-DCMAKE_BUILD_TYPE=Release',('-DCMAKE_INSTALL_PREFIX='+$taskPrefix))
 if($CCompiler){$taskCMakeArgs+=@('-G','MinGW Makefiles',('-DCMAKE_MAKE_PROGRAM='+(Join-Path (Split-Path $CCompiler) 'mingw32-make.exe')),('-DCMAKE_C_COMPILER='+$CCompiler),('-DCMAKE_CXX_COMPILER='+($CCompiler -replace 'gcc.exe$','g++.exe')))}
 Checked $CMake $taskCMakeArgs
 Checked $CMake @('--build',$taskBuild,'--config','Release')
 Checked $CMake @('--install',$taskBuild,'--config','Release')
 $taskNativeArtifact='licentivo-launch-native-0.1.2-windows-amd64-gnu.zip'
 Zip $taskPrefix $taskNativeArtifact
}
Language 'C' $taskNativeArtifact 'licentivo-launch-native-0.1.2-source.zip' 'self_contained_cmake'
Language 'C++' $taskNativeArtifact 'licentivo-launch-native-0.1.2-source.zip' 'self_contained_cmake'
Copy-Item -LiteralPath (Join-Path $taskRoot 'sdk/managed-sdk-manifest.json') -Destination $taskOutput
$taskArtifacts.Add('managed-sdk-manifest.json')
$taskBundle=Join-Path $taskStage 'source-bundle'
Copy-Tree $taskGo $taskBundle
foreach($taskName in @('python','csharp','javascript','ruby','java','rust','native')){Copy-Tree (Join-Path $taskStage $taskName) (Join-Path $taskBundle ('sdk/'+$taskName))}
foreach($taskName in @('csharp-tests','csharp-runtime-tests')){Copy-Tree (Join-Path $taskRoot ('sdk/'+$taskName)) (Join-Path $taskBundle ('sdk/'+$taskName))}
foreach($taskName in @('rust','ruby','native')){Curl (Join-Path $taskBundle ('sdk/'+$taskName+'/vendor/curl'))}
foreach($taskName in @('VERSION','UPSTREAM_VERSION','managed-sdk-manifest.json')){Copy-Item -LiteralPath (Join-Path $taskRoot ('sdk/'+$taskName)) -Destination (Join-Path $taskBundle 'sdk')}
foreach($taskName in @('go','python','csharp','javascript','ruby','java','rust','native','c','cpp','web-backend')){Copy-Tree (Join-Path $taskRoot ('examples/'+$taskName)) (Join-Path $taskBundle ('examples/'+$taskName))}
Copy-Item -LiteralPath (Join-Path $taskRoot 'examples/README.md') -Destination (Join-Path $taskBundle 'examples')
Copy-Tree (Join-Path $taskRoot 'docs') (Join-Path $taskBundle 'docs')
New-Item -ItemType Directory -Path (Join-Path $taskBundle 'scripts/fixtures'),(Join-Path $taskBundle 'internal/httpapi/web') -Force | Out-Null
foreach($taskName in @('test-integration-packages.ps1','test-managed-installations.ps1','generate-integration-smoke.mjs','acceptance.py','test_acceptance.py','integration-prompts.test.mjs','integration-state.test.mjs')){Copy-Item -LiteralPath (Join-Path $PSScriptRoot $taskName) -Destination (Join-Path $taskBundle 'scripts')}
Copy-Item -LiteralPath (Join-Path $taskRoot 'internal/app/integration.go') -Destination (Join-Path $taskBundle 'scripts/fixtures/integration-contract.go.txt')
foreach($taskName in @('integration-prompts.js','integration-state.js')){Copy-Item -LiteralPath (Join-Path $taskRoot ('internal/httpapi/web/'+$taskName)) -Destination (Join-Path $taskBundle 'internal/httpapi/web')}
Zip $taskBundle 'licentivo-launch-0.1.2-source.zip'
$taskInventory=[ordered]@{schema_version=2;wrapper_version='0.1.2';upstream_sdk_version='0.11.1';sdk_dependency_management='embedded';requires_separate_official_sdk=$false;initialization='direct_parameters';configuration_files_required=$false;published=$false;languages=$taskLanguages.ToArray()}
$taskInventory.source_bundle='licentivo-launch-0.1.2-source.zip'
$taskInventory | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $taskOutput 'integration-packages.json') -Encoding utf8
$taskArtifacts.Add('integration-packages.json')
@'
# Launch integration packages 0.1.2

Install only the Launch artifact for your language. The pinned Licentivo SDK
0.11.1 implementation, adapters and license notices are embedded. Do not install
a separate official SDK, copy an adapter, or configure official SDK paths.

- Python: pip install ./licentivo_launch-0.1.2-py3-none-any.whl
- Node.js: npm install ./licentivo-launch-0.1.2.tgz
- C#: add this directory as a local NuGet source, then install Licentivo.Launch 0.1.2.
- Java: use launch-client-0.1.2.jar as your single Launch classpath entry.
- Go: extract its ZIP, require/replace only the Launch module, and call
  launchclient.Connect with the public parameters filled by Launch.
- Ruby: gem install --local the Launch gem matching your Ruby platform; a source
  gem is also supplied. The Windows platform gem includes the native DLL and curl.
- Rust: extract the source ZIP or .crate, add only a local licentivo-launch path
  dependency. Its build script compiles the embedded native SDK automatically.
- C/C++: extract the native install ZIP and find_package(LicentivoLaunch CONFIG).
  Call licentivo_launch_link(your_target), or add CXX for a C++ target; it handles SDK linking and bundled DLLs.
  The source ZIP can instead be built/installed through its own CMakeLists.txt.

Language runtimes and ordinary third-party dependencies (such as cryptography or
BouncyCastle) still use their normal package managers. Compiled native binaries
and bundled curl are Windows amd64 GNU builds; other platforms use the complete
source with a matching C11 toolchain and normal OS curl/OpenSSL development files.
No official SDK installation or source checkout is required. Nothing is published.
SHA256SUMS and integration-packages.json identify this local distribution.
'@ | Set-Content -LiteralPath (Join-Path $taskOutput 'README.md') -Encoding utf8
$taskArtifacts.Add('README.md')
$taskArtifacts | Sort-Object -Unique | ForEach-Object {(Get-FileHash -LiteralPath (Join-Path $taskOutput $_) -Algorithm SHA256).Hash.ToLowerInvariant()+'  '+$_} | Set-Content -LiteralPath (Join-Path $taskOutput 'SHA256SUMS') -Encoding ascii
Write-Output ('Managed nine-language packages: '+$taskOutput)
