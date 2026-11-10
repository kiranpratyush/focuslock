# Go tests, then the Qt tests offscreen. Same toolchain as build.ps1.
$ErrorActionPreference = "Stop"
$App = Split-Path -Parent $PSScriptRoot
$Repo = Split-Path -Parent $App
function Check($what) { if ($LASTEXITCODE -ne 0) { throw "$what failed ($LASTEXITCODE)" } }

Push-Location $Repo
go vet ./...; Check "go vet"
go test ./...; Check "go test"
Pop-Location

$Tests = Join-Path $App "build\tests"
New-Item -ItemType Directory -Force $Tests | Out-Null
Push-Location $Tests
qmake (Join-Path $App "tests\focuslock_tests.pro"); Check "qmake"
if (Get-Command jom -ErrorAction SilentlyContinue) { jom; Check "jom" }
elseif (Get-Command nmake -ErrorAction SilentlyContinue) { nmake; Check "nmake" }
else { mingw32-make -j $env:NUMBER_OF_PROCESSORS; Check "make" }
$env:QT_QPA_PLATFORM = "offscreen"
$exe = Get-ChildItem . -Recurse -Filter focuslock_tests.exe | Select-Object -First 1
& $exe.FullName; Check "Qt tests"
Pop-Location
