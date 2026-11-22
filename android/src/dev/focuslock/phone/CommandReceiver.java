package dev.focuslock.phone;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;

public class CommandReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        String action = String.valueOf(intent.getAction());
        String result;
        if (action.endsWith(".RELEASE")) {
            result = Policy.release(context);
        } else if (action.endsWith(".ALLOW")) {
            result = Policy.setAllowed(context, intent.getStringExtra("packages"));
        } else if (action.endsWith(".LIMIT")) {
            // Only the limits that were named: the others stay as they are.
            result = "";
            if (intent.hasExtra("youtube_minutes"))
                result = Policy.setLimit(context, "YouTube", "yt_limit", intent.getIntExtra("youtube_minutes", 0));
            if (intent.hasExtra("shop_minutes"))
                result = Policy.setLimit(context, "Shopping", "shop_limit", intent.getIntExtra("shop_minutes", 0));
            if (result.isEmpty()) result = "Use --ei youtube_minutes N or --ei shop_minutes N";
        } else if (action.endsWith(".FINAL_LOCK")) {
            result = Policy.finalLock(context);
        } else {
            result = Policy.apply(context);
        }
        Log.i(Policy.TAG, action + ": " + result);
        setResultData(result);
    }
}
