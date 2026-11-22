# Checks that the scripts parse and that Kill-Banned kills the right programs.
# It really kills banned programs that are running. Run: lockdown\windows\test.ps1
$ErrorActionPreference = 'Stop'
$ok = $true
function Check($what, $pass) { if ($pass) { Write-Host "PASS $what" } else { Write-Host "FAIL $what"; $script:ok = $false } }

foreach ($f in 'enforce.ps1', 'install.ps1', 'uninstall.ps1') {
    $errs = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\$f", [ref]$null, [ref]$errs)
    Check "$f parses" ($errs.Count -eq 0)
    if ($f -eq 'enforce.ps1') { $enforce = $ast }
}
# Only these functions, not the rest of the enforcer.
$Src = (Resolve-Path "$PSScriptRoot\..").Path
$enforce.FindAll({ $args[0] -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true) |
    Where-Object { $_.Name -in 'Lines', 'Kill-Banned' } | ForEach-Object { . ([scriptblock]::Create($_.Extent.Text)) }

$tmp = Join-Path $env:TEMP "focuslock-test-$PID"
New-Item "$tmp\steamapps\common\g" -ItemType Directory -Force | Out-Null
function Start-Fake($path) { Copy-Item "$env:SystemRoot\System32\PING.EXE" $path; Start-Process $path '-n 60 127.0.0.1' -PassThru -WindowStyle Hidden }
$named = Start-Fake "$tmp\lutris.exe"
$folder = Start-Fake "$tmp\steamapps\common\g\unlisted.exe"
$control = Start-Fake "$tmp\notagame.exe"
Start-Sleep 2
Check 'the fake programs are running' (-not ($named.HasExited -or $folder.HasExited -or $control.HasExited))
Kill-Banned
Start-Sleep 2
Check 'a banned name is killed' $named.HasExited
Check 'an unlisted program in a game folder is killed' $folder.HasExited
Check 'another program is left alone' (-not $control.HasExited)
Write-Host ("Kill-Banned takes {0:N0} ms" -f (Measure-Command { Kill-Banned }).TotalMilliseconds)
if (-not $control.HasExited) { $control.Kill() }
Start-Sleep 1
Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
if (-not $ok) { exit 1 }
