# FocusLock lockdown enforcer for Windows. Runs as SYSTEM from the
# "FocusLock Lockdown" scheduled task. With -Loop (how the task runs it) it
# stays running: the time budgets every 5 seconds, every block put back every
# minute. The task restarts it within a minute if it's ever stopped.
param([switch]$Loop)
$ErrorActionPreference = 'SilentlyContinue'
$Src   = 'C:\ProgramData\FocusLockLockdown'
$Req   = "$Src\requests"   # your user may create <name>.open here
$State = "$Src\state"      # time used today; only SYSTEM/Administrators can write
$Dns   = @('185.228.168.10', '185.228.169.11')   # CleanBrowsing Adult filter
$Doh   = @('1.1.1.1','1.0.0.1','8.8.8.8','8.8.4.4','9.9.9.9','149.112.112.112',
           '208.67.222.222','208.67.220.220','94.140.14.14','94.140.15.15')
$script:OpenDomains = @()  # budget domains open right now

function Lines($f) { Get-Content "$Src\$f" | ForEach-Object { $_.Trim() } | Where-Object { $_ -and -not $_.StartsWith('#') } }
# Blocked domains, minus any budget domains that are open right now.
function BlockedDomains { Lines 'blocklist.txt' | Where-Object { $script:OpenDomains -notcontains $_ } }

function Apply-Hosts {
    $hosts = "$env:SystemRoot\System32\drivers\etc\hosts"
    $begin = '# >>> focuslock lockdown >>>'; $end = '# <<< focuslock lockdown <<<'
    $text  = (Get-Content $hosts -Raw) -replace "(?s)\r?\n?$([regex]::Escape($begin)).*?$([regex]::Escape($end))", ''
    $block = foreach ($d in BlockedDomains) { "0.0.0.0 $d"; "0.0.0.0 www.$d"; "0.0.0.0 m.$d" }
    $want  = $text.TrimEnd() + "`r`n$begin`r`n" + ($block -join "`r`n") + "`r`n$end`r`n"
    if ((Get-Content $hosts -Raw) -ne $want) {
        attrib -r $hosts; Set-Content $hosts $want -Encoding ascii; attrib +r $hosts; ipconfig /flushdns | Out-Null
    }
}

function Apply-Dns {
    # DNS filter on every adapter; Windows' own DNS-over-HTTPS off
    Get-NetAdapter | Where-Object Status -eq 'Up' | ForEach-Object {
        $cur = (Get-DnsClientServerAddress -InterfaceIndex $_.ifIndex -AddressFamily IPv4).ServerAddresses
        if (($cur -join ',') -ne ($Dns -join ',')) { Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ServerAddresses $Dns }
    }
    $k = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\DNSClient'
    New-Item $k -Force | Out-Null; Set-ItemProperty $k DoHPolicy 1 -Type DWord   # 1 = prohibit DoH

    # Firewall: only the filter may answer DNS; DoT and known DoH resolvers blocked
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
}

function Apply-BrowserPolicy {
    $ext = Lines 'allowed-extensions.txt'
    $blocked = @(BlockedDomains)
    foreach ($base in 'HKLM:\SOFTWARE\Policies\Google\Chrome','HKLM:\SOFTWARE\Policies\Microsoft\Edge','HKLM:\SOFTWARE\Policies\BraveSoftware\Brave') {
        New-Item $base -Force | Out-Null
        # QuicAllowed=0: HTTP/3 runs over UDP, which can't be cut when a budget closes.
        $p = @{ DnsOverHttpsMode='off'; IncognitoModeAvailability=1; InPrivateModeAvailability=1; BrowserGuestModeEnabled=0
                BrowserAddPersonEnabled=0; ForceGoogleSafeSearch=1; ForceBingSafeSearch=2; ForceYouTubeRestrict=2
                TorDisabled=1; BraveVPNDisabled=1; QuicAllowed=0 }
        foreach ($n in $p.Keys) {
            $t = if ($p[$n] -is [string]) { 'String' } else { 'DWord' }
            Set-ItemProperty $base $n $p[$n] -Type $t
        }
        $lists = @{ URLBlocklist=$blocked; ExtensionInstallBlocklist=@('*'); ExtensionInstallAllowlist=$ext }
        foreach ($n in $lists.Keys) {
            $want = @($lists[$n])
            $have = @((Get-Item "$base\$n").Property | ForEach-Object { (Get-ItemProperty "$base\$n").$_ })
            if (($have -join '|') -eq ($want -join '|')) { continue }   # unchanged: don't make Chrome reload
            Remove-Item "$base\$n" -Recurse -Force -ErrorAction SilentlyContinue; New-Item "$base\$n" -Force | Out-Null
            $i = 1; foreach ($v in $want) { Set-ItemProperty "$base\$n" "$i" $v -Type String; $i++ }
        }
    }
}

function Kill-Banned {
    $ifeo = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Image File Execution Options'
    foreach ($b in (Lines 'banned-apps.txt')) {
        if (-not (Test-Path "$ifeo\$b.exe")) { New-Item "$ifeo\$b.exe" -Force | Out-Null }
        Set-ItemProperty "$ifeo\$b.exe" Debugger 'C:\ProgramData\FocusLockLockdown\blocked.exe'
        Get-Process -Name $b -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    }
    Get-Process | Where-Object { $_.Path -match 'Tor Browser|tor-browser|Mullvad Browser' } | Stop-Process -Force
}

# --- Daily budgets (budgets.txt): open a blocked site for limited time ---

Add-Type -ErrorAction SilentlyContinue @"
using System; using System.Net; using System.Runtime.InteropServices;
public static class FocusLockTcp {
    [StructLayout(LayoutKind.Sequential)]
    struct Row { public uint State, LocalAddr, LocalPort, RemoteAddr, RemotePort; }
    [DllImport("iphlpapi.dll")] static extern int SetTcpEntry(ref Row row);
    static uint Port(int p) { return (uint)(ushort)IPAddress.HostToNetworkOrder((short)p); }
    static uint Addr(string a) { return BitConverter.ToUInt32(IPAddress.Parse(a).GetAddressBytes(), 0); }
    // Ends an open IPv4 TCP connection (state 12 = delete), like closing the tab's socket.
    public static int Close(string la, int lp, string ra, int rp) {
        Row r = new Row { State = 12, LocalAddr = Addr(la), LocalPort = Port(lp), RemoteAddr = Addr(ra), RemotePort = Port(rp) };
        return SetTcpEntry(ref r);
    }
}
"@

function Budgets {
    Lines 'budgets.txt' | ForEach-Object {
        $f = $_ -split '\s+'
        [pscustomobject]@{ Name=$f[0]; Daily=[int]$f[1]; Session=[int]$f[2]; Domains=@($f[3..($f.Count-1)]) }
    }
}

# While a budget site is closed, its servers' addresses are blocked and open
# connections to them are ended, so an open tab stops working too.
function Apply-BudgetFirewall {
    $name = 'FocusLock: budget sites'
    Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
    if ($script:OpenDomains.Count -gt 0) { return }
    $ips = @(Get-Content "$State\ips-*" | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+$' } | Sort-Object -Unique)
    if ($ips.Count -eq 0) { return }
    New-NetFirewallRule -DisplayName $name -Direction Outbound -Action Block -RemoteAddress $ips | Out-Null
    Get-NetTCPConnection -State Established | Where-Object { $ips -contains $_.RemoteAddress } | ForEach-Object {
        [FocusLockTcp]::Close($_.LocalAddress, $_.LocalPort, $_.RemoteAddress, $_.RemotePort) | Out-Null
    }
}

# Learn the servers' addresses while the site is open (DNS isn't blocked then).
function Remember-Ips($b) {
    $f = "$State\ips-$($b.Name)"
    $new = foreach ($d in $b.Domains) {
        foreach ($h in $d, "api.$d") { (Resolve-DnsName $h -Type A -DnsOnly).IPAddress }
    }
    @(Get-Content $f) + @($new) | Where-Object { $_ } | Sort-Object -Unique | Select-Object -Last 200 | Set-Content $f
}

function Budget-Tick([int]$dt) {
    $today = Get-Date -Format 'yyyy-MM-dd'
    $open = @()
    foreach ($b in Budgets) {
        $f = "$State\budget-$($b.Name)"
        $day, $used, $sess = (Get-Content $f -ErrorAction SilentlyContinue) -split ' '
        $used = [int]$used; $sess = [int]$sess
        if ($day -ne $today) { $used = 0; $sess = 0 }
        $req = "$Req\$($b.Name).open"
        if (Test-Path $req) {
            if ($used -ge $b.Daily * 60) { Remove-Item $req -Force; $sess = 0 }
            elseif ($sess -ge $b.Session * 60) { Remove-Item $req -Force; $sess = 0 }
            else { $used += $dt; $sess += $dt; $open += $b.Domains; Remember-Ips $b }
        } else { $sess = 0 }
        "$today $used $sess" | Set-Content $f
        $st = if (Test-Path $req) {
            "open: $([int][math]::Floor(($b.Session * 60 - $sess) / 60)) min left in this session, $([int][math]::Floor(($b.Daily * 60 - $used) / 60)) min left today"
        } else {
            "closed: $([math]::Max(0, [int][math]::Floor(($b.Daily * 60 - $used) / 60))) of $($b.Daily) min left today"
        }
        "$($b.Name) $st" | Set-Content "$Req\$($b.Name).status"
    }
    if (($open -join ' ') -ne ($script:OpenDomains -join ' ')) {
        $script:OpenDomains = $open
        Apply-Hosts; Apply-BrowserPolicy; Apply-BudgetFirewall
    }
}

function Apply-All { Apply-Hosts; Apply-Dns; Apply-BrowserPolicy; Apply-BudgetFirewall; Kill-Banned }

New-Item $Req, $State -ItemType Directory -Force | Out-Null
Budget-Tick 0
Apply-All
if ($Loop) {
    while ($true) {
        for ($i = 0; $i -lt 12; $i++) { Start-Sleep 5; Budget-Tick 5; Kill-Banned }
        Apply-All
    }
}
