[CmdletBinding()]
param(
 [Parameter(Mandatory=$true)][string]$Packages,
 [ValidateSet('go','python','csharp','java','javascript','rust','ruby','c','cpp')][string[]]$Languages=@('go','python','csharp','java','javascript','rust','ruby','c','cpp'),
 [string]$Python='python',[string]$DotNet='dotnet',[string]$Npm='npm.cmd',[string]$Node='node',[string]$Ruby='ruby',[string]$Cargo='cargo',[string]$JavaHome,
 [string]$CMake='cmake',[string]$CCompiler,[string]$NuGetDependencyCache,[string]$Output
)
$ErrorActionPreference='Stop'
$taskRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskPackages=[IO.Path]::GetFullPath($Packages)
$taskOutput=if($Output){[IO.Path]::GetFullPath($Output)}else{Join-Path $taskRoot ('artifacts/integration-checks/managed-installs-'+[guid]::NewGuid().ToString('N'))}
if(-not $taskOutput.StartsWith($taskRoot+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Evidence must be inside the workspace.'}
if(Test-Path -LiteralPath $taskOutput){throw 'Choose a new evidence directory.'}
$taskInventory=Get-Content -LiteralPath (Join-Path $taskPackages 'integration-packages.json') -Raw | ConvertFrom-Json
if($taskInventory.schema_version -ne 2 -or $taskInventory.requires_separate_official_sdk -ne $false -or @($taskInventory.languages).Count -ne 9){throw 'Expected a managed nine-language distribution.'}
foreach($taskLine in (Get-Content -LiteralPath (Join-Path $taskPackages 'SHA256SUMS'))){
 if($taskLine -notmatch '^([a-f0-9]{64})  ([^/\\]+)$'){throw 'Invalid artifact checksum record.'}
 $taskExpected=$Matches[1];$taskName=$Matches[2]
 if((Get-FileHash -LiteralPath (Join-Path $taskPackages $taskName) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskExpected){throw "Artifact checksum mismatch: $taskName"}
}
New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
$taskResults=[Collections.Generic.List[object]]::new()
function Write-UTF8([string]$Path,[string]$Content){New-Item -ItemType Directory -Path (Split-Path $Path) -Force | Out-Null;[IO.File]::WriteAllText($Path,$Content,[Text.UTF8Encoding]::new($false))}
function Checked([string]$Program,[string[]]$Arguments){& $Program @Arguments;if($LASTEXITCODE -ne 0){throw "Consumer command failed ($LASTEXITCODE): $Program"}}
function Run-Language([string]$Name,[scriptblock]$Action){
 $taskConsumer=Join-Path $taskOutput $Name
 New-Item -ItemType Directory -Path $taskConsumer -Force | Out-Null
 $taskStatus='passed'
 Push-Location -LiteralPath $taskConsumer
 try{Checked $Node @((Join-Path $PSScriptRoot 'generate-integration-smoke.mjs'),'--language',$Name,'--output',(Join-Path $taskConsumer 'direct')); & $Action *>&1 | Tee-Object -FilePath (Join-Path $taskOutput ($Name+'.log')) | Out-Host}catch{$taskStatus='failed';$_.Exception.Message | Add-Content -LiteralPath (Join-Path $taskOutput ($Name+'.log'));Write-Warning "$Name consumer failed"}finally{Pop-Location}
 $taskResults.Add([ordered]@{language=$Name;result=$taskStatus;log=($Name+'.log');separate_official_sdk=$false;initialization='direct_parameters';configuration_files_required=$false})
}
function Unzip([string]$Name,[string]$Destination){Expand-Archive -LiteralPath (Join-Path $taskPackages $Name) -DestinationPath $Destination}
foreach($taskLanguage in @($Languages | Select-Object -Unique)){
 switch($taskLanguage){
 'python'{Run-Language 'python' {
  $taskTarget=Join-Path $taskConsumer 'site'
  Checked $Python @('-m','pip','install','--no-index','--no-deps','--target',$taskTarget,(Join-Path $taskPackages 'licentivo_launch-0.1.5-py3-none-any.whl'))
  Copy-Item -LiteralPath (Join-Path $taskRoot 'sdk/python/tests') -Destination (Join-Path $taskConsumer 'tests') -Recurse
  $taskPrior=$env:PYTHONPATH
  try{
   $env:PYTHONPATH=$taskTarget
   Checked $Python @('-c','import pathlib,licentivo_launch; assert pathlib.Path(licentivo_launch.__file__).is_relative_to(pathlib.Path("site").resolve()); print("Installed Launch wheel:",licentivo_launch.__version__)')
   Checked $Python @('-m','unittest','discover','-s','tests','-v')
   Checked $Python @('direct/main.py')
  }finally{$env:PYTHONPATH=$taskPrior}
 }}
 'javascript'{Run-Language 'javascript' {
  Write-UTF8 (Join-Path $taskConsumer 'package.json') '{"private":true,"type":"module"}'
  Checked $Npm @('install','--offline','--ignore-scripts','--no-audit','--no-fund',(Join-Path $taskPackages 'licentivo-launch-0.1.5.tgz'))
  $taskManifest=Get-Content -LiteralPath 'node_modules/@licentivo/launch/package.json' -Raw | ConvertFrom-Json
  if($taskManifest.peerDependencies -or $taskManifest.dependencies){throw 'Unexpected separate SDK dependency in npm package.'}
  foreach($taskTest in (Get-ChildItem -LiteralPath (Join-Path $taskRoot 'sdk/javascript/test') -Filter '*.test.mjs')){
   $taskCode=[IO.File]::ReadAllText($taskTest.FullName).Replace("'../src/index.mjs'","'@licentivo/launch'").Replace("'../src/","'./node_modules/@licentivo/launch/src/")
   Write-UTF8 (Join-Path $taskConsumer $taskTest.Name) $taskCode
  }
  $taskTests=@(Get-ChildItem -LiteralPath $taskConsumer -Filter '*.test.mjs' | ForEach-Object FullName)
  Checked $Node (@('--test')+$taskTests)
  Checked $Node @('--test','direct/connect-smoke.test.mjs')
 }}
 'csharp'{Run-Language 'csharp' {
  $taskFeed=Join-Path $taskConsumer 'feed';New-Item -ItemType Directory -Path $taskFeed -Force | Out-Null
  $taskDependencyRoot=if($NuGetDependencyCache){$NuGetDependencyCache}else{Join-Path $taskRoot '.cache/nuget'}
  Copy-Item -LiteralPath (Join-Path $taskDependencyRoot 'bouncycastle.cryptography/2.6.2/bouncycastle.cryptography.2.6.2.nupkg') -Destination $taskFeed
  Write-UTF8 (Join-Path $taskConsumer 'NuGet.Config') ('<configuration><packageSources><clear/><add key="launch" value="'+[Security.SecurityElement]::Escape($taskPackages)+'"/><add key="ordinary" value="'+[Security.SecurityElement]::Escape($taskFeed)+'"/></packageSources></configuration>')
  $taskPrior=$env:NUGET_PACKAGES
  try{
   $env:NUGET_PACKAGES=Join-Path $taskConsumer 'packages'
   foreach($taskSuite in @('csharp-tests','csharp-runtime-tests')){
    $taskSuiteRoot=Join-Path $taskConsumer $taskSuite
    New-Item -ItemType Directory -Path $taskSuiteRoot -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $taskRoot ('sdk/'+$taskSuite+'/Program.cs')) -Destination $taskSuiteRoot
    Write-UTF8 (Join-Path $taskSuiteRoot 'Consumer.csproj') '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable></PropertyGroup><ItemGroup><PackageReference Include="Licentivo.Launch" Version="0.1.5"/><FrameworkReference Include="Microsoft.AspNetCore.App"/></ItemGroup></Project>'
    Checked $DotNet @('run','--project',(Join-Path $taskSuiteRoot 'Consumer.csproj'),'-p:UseSharedCompilation=false')
   }
   Checked $DotNet @('run','--project',(Join-Path $taskConsumer 'direct/Consumer.csproj'),'-p:UseSharedCompilation=false')
   if(Test-Path -LiteralPath (Join-Path $env:NUGET_PACKAGES 'licentivo.sdk')){throw 'Consumer installed an official SDK package.'}
  }finally{$env:NUGET_PACKAGES=$taskPrior}
 }}
 'java'{Run-Language 'java' {
  $taskJavac=if($JavaHome){Join-Path $JavaHome 'bin/javac.exe'}else{'javac'}
  $taskJava=if($JavaHome){Join-Path $JavaHome 'bin/java.exe'}else{'java'}
  $taskJar=Join-Path $taskPackages 'launch-client-0.1.5.jar'
  $taskTests=@(Get-ChildItem -LiteralPath (Join-Path $taskRoot 'sdk/java/tests') -Filter '*.java' | ForEach-Object FullName)
  $taskTests+=@(Get-ChildItem -LiteralPath (Join-Path $taskConsumer 'direct') -Filter '*.java' | ForEach-Object FullName)
  Checked $taskJavac (@('--release','17','-encoding','UTF-8','-cp',$taskJar,'-d',$taskConsumer)+$taskTests)
  foreach($taskClass in @('LaunchTests','OfficialSDKTests','ConnectSmoke')){Checked $taskJava @('-ea','-cp',($taskConsumer+[IO.Path]::PathSeparator+$taskJar),$taskClass)}
 }}
 'ruby'{Run-Language 'ruby' {
  $taskPriorHome=$env:GEM_HOME;$taskPriorPath=$env:GEM_PATH;$taskPriorSDK=$env:LICENTIVO_NATIVE_LIBRARY;$taskPriorCurl=$env:LICENTIVO_CURL_ROOT
  try{
   $env:GEM_HOME=Join-Path $taskConsumer 'gems';$env:GEM_PATH=$env:GEM_HOME;$env:LICENTIVO_NATIVE_LIBRARY=$null;$env:LICENTIVO_CURL_ROOT=$null
   $taskGemCache=Join-Path ((& $Ruby -e 'print Gem.default_dir').Trim()) 'cache'
   $taskBase64=@(Get-ChildItem -LiteralPath $taskGemCache -Filter 'base64-*.gem')
   if(-not $taskBase64.Count){throw 'Cache the ordinary base64 gem before running this offline consumer check.'}
   $taskBase64 | ForEach-Object {Copy-Item -LiteralPath $_.FullName -Destination $taskConsumer}
   $taskGem=@($taskInventory.languages | Where-Object language -eq 'Ruby')[0].artifact
   Checked $Ruby @('-e',"require 'rubygems/gem_runner'; Gem::GemRunner.new.run(ARGV)",'--','install','--local','--no-document',(Join-Path $taskPackages $taskGem))
   foreach($taskTest in (Get-ChildItem -LiteralPath (Join-Path $taskRoot 'sdk/ruby/test') -Filter 'test_*.rb')){
    $taskCode=[IO.File]::ReadAllText($taskTest.FullName).Replace("require_relative '../lib/licentivo_launch/vendor/licentivo'","require 'licentivo_launch/vendor/licentivo'").Replace("require_relative '../lib/licentivo_launch'","require 'licentivo_launch'")
    $taskTestPath=Join-Path $taskConsumer $taskTest.Name;Write-UTF8 $taskTestPath $taskCode
    Checked $Ruby @($taskTestPath)
   }
   Checked $Ruby @('direct/main.rb')
   Checked $Ruby @('-e',"require 'licentivo_launch'; raise 'unexpected official gem' if Gem.loaded_specs.key?('licentivo-sdk'); puts Gem.loaded_specs.fetch('licentivo-launch').full_gem_path")
  }finally{$env:GEM_HOME=$taskPriorHome;$env:GEM_PATH=$taskPriorPath;$env:LICENTIVO_NATIVE_LIBRARY=$taskPriorSDK;$env:LICENTIVO_CURL_ROOT=$taskPriorCurl}
 }}
 'go'{Run-Language 'go' {
  $taskModule=Join-Path $taskConsumer 'module';Unzip 'licentivo-launch-go-0.1.5.zip' $taskModule
  Push-Location $taskModule
  try{Checked 'go' @('test','./pkg/launchclient','-count=1')}finally{Pop-Location}
  Write-UTF8 (Join-Path $taskConsumer 'go.mod') ("module consumer`n`ngo 1.26.0`n`nrequire github.com/spf86/licentivo-launch-sdk v0.1.5`nreplace github.com/spf86/licentivo-launch-sdk => `""+$taskModule.Replace('\','/')+"`"`n")
  Checked 'go' @('run','-mod=mod','./direct')
  $taskModules=& go list -m all
  if($LASTEXITCODE -ne 0 -or ($taskModules -join "`n") -match 'licentivo/sdk'){throw 'Go consumer requires a separate official SDK module.'}
 }}
 'rust'{Run-Language 'rust' {
  $taskPriorCurl=$env:LICENTIVO_CURL_ROOT;$taskPriorTarget=$env:CARGO_TARGET_DIR
  try{
   $env:LICENTIVO_CURL_ROOT=$null;$env:CARGO_TARGET_DIR=Join-Path $taskConsumer 'target'
   Checked 'tar' @('-xf',(Join-Path $taskPackages 'licentivo-launch-0.1.5.crate'),'-C',$taskConsumer)
   $taskCrate=Join-Path $taskConsumer 'licentivo-launch-0.1.5'
   Checked $Cargo @('test','--manifest-path',(Join-Path $taskCrate 'Cargo.toml'),'--offline','--locked')
   Write-UTF8 (Join-Path $taskConsumer 'Cargo.toml') ('[package]'+"`n"+'name="launch-consumer"'+"`n"+'version="0.0.0"'+"`n"+'edition="2021"'+"`n"+'[dependencies]'+"`n"+'licentivo-launch={path="./licentivo-launch-0.1.5"}'+"`n"+'[[bin]]'+"`n"+'name="launch-consumer"'+"`n"+'path="direct/main.rs"'+"`n")
   Checked $Cargo @('run','--manifest-path',(Join-Path $taskConsumer 'Cargo.toml'),'--offline')
   Checked (Join-Path $env:CARGO_TARGET_DIR 'debug/launch-consumer.exe') @()
   if((Get-Content -LiteralPath (Join-Path $taskConsumer 'Cargo.lock') -Raw) -match 'name = "licentivo"'){throw 'Rust consumer requires a separate official SDK crate.'}
  }finally{$env:LICENTIVO_CURL_ROOT=$taskPriorCurl;$env:CARGO_TARGET_DIR=$taskPriorTarget}
 }}
 {$_ -in @('c','cpp')}{Run-Language $taskLanguage {
  if(-not $CCompiler){throw 'Set -CCompiler to the native GNU C compiler matching this binary distribution.'}
  $taskPrefix=Join-Path $taskConsumer 'relocated';Unzip 'licentivo-launch-native-0.1.5-windows-amd64-gnu.zip' $taskPrefix
  $taskCPP=$taskLanguage -eq 'cpp';$taskExtension=if($taskCPP){'cpp'}else{'c'};$taskMode=if($taskCPP){' CXX'}else{''}
  Write-UTF8 (Join-Path $taskConsumer 'CMakeLists.txt') ('cmake_minimum_required(VERSION 3.18)'+"`n"+'project(consumer LANGUAGES C'+$taskMode+")`n"+'find_package(LicentivoLaunch CONFIG REQUIRED)'+"`n"+'add_executable(consumer direct/main.'+$taskExtension+")`n"+'licentivo_launch_link(consumer'+$taskMode+")`n")
  $taskPrior=$env:PATH
  try{
   $env:PATH=(Split-Path $CCompiler)+';'+$env:PATH
   $taskBuild=Join-Path $taskConsumer 'build'
   Checked $CMake @('-S',$taskConsumer,'-B',$taskBuild,'-G','MinGW Makefiles',('-DCMAKE_MAKE_PROGRAM='+(Join-Path (Split-Path $CCompiler) 'mingw32-make.exe')),('-DCMAKE_C_COMPILER='+$CCompiler),('-DCMAKE_CXX_COMPILER='+($CCompiler -replace 'gcc.exe$','g++.exe')),('-DCMAKE_PREFIX_PATH='+$taskPrefix))
   Checked $CMake @('--build',$taskBuild)
   Checked (Join-Path $taskBuild 'consumer.exe') @()
  }finally{$env:PATH=$taskPrior}
 }}
 }
}
$taskPassed=@($taskResults | Where-Object result -eq 'passed').Count
$taskReport=[ordered]@{schema_version=1;scope='fresh_consumer_installations';package_directory=$taskPackages;wrapper_version=$taskInventory.wrapper_version;upstream_sdk_version=$taskInventory.upstream_sdk_version;results=$taskResults.ToArray();all_nine_passed=($taskResults.Count -eq 9 -and $taskPassed -eq 9);external_acceptance_complete=$false;completed_at=[DateTimeOffset]::UtcNow.ToString('o')}
$taskReport | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $taskOutput 'report.json') -Encoding utf8
Write-Output "Fresh Launch consumers: $taskPassed/$($taskResults.Count). Evidence: $taskOutput"
if($taskPassed -ne $taskResults.Count){exit 1}
