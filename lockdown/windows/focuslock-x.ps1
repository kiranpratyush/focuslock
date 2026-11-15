# Open X for a while out of its daily budget (see budgets.txt).
#   focuslock-x open | close | status
param([string]$Command = 'status', [string]$Name = 'x')
$Req = 'C:\ProgramData\FocusLockLockdown\requests'
$status = "$Req\$Name.status"
switch ($Command) {
    'open' {
        if ((Get-Content $status -ErrorAction SilentlyContinue) -match ': 0 of') { Get-Content $status; exit 1 }
        New-Item "$Req\$Name.open" -ItemType File -Force | Out-Null
        Write-Host "Opening $Name (takes up to 5 seconds)..."; Start-Sleep 7
        Get-Content $status
        Write-Host 'Post, then close it: focuslock-x close'
    }
    'close'  { Remove-Item "$Req\$Name.open" -Force -ErrorAction SilentlyContinue; Start-Sleep 7; Get-Content $status }
    'status' { if (Test-Path $status) { Get-Content $status } else { "The lockdown task isn't running." } }
    default  { 'use: focuslock-x open | close | status'; exit 2 }
}
