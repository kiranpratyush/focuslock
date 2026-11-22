package dev.focuslock.phone;

import android.app.AppOpsManager;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.app.admin.DevicePolicyManager;
import android.app.usage.UsageEvents;
import android.app.usage.UsageStatsManager;
import android.content.Context;
import android.content.Intent;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.util.Log;

import java.util.Arrays;
import java.util.Calendar;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

// Daily time limits: YouTube (where it's allowed) and the shopping apps, which
// share one limit. Adds up today's foreground time every 30 s; at the limit
// the apps are suspended until midnight, and YouTube's site is blocked too.
public class UsageWatcher extends Service {
    static final String YT = "com.google.android.youtube";
    static final String CHANNEL = "limit";
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final Runnable tick = new Runnable() {
        @Override public void run() {
            check(UsageWatcher.this);
            handler.postDelayed(this, 30_000);
        }
    };

    static void start(Context c) {
        if (Policy.youtubeLimitMinutes(c) <= 0 && Policy.shopLimitMinutes(c) <= 0) return;
        try { c.startForegroundService(new Intent(c, UsageWatcher.class)); }
        catch (Exception e) { Log.w(Policy.TAG, "can't start the limit watcher", e); }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        NotificationManager nm = getSystemService(NotificationManager.class);
        nm.createNotificationChannel(new NotificationChannel(CHANNEL, "Daily limits", NotificationManager.IMPORTANCE_MIN));
        startForeground(1, notification(this, "Daily limits are on"));
        handler.removeCallbacks(tick);
        handler.post(tick);
        return START_STICKY;
    }

    @Override public IBinder onBind(Intent intent) { return null; }

    static Notification notification(Context c, String text) {
        return new Notification.Builder(c, CHANNEL)
                .setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
                .setContentTitle("FocusLock")
                .setContentText(text.replace("\n", " · "))
                .setStyle(new Notification.BigTextStyle().bigText(text))
                .setOngoing(true)
                .build();
    }

    static void check(Context c) {
        int yt = Policy.youtubeLimitMinutes(c), shop = Policy.shopLimitMinutes(c);
        DevicePolicyManager dpm = Policy.dpm(c);
        boolean owner = dpm.isDeviceOwnerApp(c.getPackageName());
        boolean counting = canCount(c);
        // A limit that was turned off must not leave its apps suspended.
        if (owner && yt <= 0) suspend(c, new String[] { YT }, false);
        if (owner && shop <= 0) suspend(c, Policy.SHOPPING, false);
        if (yt <= 0 && shop <= 0) return;
        StringBuilder text = new StringBuilder();
        if (yt > 0) {
            long used = usedTodayMs(c, YT);
            // Without usage access nothing can be counted: closed, not unlimited.
            boolean over = !counting || used >= yt * 60_000L;
            if (owner) {
                suspend(c, new String[] { YT }, over);
                if (over != Policy.youtubeOver(c)) {
                    Policy.prefs(c).edit().putBoolean("yt_over", over).commit();
                    dpm.setApplicationRestrictions(Policy.admin(c), Policy.CHROME, Policy.chromePolicy(c));
                    Log.i(Policy.TAG, over ? "YouTube limit reached" : "YouTube limit reset");
                }
            }
            text.append(line("YouTube", used, yt, over, counting));
        }
        if (shop > 0) {
            long used = usedTodayMs(c, Policy.SHOPPING);
            boolean over = !counting || used >= shop * 60_000L;
            if (owner) suspend(c, Policy.SHOPPING, over);
            if (text.length() > 0) text.append('\n');
            text.append(line("Shopping", used, shop, over, counting));
        }
        c.getSystemService(NotificationManager.class).notify(1, notification(c, text.toString()));
    }

    static void suspend(Context c, String[] packages, boolean on) {
        for (String p : packages) {   // one by one: a missing app mustn't stop the rest
            try { Policy.dpm(c).setPackagesSuspended(Policy.admin(c), new String[] { p }, on); } catch (Exception ignored) { }
        }
    }

    static String line(String name, long used, int limit, boolean over, boolean counting) {
        if (!counting) return name + " is blocked: FocusLock has no usage access.";
        return over ? name + " is done for today. Back tomorrow."
                : name + " today: " + fmt(used) + " of " + fmt(limit * 60_000L);
    }

    // Usage access can be switched off in Settings.
    static boolean canCount(Context c) {
        AppOpsManager ops = c.getSystemService(AppOpsManager.class);
        return ops.unsafeCheckOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, android.os.Process.myUid(),
                c.getPackageName()) == AppOpsManager.MODE_ALLOWED;
    }

    static long midnight() {
        Calendar cal = Calendar.getInstance();
        cal.set(Calendar.HOUR_OF_DAY, 0); cal.set(Calendar.MINUTE, 0);
        cal.set(Calendar.SECOND, 0); cal.set(Calendar.MILLISECOND, 0);
        return cal.getTimeInMillis();
    }

    // Time since midnight with any of these apps in front. Moving between two
    // screens of an app gives "A paused, B resumed, A stopped", so the open
    // screens are counted and the late "stopped" is ignored.
    static long usedTodayMs(Context c, String... pkgs) {
        Set<String> wanted = new HashSet<>(Arrays.asList(pkgs));
        UsageStatsManager usm = c.getSystemService(UsageStatsManager.class);
        long now = System.currentTimeMillis(), total = 0, since = -1;
        Map<String, Integer> open = new HashMap<>();
        UsageEvents events = usm.queryEvents(midnight(), now);
        if (events == null) return 0;
        UsageEvents.Event e = new UsageEvents.Event();
        while (events.hasNextEvent()) {
            events.getNextEvent(e);
            int t = e.getEventType();
            if (t == UsageEvents.Event.SCREEN_NON_INTERACTIVE || t == UsageEvents.Event.DEVICE_SHUTDOWN
                    || t == UsageEvents.Event.DEVICE_STARTUP) {
                open.clear();   // also ends a screen whose app crashed
            } else if (wanted.contains(e.getPackageName())) {
                String screen = e.getPackageName() + "/" + e.getClassName();
                int n = open.containsKey(screen) ? open.get(screen) : 0;
                if (t == UsageEvents.Event.ACTIVITY_RESUMED) open.put(screen, n + 1);
                else if (t == UsageEvents.Event.ACTIVITY_PAUSED) { if (n > 1) open.put(screen, n - 1); else open.remove(screen); }
                else continue;
            } else continue;
            if (!open.isEmpty() && since < 0) since = e.getTimeStamp();
            if (open.isEmpty() && since >= 0) { total += e.getTimeStamp() - since; since = -1; }
        }
        if (since >= 0) total += now - since;
        return total;
    }

    static String fmt(long ms) {
        long m = ms / 60_000;
        return (m / 60) + "h " + (m % 60) + "m";
    }
}
