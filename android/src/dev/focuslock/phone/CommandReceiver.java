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
            result = Policy.setYoutubeLimit(context, intent.getIntExtra("youtube_minutes", 0));
        } else if (action.endsWith(".FINAL_LOCK")) {
            result = Policy.finalLock(context);
        } else {
            result = Policy.apply(context);
        }
        Log.i(Policy.TAG, action + ": " + result);
        setResultData(result);
    }
}
