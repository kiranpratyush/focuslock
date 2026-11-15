"""Write the iPad lock profiles into a directory.

    python3 -I profiles.py OUT_DIR

focuslock-lock.mobileconfig      DNS filter, web filter, blocked apps, restrictions.
focuslock-noinstall.mobileconfig No installing or deleting apps. Kept separate so
                                 `ipad release` can lift it to install an app.

Both are non-removable on a supervised iPad; only this laptop (the host that
installed them, with the supervision keybag) can take them off.
"""
import pathlib
import plistlib
import sys
import uuid

ROOT = pathlib.Path(__file__).resolve().parent
ORG = "FocusLock"
ID = "dev.focuslock.ipad"

# CleanBrowsing Adult filter, the same one the laptops, phones and router use.
DOH = "https://doh.cleanbrowsing.org/doh/adult-filter/"
DNS_SERVERS = ["185.228.168.10", "185.228.169.11", "2a0d:2a00:1::1", "2a0d:2a00:2::1"]

# YouTube is allowed on the iPad (with a Screen Time limit), so its domains
# are left out of the shared blocklist.
ALLOWED = {"youtube.com", "youtu.be", "youtube-nocookie.com", "youtubei.googleapis.com",
           "ytimg.com", "googlevideo.com"}

# Apps that can't open even if they're installed (supervised only).
BLOCKED_APPS = [
    # video and feeds (YouTube itself is allowed)
    "com.google.ios.youtubemusic", "com.google.ios.youtubekids",
    "com.burbn.instagram", "com.burbn.barcelona", "com.facebook.Facebook",
    "com.zhiliaoapp.musically", "com.reddit.Reddit", "com.atebits.Tweetie2",
    "com.toyopagroup.picaboo", "pinterest", "com.netflix.Netflix",
    "com.amazon.aiv.AIVApp", "in.startv.hotstar", "com.jio.media.ondemand", "tv.twitch",
    # other browsers (Safari is the one browser, with the web filter)
    "com.google.chrome.ios", "org.mozilla.ios.Firefox", "org.mozilla.ios.Focus",
    "com.brave.ios.browser", "com.microsoft.msedge", "com.duckduckgo.mobile.ios",
    "com.opera.OperaTouch", "com.opera.mini", "com.vivaldi.browser",
    "com.miketigas.OnionBrowser", "com.ucweb.iphone.lowversion",
    # VPNs and DNS changers
    "ch.protonmail.vpn", "com.nordvpn.NordVPN", "com.cloudflare.1dot1dot1dot1",
    "ca.psiphon.Psiphon", "com.windscribe.vpn", "net.mullvad.MullvadVPN",
    "com.expressvpn.ExpressVPN",
]


def blocklist():
    sites = []
    for line in (ROOT.parent / "blocklist.txt").read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and line not in ALLOWED:
            sites.append(line)
    return sites


def payload(kind, name, **body):
    return {
        "PayloadType": kind,
        "PayloadIdentifier": f"{ID}.{name.lower().replace(' ', '-')}",
        "PayloadUUID": str(uuid.uuid5(uuid.NAMESPACE_URL, f"{ID}/{name}")).upper(),
        "PayloadVersion": 1,
        "PayloadDisplayName": name,
        **body,
    }


def profile(ident, name, description, content):
    return {
        "PayloadType": "Configuration",
        "PayloadIdentifier": ident,
        "PayloadUUID": str(uuid.uuid5(uuid.NAMESPACE_URL, ident)).upper(),
        "PayloadVersion": 1,
        "PayloadDisplayName": name,
        "PayloadDescription": description,
        "PayloadOrganization": ORG,
        "PayloadScope": "System",
        "PayloadRemovalDisallowed": True,
        "PayloadContent": content,
    }


lock = profile(f"{ID}.lock", "FocusLock", "Adult filter, blocked sites and apps, no escape routes.", [
    payload("com.apple.dnsSettings.managed", "Filtered DNS",
            DNSSettings={"DNSProtocol": "HTTPS", "ServerURL": DOH, "ServerAddresses": DNS_SERVERS},
            ProhibitDisablement=True),
    payload("com.apple.webcontent-filter", "Web filter",
            FilterType="BuiltIn", AutoFilterEnabled=True, DenyListURLs=[f"https://{s}" for s in blocklist()]),
    payload("com.apple.applicationaccess", "Restrictions",
            blockedAppBundleIDs=BLOCKED_APPS,
            allowEraseContentAndSettings=False,         # no factory reset from Settings
            allowUIConfigurationProfileInstallation=False,
            allowVPNCreation=False,
            allowCloudPrivateRelay=False,               # would skip the DNS filter
            allowAccountModification=False,             # no new accounts or Apple ID changes
            allowEnterpriseAppTrust=False,
            allowAppClips=False,                        # App Clips run without installing
            allowMarketplaceAppInstallation=False,
            allowWebDistributionAppInstallation=False,
            allowExplicitContent=False),
])

noinstall = profile(f"{ID}.noinstall", "FocusLock: no installs", "No installing or deleting apps.", [
    payload("com.apple.applicationaccess", "No installs",
            allowAppInstallation=False,
            allowUIAppInstallation=False,
            allowAppRemoval=False,
            allowAutomaticAppDownloads=False),
])

out = pathlib.Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
(out / "focuslock-lock.mobileconfig").write_bytes(plistlib.dumps(lock))
(out / "focuslock-noinstall.mobileconfig").write_bytes(plistlib.dumps(noinstall))
