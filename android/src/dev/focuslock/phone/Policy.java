package dev.focuslock.phone;

import android.app.admin.DevicePolicyManager;
import android.content.ComponentName;
import android.content.Context;
import android.content.SharedPreferences;
import android.os.Bundle;
import android.os.UserManager;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.util.ArrayList;
import java.util.List;

// The rules. The app is the phone's Device Owner, so Android enforces these
// for it: no installing apps, no factory reset from Settings, no VPN, Private
// DNS locked to the adult filter, distracting apps hidden, Chrome policies.
final class Policy {
    static final String TAG = "FocusLock";
    static final String DNS_HOST = "adult-filter-dns.cleanbrowsing.org";
    static final String CHROME = "com.android.chrome";

    // Hidden (as good as uninstalled, and installing is blocked).
    static final String[] HIDDEN = {
        // video and feeds
        "com.google.android.youtube", "com.google.android.apps.youtube.music",
        "com.google.android.apps.youtube.kids", "com.instagram.android",
        "com.instagram.barcelona", "com.facebook.katana", "com.facebook.lite",
        "com.zhiliaoapp.musically", "com.ss.android.ugc.trill", "com.reddit.frontpage",
        "com.twitter.android", "com.snapchat.android", "com.pinterest",
        "com.netflix.mediaclient", "com.amazon.avod.thirdpartyclient", "in.startv.hotstar",
        "com.jio.media.ondemand", "tv.twitch.android.app", "in.mohalla.sharechat",
        "in.mohalla.video", "com.eterno.shortvideos", "com.next.innovation.takatak",
        // other browsers (Chrome is the one browser, with policies)
        "org.mozilla.firefox", "org.mozilla.firefox_beta", "org.mozilla.fenix",
        "org.mozilla.focus", "org.torproject.torbrowser", "org.torproject.android",
        "com.opera.browser", "com.opera.mini.native", "com.opera.gx", "com.brave.browser",
        "com.microsoft.emmx", "com.sec.android.app.sbrowser", "com.duckduckgo.mobile.android",
        "com.kiwibrowser.browser", "com.vivaldi.browser", "com.UCMobile.intl",
        "mark.via.gp", "com.mi.globalbrowser",
        // VPNs and proxies
        "com.cloudflare.onedotonedotonedotone", "com.protonvpn.android", "com.nordvpn.android",
        "free.vpn.unblock.proxy.turbovpn", "com.psiphon3", "com.psiphon3.subscription",
        "com.expressvpn.vpn", "com.windscribe.vpn", "net.mullvad.mullvadvpn",
        // games and game streaming
        "com.chess", "org.lichess.mobileapp", "com.matiks.app", "com.playdigious.deadcells.mobile",
        "com.google.android.play.games", "com.playstation.remoteplay", "com.valvesoftware.steamlink",
        "com.nvidia.geforcenow", "com.microsoft.xcloud", "com.limelight",
        "com.roblox.client", "com.mojang.minecraftpe", "com.pubg.imobile", "com.tencent.ig",
        "com.dts.freefireth", "com.dts.freefiremax", "com.activision.callofduty.shooter",
        "com.supercell.clashofclans", "com.supercell.clashroyale", "com.supercell.brawlstars",
        "com.king.candycrushsaga", "com.kiloo.subwaysurf", "com.ludo.king", "com.miHoYo.GenshinImpact",
        "com.dream11.fantasy.cricket.football.kabaddi",
    };

    // Shopping apps that share one daily limit (LIMIT --ei shop_minutes N).
    static final String[] SHOPPING = {
        "in.amazon.mShop.android.shopping", "com.amazon.mShop.android.shopping", "in.swiggy.android",
        "com.swiggy.android.instamart", "com.flipkart.android", "com.flipkart.shopsy", "com.myntra.android",
        "com.meesho.supply", "com.ril.ajio", "com.tul.tatacliq", "com.fsn.nykaa", "com.lenskart.app",
        "co.shopney.snitch", "com.application.zomato", "com.grofers.customerapp", "com.zeptoconsumerapp",
        "com.bigbasket.mobileapp", "com.jpl.jiomart", "com.snapdeal.main", "com.ebay.mobile",
    };

    // Their sites are always blocked in Chrome while there's a shopping limit:
    // Android can't tell how long one site was open, only Chrome as a whole.
    // A leading dot means that exact host, so aws.amazon.com keeps working.
    static final String[] SHOPPING_SITES = {
        "amazon.in", ".amazon.com", ".www.amazon.com", ".m.amazon.com", "amzn.in", "amzn.to",
        "swiggy.com", "flipkart.com", "fkrt.it", "shopsy.in", "myntra.com", "meesho.com", "ajio.com",
        "tatacliq.com", "nykaa.com", "nykaafashion.com", "lenskart.com", "snitch.com", "snitch.co.in",
        "zomato.com", "blinkit.com", "zepto.com", "zeptonow.com", "bigbasket.com", "jiomart.com",
        "snapdeal.com", "ebay.com", "ebay.in",
    };

    static final String[] RESTRICTIONS = {
        UserManager.DISALLOW_INSTALL_APPS,
        UserManager.DISALLOW_INSTALL_UNKNOWN_SOURCES_GLOBALLY,
        UserManager.DISALLOW_FACTORY_RESET,
        UserManager.DISALLOW_SAFE_BOOT,
        UserManager.DISALLOW_ADD_USER,
        UserManager.DISALLOW_ADD_MANAGED_PROFILE,
        "no_add_private_profile",               // Android 15 Private Space
        UserManager.DISALLOW_CONFIG_VPN,
        UserManager.DISALLOW_CONFIG_PRIVATE_DNS,
        UserManager.DISALLOW_CONFIG_DATE_TIME,
        UserManager.DISALLOW_APPS_CONTROL,      // no disabling/clearing apps in Settings
    };

    static ComponentName admin(Context c) { return new ComponentName(c, AdminReceiver.class); }
    static DevicePolicyManager dpm(Context c) { return c.getSystemService(DevicePolicyManager.class); }
    static SharedPreferences prefs(Context c) { return c.getSharedPreferences("focuslock", Context.MODE_PRIVATE); }
    static boolean isFinal(Context c) { return prefs(c).getBoolean("final", false); }

    // Per-phone exceptions, e.g. YouTube on a phone that's shared with family.
    // Set over USB in test mode only; frozen once the final lock is on.
    static java.util.Set<String> allowed(Context c) {
        return prefs(c).getStringSet("allow", java.util.Collections.<String>emptySet());
    }
    static boolean youtubeAllowed(Context c) { return allowed(c).contains("com.google.android.youtube"); }
    static int youtubeLimitMinutes(Context c) { return prefs(c).getInt("yt_limit", 0); }
    static boolean youtubeOver(Context c) { return prefs(c).getBoolean("yt_over", false); }

    static int shopLimitMinutes(Context c) { return prefs(c).getInt("shop_limit", 0); }

    // Daily limits. Lowering it is always allowed; raising it or
    // removing it only in test mode.
    static String setLimit(Context c, String name, String pref, int minutes) {
        int cur = prefs(c).getInt(pref, 0);
        boolean looser = cur > 0 && (minutes <= 0 || minutes > cur);
        if (isFinal(c) && looser) return "Refused: final lock is on; the limit can only go down.";
        prefs(c).edit().putInt(pref, Math.max(minutes, 0)).commit();
        if (minutes > 0) UsageWatcher.start(c);
        UsageWatcher.check(c);
        return name + " limit: " + (minutes > 0 ? minutes + " min a day" : "off") + "\n" + apply(c);
    }

    static String setAllowed(Context c, String csv) {
        if (isFinal(c)) return "Refused: final lock is on.";
        java.util.Set<String> set = new java.util.HashSet<>();
        if (csv != null) for (String p : csv.split(",")) if (!p.trim().isEmpty()) set.add(p.trim());
        prefs(c).edit().putStringSet("allow", set).commit();
        DevicePolicyManager dpm = dpm(c);
        if (dpm.isDeviceOwnerApp(c.getPackageName())) {
            for (String p : set) { try { dpm.setApplicationHidden(admin(c), p, false); } catch (Exception ignored) { } }
        }
        return "Allowed: " + set + "\n" + apply(c);
    }

    static String apply(Context c) {
        DevicePolicyManager dpm = dpm(c);
        if (!dpm.isDeviceOwnerApp(c.getPackageName())) {
            return "Not active: FocusLock isn't the Device Owner yet.";
        }
        ComponentName a = admin(c);
        StringBuilder out = new StringBuilder();

        for (String r : RESTRICTIONS) {
            try { dpm.addUserRestriction(a, r); } catch (Exception e) { out.append("! ").append(r).append(": ").append(e.getMessage()).append('\n'); }
        }
        if (isFinal(c)) dpm.addUserRestriction(a, UserManager.DISALLOW_DEBUGGING_FEATURES);

        // Android checks the host over the network before accepting it, and
        // that can't run on the main thread (it fails as "host not serving").
        final int[] dnsBox = { -1 };
        Thread t = new Thread(() -> {
            try { dnsBox[0] = dpm.setGlobalPrivateDnsModeSpecifiedHost(a, DNS_HOST); }
            catch (Exception e) { android.util.Log.w(TAG, "private DNS", e); }
        });
        t.start();
        try { t.join(8000); } catch (InterruptedException ignored) { }
        int dns = dnsBox[0];

        int hidden = 0;
        java.util.Set<String> allow = allowed(c);
        for (String p : HIDDEN) {
            if (allow.contains(p)) continue;
            try { if (dpm.setApplicationHidden(a, p, true) || dpm.isApplicationHidden(a, p)) hidden++; } catch (Exception ignored) { }
        }

        dpm.setApplicationRestrictions(a, CHROME, chromePolicy(c));
        try { dpm.setUninstallBlocked(a, CHROME, true); } catch (Exception ignored) { }

        UsageWatcher.start(c);
        int ytLimit = youtubeLimitMinutes(c);
        out.append("Active").append(isFinal(c) ? " (final lock)" : " (test mode: removable over USB)").append('\n')
           .append("• Installing apps: blocked\n")
           .append("• Factory reset, safe mode, new users, VPN: blocked\n")
           .append("• Private DNS: ").append(DNS_HOST).append(dns == 0 ? "" : " (status " + dns + ")").append('\n')
           .append("• Hidden apps: ").append(hidden).append('\n')
           .append(allow.isEmpty() ? "" : "• Allowed on this phone: " + allow + "\n")
           .append(ytLimit > 0 ? "• YouTube limit: " + ytLimit + " min a day" + (youtubeOver(c) ? " (used up today)" : "") + "\n" : "")
           .append(shopLimitMinutes(c) > 0 ? "• Shopping limit: " + shopLimitMinutes(c) + " min a day, apps only\n" : "")
           .append(UsageWatcher.canCount(c) || (ytLimit <= 0 && shopLimitMinutes(c) <= 0) ? "" : "! Usage access is off, so limited apps stay blocked\n")
           .append("• Chrome: no Incognito, SafeSearch, blocked sites");
        return out.toString();
    }

    static Bundle chromePolicy(Context c) {
        Bundle b = new Bundle();
        b.putInt("IncognitoModeAvailability", 1);
        b.putString("DnsOverHttpsMode", "off");
        b.putBoolean("ForceGoogleSafeSearch", true);
        b.putInt("ForceYouTubeRestrict", 2);   // strict Restricted Mode where YouTube is allowed
        b.putBoolean("NTPContentSuggestionsEnabled", false);
        b.putStringArray("URLBlocklist", blockedDomains(c));
        return b;
    }

    // The same list as the laptop lockdown (lockdown/blocklist.txt, packed as an asset).
    static String[] blockedDomains(Context c) {
        List<String> out = new ArrayList<>();
        try (BufferedReader r = new BufferedReader(new InputStreamReader(c.getAssets().open("blocklist.txt")))) {
            String line;
            while ((line = r.readLine()) != null) {
                line = line.trim();
                if (line.isEmpty() || line.startsWith("#")) continue;
                if (youtubeAllowed(c) && !youtubeOver(c) && (line.contains("youtube") || line.equals("youtu.be")
                        || line.equals("ytimg.com") || line.equals("googlevideo.com"))) continue;
                out.add(line);
            }
        } catch (Exception ignored) { }
        if (shopLimitMinutes(c) > 0) out.addAll(java.util.Arrays.asList(SHOPPING_SITES));
        return out.toArray(new String[0]);
    }

    // Test mode only: undo everything so the app can be removed over USB.
    static String release(Context c) {
        if (isFinal(c)) return "Refused: final lock is on.";
        DevicePolicyManager dpm = dpm(c);
        if (!dpm.isDeviceOwnerApp(c.getPackageName())) return "Not the Device Owner.";
        ComponentName a = admin(c);
        for (String r : RESTRICTIONS) { try { dpm.clearUserRestriction(a, r); } catch (Exception ignored) { } }
        for (String p : HIDDEN) { try { dpm.setApplicationHidden(a, p, false); } catch (Exception ignored) { } }
        try { dpm.setPackagesSuspended(a, new String[] { UsageWatcher.YT }, false); } catch (Exception ignored) { }
        try { dpm.setPackagesSuspended(a, SHOPPING, false); } catch (Exception ignored) { }
        dpm.setApplicationRestrictions(a, CHROME, new Bundle());
        try { dpm.setUninstallBlocked(a, CHROME, false); } catch (Exception ignored) { }
        try { dpm.setGlobalPrivateDnsModeOpportunistic(a); } catch (Exception ignored) { }
        return "Released. Now: adb shell dpm remove-active-admin dev.focuslock.phone/.AdminReceiver";
    }

    // One way: turns off USB debugging and developer options for good.
    static String finalLock(Context c) {
        prefs(c).edit().putBoolean("final", true).commit();
        return apply(c);
    }
}
