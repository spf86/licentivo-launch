[CmdletBinding()]
param(
    [string]$Source,
    [string]$Python = 'python',
    [string]$DotNet = 'dotnet',
    [string]$Npm = 'npm.cmd',
    [string]$Ruby = 'ruby',
    [string]$JavaHome,
    [string]$Cargo = 'cargo',
    [string]$CMake = 'cmake',
    [string]$NativeBuildDir,
    [string]$Output,[string]$CurlRoot,[string]$CCompiler,
    [switch]$SkipPython,
    [switch]$SkipDotNet,
    [switch]$SkipJavaScript,
    [switch]$SkipRuby,
    [switch]$SkipJava,
    [switch]$SourceOnly,
    [switch]$BuildRustCrate,
    [switch]$BuildNative
)
$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'build-managed-integration-packages.ps1') @PSBoundParameters
