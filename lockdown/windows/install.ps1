# Install the FocusLock lockdown. Run in an elevated PowerShell:
#   powershell -ExecutionPolicy Bypass -File lockdown\windows\install.ps1
$ErrorActionPreference = 'Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole('Administrators')) {
    throw 'Run this from an elevated (Administrator) PowerShell.'
}
$Src = 'C:\ProgramData\FocusLockLockdown'
New-Item $Src -ItemType Directory -Force | Out-Null
Copy-Item "$PSScriptRoot\enforce.ps1", "$PSScriptRoot\..\blocklist.txt", "$PSScriptRoot\..\banned-apps.txt", "$PSScriptRoot\..\budgets.txt" $Src -Force
New-Item "$Src\bin", "$Src\requests", "$Src\state" -ItemType Directory -Force | Out-Null
Copy-Item "$PSScriptRoot\focuslock-x.ps1", "$PSScriptRoot\focuslock-x.cmd" "$Src\bin" -Force

# Keep the extensions you already have; block installing new ones.
$ids = 'Google\Chrome','Microsoft\Edge','BraveSoftware\Brave-Browser' |
       ForEach-Object { Get-ChildItem "C:\Users\*\AppData\Local\$_\User Data\*\Extensions\*" -Directory -ErrorAction SilentlyContinue } |
       Where-Object Name -match '^[a-p]{32}$' | Select-Object -Expand Name -Unique
$ids | Set-Content "$Src\allowed-extensions.txt"

# Only SYSTEM and Administrators may change the rules.
icacls $Src /inheritance:r /grant:r 'SYSTEM:(OI)(CI)F' 'Administrators:(OI)(CI)F' 'Users:(OI)(CI)RX' | Out-Null
# ...except the requests folder, where "focuslock-x open" leaves its request.
icacls "$Src\requests" /grant 'Users:(OI)(CI)M' | Out-Null

# focuslock-x on everyone's PATH
$path = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if (($path -split ';') -notcontains "$Src\bin") {
    [Environment]::SetEnvironmentVariable('Path', $path.TrimEnd(';') + ";$Src\bin", 'Machine')
}

# Uninstall Tor Browser / Firefox if they're installed per user.
Get-ChildItem 'C:\Users\*\Desktop\Tor Browser','C:\Users\*\AppData\Local\Tor Browser' -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force

# Run as SYSTEM at boot and every minute. It keeps running (-Loop); the
# every-minute trigger restarts it if it was stopped (IgnoreNew skips the
# trigger while it's running).
Stop-ScheduledTask -TaskName 'FocusLock Lockdown' -ErrorAction SilentlyContinue
$action   = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$Src\enforce.ps1`" -Loop"
$triggers = @((New-ScheduledTaskTrigger -AtStartup),
              (New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 1)))
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew `
                -ExecutionTimeLimit ([TimeSpan]::Zero)
Register-ScheduledTask -TaskName 'FocusLock Lockdown' -Action $action -Trigger $triggers -Settings $settings `
    -User 'SYSTEM' -RunLevel Highest -Force | Out-Null

# A separate process, so this script's "Stop on any error" doesn't apply to it.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$Src\enforce.ps1"
Start-ScheduledTask -TaskName 'FocusLock Lockdown'
Write-Host 'X budget: "focuslock-x open | close | status" in a new terminal.'
Write-Host "Done. Restart your browsers and check chrome://policy / edge://policy."
Write-Host "Last step: make your daily Windows account a Standard user, or this can all be undone."
