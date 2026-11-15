# Remove the lockdown (needs Administrator, on purpose).
Unregister-ScheduledTask -TaskName 'FocusLock Lockdown' -Confirm:$false -ErrorAction SilentlyContinue
$Src = 'C:\ProgramData\FocusLockLockdown'
$banned = Get-Content "$Src\banned-apps.txt" | Where-Object { $_ -and -not $_.StartsWith('#') }
foreach ($b in $banned) { Remove-Item "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Image File Execution Options\$($b.Trim()).exe" -Recurse -Force -ErrorAction SilentlyContinue }
'HKLM:\SOFTWARE\Policies\Google\Chrome','HKLM:\SOFTWARE\Policies\Microsoft\Edge','HKLM:\SOFTWARE\Policies\BraveSoftware\Brave' |
    ForEach-Object { Remove-Item $_ -Recurse -Force -ErrorAction SilentlyContinue }
Remove-ItemProperty 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\DNSClient' DoHPolicy -ErrorAction SilentlyContinue
Get-NetFirewallRule -DisplayName 'FocusLock:*' | Remove-NetFirewallRule
Get-NetAdapter | ForEach-Object { Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ResetServerAddresses }
$hosts = "$env:SystemRoot\System32\drivers\etc\hosts"
attrib -r $hosts
(Get-Content $hosts -Raw) -replace '(?s)\r?\n?# >>> focuslock lockdown >>>.*?# <<< focuslock lockdown <<<\r?\n?', '' | Set-Content $hosts -Encoding ascii
Remove-Item $Src -Recurse -Force
Write-Host 'Lockdown removed.'
