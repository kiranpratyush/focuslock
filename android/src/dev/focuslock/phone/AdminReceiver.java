package dev.focuslock.phone;

import android.app.admin.DeviceAdminReceiver;
import android.content.Context;
import android.content.Intent;

public class AdminReceiver extends DeviceAdminReceiver {
    // The rules are applied by an APPLY broadcast, at boot, or when the app
    // is opened, not here: setup re-enables the apps it had to disable to
    // become Device Owner first, before "no changing apps" kicks in.
    @Override
    public void onEnabled(Context context, Intent intent) { }
}
