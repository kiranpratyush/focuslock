# Builds FocusLock for Windows into app\dist: the Go engine, the Qt app, and
# the Qt libraries it needs (windeployqt). Needs Go 1.24+, Qt 6 (MSVC) with
# its bin folder on PATH, and a Visual Studio developer shell.
$ErrorActionPreference = "Stop"
$App = Split-Path -Parent $PSScriptRoot
$Repo = Split-Path -Parent $App
$Build = Join-Path $App "build"
$Dist = Join-Path $App "dist"

function Check($what) { if ($LASTEXITCODE -ne 0) { throw "$what failed ($LASTEXITCODE)" } }

New-Item -ItemType Directory -Force $Build | Out-Null
Push-Location $Repo
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o (Join-Path $Build "focuslock-engine.exe") ./cmd/focuslock; Check "go build"
Pop-Location

Push-Location $Build
qmake (Join-Path $App "focuslock.pro"); Check "qmake"
if (Get-Command jom -ErrorAction SilentlyContinue) { jom; Check "jom" }
elseif (Get-Command nmake -ErrorAction SilentlyContinue) { nmake; Check "nmake" }
else { mingw32-make -j $env:NUMBER_OF_PROCESSORS; Check "make" }
Pop-Location

if (Test-Path $Dist) { Remove-Item -Recurse -Force $Dist }
New-Item -ItemType Directory -Force $Dist | Out-Null
$exe = Get-ChildItem $Build -Recurse -Filter focuslock.exe | Select-Object -First 1
Copy-Item $exe.FullName $Dist
Copy-Item (Join-Path $Build "focuslock-engine.exe") $Dist
windeployqt --release --no-translations --qmldir (Join-Path $App "src") (Join-Path $Dist "focuslock.exe"); Check "windeployqt"

Write-Host "Built $Dist"
