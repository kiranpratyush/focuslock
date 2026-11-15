# One command to build and install FocusLock on Windows.
# Installs whatever is missing (Git, Go, Visual Studio C++ Build Tools,
# Python + aqtinstall, Qt 6 for MSVC), then builds and installs FocusLock
# for your user (Start menu + PATH).
#
#   powershell -ExecutionPolicy Bypass -File setup.ps1          from a clone
#   irm https://raw.githubusercontent.com/kiranpratyush/focuslock/main/setup.ps1 | iex
#
# Run it from a normal PowerShell. The Build Tools installer asks for admin
# (a UAC prompt) the first time; nothing else needs it.
$ErrorActionPreference = 'Stop'
$RepoUrl    = 'https://github.com/kiranpratyush/focuslock.git'
$QtVersion  = '6.8.3'                      # the same Qt as CI
$QtArch     = 'win64_msvc2022_64'
$QtRoot     = Join-Path $env:LOCALAPPDATA 'FocusLock\Qt'
$GoMin      = [version]'1.24'

function Step($m) { Write-Host "== $m" -ForegroundColor Cyan }
function Have($c) { [bool](Get-Command $c -ErrorAction SilentlyContinue) }
function RefreshPath {
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' +
                [Environment]::GetEnvironmentVariable('Path', 'User')
}
function Winget($id, $extra = @()) {
    if (-not (Have winget)) { throw 'winget is missing. Install "App Installer" from the Microsoft Store, then run this again.' }
    Step "installing $id"
    winget install --id $id -e --silent --accept-package-agreements --accept-source-agreements @extra
    if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne -1978335189) {   # -1978335189 = already installed
        throw "winget install $id failed ($LASTEXITCODE)"
    }
    RefreshPath
}

# 1. Git and the source
if (-not (Have git)) { Winget 'Git.Git' }
$here = if ($PSScriptRoot) { $PSScriptRoot } else { '' }
if ($here -and (Test-Path (Join-Path $here 'go.mod')) -and (Test-Path (Join-Path $here 'app'))) {
    $Repo = $here
} else {
    $Repo = Join-Path $HOME 'focuslock'
    if (-not (Test-Path (Join-Path $Repo '.git'))) { git clone $RepoUrl $Repo }
}
Step "FocusLock source: $Repo"

# 2. Go 1.24+
function GoOk {
    if (-not (Have go)) { return $false }
    $v = (go env GOVERSION) -replace '^go', '' -replace '[^0-9.].*$', ''
    return ([version]$v -ge $GoMin)
}
if (-not (GoOk)) { Winget 'GoLang.Go' }
if (-not (GoOk)) { throw "Go $GoMin+ is needed. Open a new PowerShell and run this again." }

# 3. Visual Studio C++ build tools (cl, nmake)
$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
function VsPath {
    if (-not (Test-Path $vswhere)) { return $null }
    & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
}
if (-not (VsPath)) {
    Winget 'Microsoft.VisualStudio.2022.BuildTools' @('--override',
        '--quiet --wait --norestart --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended')
}
$vs = VsPath
if (-not $vs) { throw 'Visual Studio C++ Build Tools did not install. Install "Desktop development with C++" from the Visual Studio Installer, then run this again.' }
Step "Visual Studio: $vs"
Import-Module (Join-Path $vs 'Common7\Tools\Microsoft.VisualStudio.DevShell.dll')
Enter-VsDevShell -VsInstallPath $vs -SkipAutomaticLocation -DevCmdArguments '-arch=x64 -host_arch=x64' | Out-Null

# 4. Qt 6 for MSVC (qmake, windeployqt), via aqtinstall into %LOCALAPPDATA%
$QtBin = Join-Path $QtRoot "$QtVersion\msvc2022_64\bin"
if (-not (Have qmake) -and -not (Test-Path (Join-Path $QtBin 'qmake.exe'))) {
    $py = if (Have py) { 'py' } elseif (Have python) { 'python' } else { $null }
    if (-not $py) { Winget 'Python.Python.3.12'; $py = if (Have py) { 'py' } else { 'python' } }
    Step "installing Qt $QtVersion (about 1 GB, takes a while)"
    & $py -m pip install --user --upgrade aqtinstall
    if ($LASTEXITCODE -ne 0) { throw 'pip install aqtinstall failed' }
    & $py -m aqt install-qt windows desktop $QtVersion $QtArch -O $QtRoot
    if ($LASTEXITCODE -ne 0) { throw 'Qt download failed' }
}
if (Test-Path $QtBin) { $env:Path = "$QtBin;$env:Path" }
if (-not (Have qmake)) { throw "qmake not found. Expected it in $QtBin." }
Step "Qt: $((Get-Command qmake).Source)"

# 5. Build and install
Step 'building'
& (Join-Path $Repo 'app\bin\build.ps1')
Step 'installing'
& (Join-Path $Repo 'app\bin\install.ps1')

Write-Host ''
Write-Host 'Done. Open FocusLock from the Start menu, or run "focuslock-engine status" in a new terminal.' -ForegroundColor Green
