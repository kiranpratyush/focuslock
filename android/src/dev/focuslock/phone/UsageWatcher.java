package dev.focuslock.phone;

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

import java.util.Calendar;

// Daily time limit for an allowed app (YouTube on the family phone): adds up
// today's foreground time every 30 s; at the limit the app is suspended and
// its site is blocked in Chrome until midnight.
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
        if (Policy.youtubeLimitMinutes(c) <= 0) return;
        try { c.startForegroundService(new Intent(c, UsageWatcher.class)); }
        catch (Exception e) { Log.w(Policy.TAG, "can't start the limit watcher", e); }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        NotificationManager nm = getSystemService(NotificationManager.class);
        nm.createNotificationChannel(new NotificationChannel(CHANNEL, "YouTube limit", NotificationManager.IMPORTANCE_MIN));
        startForeground(1, notification(this, "YouTube limit is on"));
        handler.removeCallbacks(tick);
        handler.post(tick);
        return START_STICKY;
    }

    @Override public IBinder onBind(Intent intent) { return null; }

    static Notification notification(Context c, String text) {
        return new Notification.Builder(c, CHANNEL)
                .setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
                .setContentTitle("FocusLock")
                .setContentText(text)
                .setOngoing(true)
                .build();
    }

    static void check(Context c) {
        int limit = Policy.youtubeLimitMinutes(c);
        if (limit <= 0) return;
        long used = usedTodayMs(c, YT);
        boolean over = used >= limit * 60_000L;
        DevicePolicyManager dpm = Policy.dpm(c);
        if (dpm.isDeviceOwnerApp(c.getPackageName())) {
            try { dpm.setPackagesSuspended(Policy.admin(c), new String[] { YT }, over); } catch (Exception ignored) { }
            if (over != Policy.youtubeOver(c)) {
                Policy.prefs(c).edit().putBoolean("yt_over", over).commit();
                dpm.setApplicationRestrictions(Policy.admin(c), Policy.CHROME, Policy.chromePolicy(c));
                Log.i(Policy.TAG, over ? "YouTube limit reached" : "YouTube limit reset");
            }
        }
        String text = over ? "YouTube is done for today. Back tomorrow."
                : "YouTube today: " + fmt(used) + " of " + fmt(limit * 60_000L);
        c.getSystemService(NotificationManager.class).notify(1, notification(c, text));
    }

    static long midnight() {
        Calendar cal = Calendar.getInstance();
        cal.set(Calendar.HOUR_OF_DAY, 0); cal.set(Calendar.MINUTE, 0);
        cal.set(Calendar.SECOND, 0); cal.set(Calendar.MILLISECOND, 0);
        return cal.getTimeInMillis();
    }

    // Foreground time since midnight, from the app's resume/pause events.
    static long usedTodayMs(Context c, String pkg) {
        UsageStatsManager usm = c.getSystemService(UsageStatsManager.class);
        long now = System.currentTimeMillis(), total = 0, since = -1;
        UsageEvents events = usm.queryEvents(midnight(), now);
        UsageEvents.Event e = new UsageEvents.Event();
        while (events.hasNextEvent()) {
            events.getNextEvent(e);
            if (!pkg.equals(e.getPackageName())) continue;
            int t = e.getEventType();
            if (t == UsageEvents.Event.ACTIVITY_RESUMED) {
                if (since < 0) since = e.getTimeStamp();
            } else if (t == UsageEvents.Event.ACTIVITY_PAUSED || t == UsageEvents.Event.ACTIVITY_STOPPED) {
                if (since >= 0) { total += e.getTimeStamp() - since; since = -1; }
            }
        }
        if (since >= 0) total += now - since;
        return total;
    }

    static String fmt(long ms) {
        long m = ms / 60_000;
        return (m / 60) + "h " + (m % 60) + "m";
    }
}
