[CmdletBinding()]
param(
  [string]$Sdk = '',
  [string]$VcVars = ''
)
$ErrorActionPreference = 'Stop'
$sdkVersion = '1.0.2903.40'
$root = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$desktop = Join-Path $root 'desktop'
$build = Join-Path $desktop 'build'
$output = Join-Path $desktop 'CodexObservatory.exe'
$reportPath = Join-Path $build 'build-report.json'
$writableBuild = $false
$sourceFiles = @('desktop/host.cpp', 'desktop/composition_host.h', 'desktop/legacy_menu.h', 'desktop/vendor/json.hpp', 'tools/build-desktop.ps1')
$report = [ordered]@{
  formatVersion = 1
  kind = 'codex-observatory-desktop-build'
  started = [DateTime]::UtcNow.ToString('o')
  result = 'FAIL'
  webview2Sdk = $sdkVersion
  browserBundled = $false
  sourceHashes = [ordered]@{}
  sourceUnchanged = $false
  artifact = $null
}

function Get-SHA256([string]$File) {
  $stream = [System.IO.File]::OpenRead($File)
  $algorithm = [System.Security.Cryptography.SHA256]::Create()
  try { return ([BitConverter]::ToString($algorithm.ComputeHash($stream))).Replace('-', '').ToLowerInvariant() }
  finally { $algorithm.Dispose(); $stream.Dispose() }
}

function Assert-PlainPath([string]$File) {
  $cursor = [System.IO.Path]::GetFullPath($File)
  while ($cursor) {
    if (Test-Path -LiteralPath $cursor) {
      if (((Get-Item -LiteralPath $cursor -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Build inputs and outputs must use ordinary files and directories, without junctions or symbolic links.'
      }
    }
    $parent = [System.IO.Path]::GetDirectoryName($cursor)
    if ($parent -eq $cursor) { break }
    $cursor = $parent
  }
}

function Find-Sdk {
  if ($Sdk) { return $Sdk }
  if ($env:OBSERVATORY_WEBVIEW2_SDK) { return $env:OBSERVATORY_WEBVIEW2_SDK }
  $candidates = @((Join-Path $root ('.deps/Microsoft.Web.WebView2.' + $sdkVersion)))
  if ($env:NUGET_PACKAGES) { $candidates += Join-Path $env:NUGET_PACKAGES ('microsoft.web.webview2/' + $sdkVersion) }
  if ($env:USERPROFILE) { $candidates += Join-Path $env:USERPROFILE ('.nuget/packages/microsoft.web.webview2/' + $sdkVersion) }
  foreach ($candidate in $candidates) { if (Test-Path -LiteralPath $candidate -PathType Container) { return $candidate } }
  throw ('WebView2 SDK ' + $sdkVersion + ' is required. Extract the official Microsoft.Web.WebView2 NuGet package and pass -Sdk, or set OBSERVATORY_WEBVIEW2_SDK. This script does not download dependencies.')
}

function Find-VcVars {
  if ($VcVars) { return $VcVars }
  if ($env:OBSERVATORY_VCVARS) { return $env:OBSERVATORY_VCVARS }
  $candidates = @()
  if ($env:VSINSTALLDIR) { $candidates += Join-Path $env:VSINSTALLDIR 'VC/Auxiliary/Build/vcvars64.bat' }
  $vswhere = $null
  if (${env:ProgramFiles(x86)}) {
    $path = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
    if (Test-Path -LiteralPath $path -PathType Leaf) { $vswhere = $path }
  }
  if (!$vswhere) {
    $command = Get-Command vswhere.exe -ErrorAction SilentlyContinue
    if ($command) { $vswhere = $command.Source }
  }
  if ($vswhere) {
    $installations = @(& $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath)
    foreach ($installation in $installations) {
      if ($installation) { $candidates += Join-Path $installation.Trim() 'VC/Auxiliary/Build/vcvars64.bat' }
    }
  }
  foreach ($programRoot in @($env:ProgramFiles, ${env:ProgramFiles(x86)}) | Where-Object { $_ }) {
    foreach ($year in @('2022', '2019', '2017')) {
      foreach ($edition in @('BuildTools', 'Community', 'Professional', 'Enterprise')) {
        $candidates += Join-Path $programRoot ('Microsoft Visual Studio/' + $year + '/' + $edition + '/VC/Auxiliary/Build/vcvars64.bat')
      }
    }
  }
  foreach ($candidate in $candidates) { if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate } }
  throw 'MSVC x64 build tools and a Windows SDK are required. Pass -VcVars or set OBSERVATORY_VCVARS to vcvars64.bat.'
}

try {
  if ($env:OS -ne 'Windows_NT') { throw 'The desktop host is built on Windows with MSVC x64 and a Windows SDK.' }
  Assert-PlainPath $build
  Assert-PlainPath $output
  New-Item -ItemType Directory -Force -Path $build | Out-Null
  $writableBuild = $true
  foreach ($file in $sourceFiles) {
    $source = Join-Path $root $file
    Assert-PlainPath $source
    if (!(Test-Path -LiteralPath $source -PathType Leaf)) { throw ('Build source is missing: ' + $file) }
    $report.sourceHashes[$file] = Get-SHA256 $source
  }

  $resolvedSdk = [System.IO.Path]::GetFullPath((Find-Sdk))
  $resolvedVcVars = [System.IO.Path]::GetFullPath((Find-VcVars))
  $include = Join-Path $resolvedSdk 'build/native/include'
  $header = Join-Path $include 'WebView2.h'
  $loader = Join-Path $resolvedSdk 'build/native/x64/WebView2LoaderStatic.lib'
  $nuspec = Join-Path $resolvedSdk 'Microsoft.Web.WebView2.nuspec'
  foreach ($file in @($resolvedVcVars, $header, $loader, $nuspec)) {
    Assert-PlainPath $file
    if (!(Test-Path -LiteralPath $file -PathType Leaf)) { throw 'SDK or MSVC prerequisite is missing; verify -Sdk and -VcVars.' }
  }
  [xml]$metadata = Get-Content -LiteralPath $nuspec -Raw
  $versionNode = $metadata.SelectSingleNode('/*[local-name()="package"]/*[local-name()="metadata"]/*[local-name()="version"]')
  $idNode = $metadata.SelectSingleNode('/*[local-name()="package"]/*[local-name()="metadata"]/*[local-name()="id"]')
  if (!$versionNode -or $versionNode.InnerText -ne $sdkVersion -or !$idNode -or $idNode.InnerText -ne 'Microsoft.Web.WebView2') {
    throw ('This build requires the official Microsoft.Web.WebView2 ' + $sdkVersion + ' package metadata.')
  }
  $report.sdkInputs = [ordered]@{ headerSHA256 = Get-SHA256 $header; loaderSHA256 = Get-SHA256 $loader; packageMetadataSHA256 = Get-SHA256 $nuspec }
  # cmd.exe expands percent signs twice through CALL. Reject ambiguous batch paths.
  foreach ($file in @($root, $resolvedSdk, $resolvedVcVars)) {
    if ($file -match '[%"!\r\n]') { throw 'Build paths must not contain percent signs, exclamation marks, quotes or newlines.' }
  }
  $commandFile = Join-Path $build 'build.cmd'
  $command = @"
@chcp 65001 >nul
@call "$resolvedVcVars" >nul
@if errorlevel 1 exit /b %errorlevel%
@cl /nologo /std:c++17 /EHsc /O2 /MT /utf-8 /W4 /DUNICODE /D_UNICODE /D_WIN32_WINNT=0x0A00 /I"$include" "..\host.cpp" /Fo"host.obj" /Fe"..\CodexObservatory.exe" /link /SUBSYSTEM:WINDOWS /INCREMENTAL:NO "$loader" user32.lib gdi32.lib ole32.lib oleaut32.lib shell32.lib shlwapi.lib winhttp.lib version.lib advapi32.lib dcomp.lib
@exit /b %errorlevel%
"@
  [System.IO.File]::WriteAllText($commandFile, ($command -replace "`r?`n", "`r`n") + "`r`n", (New-Object System.Text.UTF8Encoding($false)))
  Push-Location -LiteralPath $build
  try {
    & $env:ComSpec /d /v:off /c ('"' + $commandFile + '"') 2>&1 | Tee-Object -FilePath (Join-Path $build 'compiler.log') | Out-Host
    $compilerExit = $LASTEXITCODE
    if ($compilerExit -ne 0) { throw ('C++ compiler returned ' + $compilerExit + '; see desktop/build/compiler.log.') }
  } finally { Pop-Location }

  $image = [System.IO.File]::ReadAllBytes($output)
  if ($image.Length -lt 64 -or [BitConverter]::ToUInt16($image, 0) -ne 0x5a4d) { throw 'The compiler output is not a PE image.' }
  $pe = [BitConverter]::ToUInt32($image, 0x3c)
  if ($pe -gt $image.Length - 96 -or [BitConverter]::ToUInt32($image, $pe) -ne 0x4550 -or [BitConverter]::ToUInt16($image, $pe + 4) -ne 0x8664 -or [BitConverter]::ToUInt16($image, $pe + 24) -ne 0x20b -or [BitConverter]::ToUInt16($image, $pe + 24 + 68) -ne 2) {
    throw 'The host must be an AMD64 PE32+ Windows GUI executable.'
  }
  foreach ($file in $sourceFiles) {
    if ((Get-SHA256 (Join-Path $root $file)) -ne $report.sourceHashes[$file]) { throw ('Source changed during compilation: ' + $file) }
  }
  if ((Get-SHA256 $header) -ne $report.sdkInputs.headerSHA256 -or (Get-SHA256 $loader) -ne $report.sdkInputs.loaderSHA256 -or (Get-SHA256 $nuspec) -ne $report.sdkInputs.packageMetadataSHA256) {
    throw 'The WebView2 SDK changed during compilation.'
  }
  $report.sourceUnchanged = $true
  $report.artifact = [ordered]@{ path = 'desktop/CodexObservatory.exe'; bytes = $image.Length; sha256 = Get-SHA256 $output; architecture = 'amd64'; subsystem = 2 }
  $report.result = 'PASS'
} catch {
  $report.error = $_.Exception.Message
  throw
} finally {
  $report.finished = [DateTime]::UtcNow.ToString('o')
  if ($writableBuild -and (Test-Path -LiteralPath $build -PathType Container)) {
    [System.IO.File]::WriteAllText($reportPath, ($report | ConvertTo-Json -Depth 8), (New-Object System.Text.UTF8Encoding($false)))
  }
}
[pscustomobject]@{ Result = $report.result; Output = 'desktop/CodexObservatory.exe'; SHA256 = $report.artifact.sha256; WebView2Sdk = $sdkVersion; BrowserBundled = $false; Report = 'desktop/build/build-report.json' } | ConvertTo-Json
