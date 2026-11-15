# FocusLock lockdown enforcer for Windows. Runs as SYSTEM every minute from the
# "FocusLock Lockdown" scheduled task and puts every block back.
$ErrorActionPreference = 'SilentlyContinue'
$Src  = 'C:\ProgramData\FocusLockLockdown'
$Dns  = @('185.228.168.10', '185.228.169.11')   # CleanBrowsing Adult filter
$Doh  = @('1.1.1.1','1.0.0.1','8.8.8.8','8.8.4.4','9.9.9.9','149.112.112.112',
          '208.67.222.222','208.67.220.220','94.140.14.14','94.140.15.15')
function Lines($f) { Get-Content "$Src\$f" | ForEach-Object { $_.Trim() } | Where-Object { $_ -and -not $_.StartsWith('#') } }
$Domains = Lines 'blocklist.txt'
$Banned  = Lines 'banned-apps.txt'

# 1. hosts file
$hosts = "$env:SystemRoot\System32\drivers\etc\hosts"
$begin = '# >>> focuslock lockdown >>>'; $end = '# <<< focuslock lockdown <<<'
$text  = (Get-Content $hosts -Raw) -replace "(?s)\r?\n?$([regex]::Escape($begin)).*?$([regex]::Escape($end))", ''
$block = foreach ($d in $Domains) { "0.0.0.0 $d"; "0.0.0.0 www.$d"; "0.0.0.0 m.$d" }
$want  = $text.TrimEnd() + "`r`n$begin`r`n" + ($block -join "`r`n") + "`r`n$end`r`n"
if ((Get-Content $hosts -Raw) -ne $want) {
    attrib -r $hosts; Set-Content $hosts $want -Encoding ascii; attrib +r $hosts; ipconfig /flushdns | Out-Null
}

# 2. DNS filter on every adapter; Windows' own DNS-over-HTTPS off
Get-NetAdapter | Where-Object Status -eq 'Up' | ForEach-Object {
    $cur = (Get-DnsClientServerAddress -InterfaceIndex $_.ifIndex -AddressFamily IPv4).ServerAddresses
    if (($cur -join ',') -ne ($Dns -join ',')) { Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ServerAddresses $Dns }
}
$k = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\DNSClient'
New-Item $k -Force | Out-Null; Set-ItemProperty $k DoHPolicy 1 -Type DWord   # 1 = prohibit DoH

# 3. Firewall: only the filter may answer DNS; DoT and known DoH resolvers blocked
$fw = @(
  @{N='FocusLock: DNS elsewhere (UDP)'; P='UDP'; Port='53'; A=$null},
  @{N='FocusLock: DNS elsewhere (TCP)'; P='TCP'; Port='53'; A=$null},
  @{N='FocusLock: DNS-over-TLS';        P='TCP'; Port='853'; A=$null},
  @{N='FocusLock: DoH resolvers';       P='TCP'; Port='443'; A=$Doh})
# "Everything except the filter": the IPv4 space minus the two filter addresses.
$notFilter = @('1.0.0.0-185.228.168.9','185.228.168.11-185.228.169.10','185.228.169.12-223.255.255.255')
foreach ($r in $fw) {
    $addr = if ($r.A) { $r.A } else { $notFilter }
    Remove-NetFirewallRule -DisplayName $r.N -ErrorAction SilentlyContinue
    New-NetFirewallRule -DisplayName $r.N -Direction Outbound -Action Block -Protocol $r.P -RemotePort $r.Port -RemoteAddress $addr | Out-Null
}
# IPv6 DNS (from the router) would skip the filter too.
foreach ($pr in 'UDP','TCP') {
    Remove-NetFirewallRule -DisplayName "FocusLock: IPv6 DNS ($pr)" -ErrorAction SilentlyContinue
    New-NetFirewallRule -DisplayName "FocusLock: IPv6 DNS ($pr)" -Direction Outbound -Action Block -Protocol $pr -RemotePort 53,853 -RemoteAddress '2000::/3' | Out-Null
}

# 4. Browser policies (Chrome, Edge, Brave)
$ext = Lines 'allowed-extensions.txt'
foreach ($base in 'HKLM:\SOFTWARE\Policies\Google\Chrome','HKLM:\SOFTWARE\Policies\Microsoft\Edge','HKLM:\SOFTWARE\Policies\BraveSoftware\Brave') {
    New-Item $base -Force | Out-Null
    $p = @{ DnsOverHttpsMode='off'; IncognitoModeAvailability=1; InPrivateModeAvailability=1; BrowserGuestModeEnabled=0
            BrowserAddPersonEnabled=0; ForceGoogleSafeSearch=1; ForceBingSafeSearch=2; ForceYouTubeRestrict=2
            TorDisabled=1; BraveVPNDisabled=1 }
    foreach ($n in $p.Keys) {
        $t = if ($p[$n] -is [string]) { 'String' } else { 'DWord' }
        Set-ItemProperty $base $n $p[$n] -Type $t
    }
    $lists = @{ URLBlocklist=$Domains; ExtensionInstallBlocklist=@('*'); ExtensionInstallAllowlist=$ext }
    foreach ($n in $lists.Keys) {
        Remove-Item "$base\$n" -Recurse -Force -ErrorAction SilentlyContinue; New-Item "$base\$n" -Force | Out-Null
        $i = 1; foreach ($v in $lists[$n]) { Set-ItemProperty "$base\$n" "$i" $v -Type String; $i++ }
    }
}

# 5. Banned programs can't start (Image File Execution Options), and are killed if running
$ifeo = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Image File Execution Options'
foreach ($b in $Banned) {
    New-Item "$ifeo\$b.exe" -Force | Out-Null
    Set-ItemProperty "$ifeo\$b.exe" Debugger 'C:\ProgramData\FocusLockLockdown\blocked.exe'
    Get-Process -Name $b -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
Get-Process | Where-Object { $_.Path -match 'Tor Browser|tor-browser|Mullvad Browser' } | Stop-Process -Force
