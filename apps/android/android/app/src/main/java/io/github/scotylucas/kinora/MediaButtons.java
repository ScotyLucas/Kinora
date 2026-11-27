package io.github.scotylucas.kinora;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** The buttons of the media notification (MediaService): they reach the page's player as kinoraMedia(action). */
public class MediaButtons extends BroadcastReceiver {
    @Override
    public void onReceive(Context ctx, Intent intent) {
        String a = intent.getAction();
        if ("play".equals(a) || "pause".equals(a) || "seekforward".equals(a) || "seekbackward".equals(a)) {
            MainActivity.page("kinoraMedia('" + a + "')");
        }
    }
}
