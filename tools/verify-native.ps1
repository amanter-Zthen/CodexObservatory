param([string]$GoExe = 'go')
$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
$native = Join-Path $project 'native'
$report = Join-Path $project 'verification\native'
New-Item -ItemType Directory -Path $report -Force | Out-Null
# Changes below exist only in this verifier process; compiler dependencies are vendored.
# No network/proxy environment setting is changed.
$oldGo = @{}
foreach($name in @('GOTOOLCHAIN','GOPROXY','GOSUMDB','GOCACHE')) { $oldGo[$name] = [Environment]::GetEnvironmentVariable($name,'Process') }
try {
 $env:GOTOOLCHAIN='local'
 $env:GOPROXY='off'
 $env:GOSUMDB='off'
 $env:GOCACHE=Join-Path $report 'build-cache'
 Push-Location $native
 try {
  $version = (& $GoExe version | Out-String).Trim()
  if($LASTEXITCODE -ne 0){ throw 'Go compiler unavailable' }
  $cases = @(
   @{name='build-all';args=@('build','-mod=vendor','./...')},
   @{name='floatui';args=@('test','-mod=vendor','-count=1','-json','./internal/floatui')},
   @{name='route-model';args=@('test','-mod=vendor','-count=1','-json','./internal/routetrace','./internal/routefeed','./internal/completion','./internal/lifecycle')},
   @{name='recovery';args=@('test','-mod=vendor','-count=1','-json','-run','Test(HeaderlessSSE|.*Resync|.*RouteSync|.*SSE|.*ResponseScoped|.*Auxiliary)','.', './internal/wirecapture')}
  )
  $results = @()
  foreach($case in $cases) {
   $arguments = $case.args
   $output = & $GoExe @arguments 2>&1
   $code = $LASTEXITCODE
   $output | Set-Content -LiteralPath (Join-Path $report ($case.name+'.log')) -Encoding utf8
   $results += [pscustomobject]@{name=$case.name;arguments=$arguments;exitCode=$code;pass=($code -eq 0)}
   Write-Output "$($case.name): exit $code"
   if($code -ne 0){ $output | Select-Object -Last 20 }
  }
  $passed = @($results | Where-Object {!$_.pass}).Count -eq 0
  [ordered]@{version='0.4.9';nativeBaseline='0.4.8-nacre';at=(Get-Date).ToString('o');go=$version;result=$(if($passed){'PASS'}else{'FAIL'});checks=$results;scope='Offline build; pure snapshot/state tests; isolated loopback SSE fixtures. Production binaries, watchers and proxy settings untouched.'} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $report 'result.json') -Encoding utf8
  if(!$passed){throw 'Native verification failed; inspect verification/native.'}
 } finally { Pop-Location }
} finally {
 foreach($name in $oldGo.Keys) { [Environment]::SetEnvironmentVariable($name,$oldGo[$name],'Process') }
}
