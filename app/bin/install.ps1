# Installs app\dist for this user: %LOCALAPPDATA%\Programs\FocusLock, a
# Start menu entry, and the folder on your PATH for the focuslock commands.
# No admin needed. Run bin\build.ps1 first, or unzip the CI build into dist.
$ErrorActionPreference = "Stop"
$App = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $App "dist"
if (-not (Test-Path (Join-Path $Dist "focuslock.exe"))) { throw "No build in $Dist. Run bin\build.ps1 first." }

$Target = Join-Path $env:LOCALAPPDATA "Programs\FocusLock"
# A running bar holds its files open; a session keeps going without it.
Get-Process focuslock -ErrorAction SilentlyContinue | Stop-Process -Force
New-Item -ItemType Directory -Force $Target | Out-Null
Copy-Item -Recurse -Force (Join-Path $Dist "*") $Target

$Menu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\FocusLock.lnk"
$shell = New-Object -ComObject WScript.Shell
$link = $shell.CreateShortcut($Menu)
$link.TargetPath = Join-Path $Target "focuslock.exe"
$link.Arguments = "open"
$link.WorkingDirectory = $Target
$link.Description = "Start a focus session you can't casually quit"
$link.Save()

$path = [Environment]::GetEnvironmentVariable("Path", "User")
if (($path -split ";") -notcontains $Target) {
    [Environment]::SetEnvironmentVariable("Path", ($path.TrimEnd(";") + ";" + $Target), "User")
    Write-Host "Added $Target to your PATH (new terminals)."
}
Write-Host "Installed to $Target. Open FocusLock from the Start menu."
