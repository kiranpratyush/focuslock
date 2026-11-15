package dev.focuslock.phone;

import android.app.Activity;
import android.graphics.Color;
import android.os.Bundle;
import android.util.TypedValue;
import android.widget.ScrollView;
import android.widget.TextView;

public class MainActivity extends Activity {
    @Override
    protected void onResume() {
        super.onResume();
        TextView text = new TextView(this);
        text.setTextColor(Color.rgb(230, 230, 230));
        text.setBackgroundColor(Color.rgb(11, 12, 22));
        text.setTextSize(TypedValue.COMPLEX_UNIT_SP, 16);
        int pad = (int) (24 * getResources().getDisplayMetrics().density);
        text.setPadding(pad, pad * 2, pad, pad);
        text.setText("FocusLock\n\n" + Policy.apply(this)
                + "\n\nThe phone is for calls, study, maps and messages."
                + "\nGo outside. Read a book. You used to be that person.");
        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(Color.rgb(11, 12, 22));
        scroll.addView(text);
        setContentView(scroll);
    }
}
