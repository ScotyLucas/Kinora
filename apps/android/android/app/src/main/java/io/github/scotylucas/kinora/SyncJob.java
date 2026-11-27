package io.github.scotylucas.kinora;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.job.JobInfo;
import android.app.job.JobParameters;
import android.app.job.JobScheduler;
import android.app.job.JobService;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.graphics.Bitmap;
import android.net.Uri;
import android.os.Build;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * The app's background work, every 15 minutes with a network (and right after the app goes to the
 * background): the notifications the server kept for this device (GET /api/push/android/inbox, so no
 * Google push service is needed), the home screen widgets, and Android TV's Watch Next row.
 * It talks to the server of the web client that was open last, with that page's own session.
 */
public class SyncJob extends JobService {
    private static final int PERIODIC = 1;
    private static final int ONCE = 2;
    private static final String CHANNEL = "notifications";

    /** Turns the periodic run on when there is something to keep up to date, and off when there is not. */
    static void schedule(Context ctx) {
        JobScheduler js = ctx.getSystemService(JobScheduler.class);
        if (js == null) return;
        boolean wanted = Prefs.origin(ctx) != null && (Prefs.push(ctx) || Widgets.any(ctx) || KinoraAndroid.isTv(ctx));
        if (!wanted) {
            js.cancel(PERIODIC);
            return;
        }
        if (js.getPendingJob(PERIODIC) != null) return;
        js.schedule(new JobInfo.Builder(PERIODIC, new ComponentName(ctx, SyncJob.class))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPeriodic(15 * 60 * 1000L)
                .setPersisted(true)
                .build());
    }

    /** One run as soon as there is a network. */
    static void now(Context ctx) {
        if (Prefs.origin(ctx) == null) return;
        JobScheduler js = ctx.getSystemService(JobScheduler.class);
        if (js == null) return;
        try {
            js.schedule(new JobInfo.Builder(ONCE, new ComponentName(ctx, SyncJob.class))
                    .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                    .setOverrideDeadline(60 * 1000L)
                    .build());
        } catch (Exception e) {
            // too many jobs: the periodic one comes anyway
        }
    }

    private volatile Thread worker;

    @Override
    public boolean onStartJob(JobParameters params) {
        worker = new Thread(() -> {
            try {
                run(getApplicationContext());
            } finally {
                jobFinished(params, false);
            }
        });
        worker.start();
        return true;
    }

    @Override
    public boolean onStopJob(JobParameters params) {
        Thread t = worker;
        if (t != null) t.interrupt();
        return false; // the next run catches up
    }

    static void run(Context ctx) {
        Api api = Api.open(ctx);
        if (api == null) {
            Widgets.signedOut(ctx);
            return;
        }
        if (Prefs.push(ctx)) {
            try {
                inbox(ctx, api);
            } catch (Api.HttpError e) {
                if (e.status == 404 || e.status == 401) Prefs.setPush(ctx, false); // unpaired, or signed out
            } catch (Exception e) {
                // next time
            }
        }
        boolean tv = KinoraAndroid.isTv(ctx);
        if (!Widgets.any(ctx) && !tv) return;
        try {
            JSONObject home = api.get("/api/home");
            if (Widgets.any(ctx)) Widgets.render(ctx, api, home);
            if (tv && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) WatchNext.sync(ctx, api, home);
        } catch (Api.HttpError e) {
            if (e.status == 401 || e.status == 403) Widgets.signedOut(ctx);
        } catch (Exception e) {
            // offline: what the widgets show stays
        }
    }

    // ------------------------------------------------------------------ notifications

    private static void inbox(Context ctx, Api api) throws Exception {
        JSONArray msgs = api.get("/api/push/android/inbox").optJSONArray("messages");
        if (msgs == null || msgs.length() == 0) return;
        NotificationManager nm = ctx.getSystemService(NotificationManager.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            nm.createNotificationChannel(new NotificationChannel(CHANNEL, ctx.getString(R.string.channel_notifications), NotificationManager.IMPORTANCE_DEFAULT));
        }
        for (int i = 0; i < msgs.length(); i++) {
            JSONObject m = msgs.optJSONObject(i);
            if (m == null || m.optString("title").isEmpty()) continue;
            Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O ? new Notification.Builder(ctx, CHANNEL) : new Notification.Builder(ctx);
            b.setSmallIcon(R.drawable.ic_stat_kinora)
                    .setContentTitle(m.optString("title"))
                    .setContentText(m.optString("body"))
                    .setStyle(new Notification.BigTextStyle().bigText(m.optString("body")))
                    .setAutoCancel(true)
                    .setContentIntent(open(ctx, link(m), i));
            String img = m.optString("image");
            if (img.startsWith("/")) {
                Bitmap bm = api.image(img, 256);
                if (bm != null) b.setLargeIcon(bm);
            }
            String tag = m.optString("tag");
            nm.notify(tag.isEmpty() ? null : tag, tag.isEmpty() ? (int) (System.currentTimeMillis() / 1000 + i) : 0, b.build());
        }
    }

    static Uri link(JSONObject m) {
        return Uri.parse(Links.ofNotification(m.optString("href"), m.optLong("item")));
    }

    static PendingIntent open(Context ctx, Uri link, int code) {
        Intent i = new Intent(Intent.ACTION_VIEW, link, ctx, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        return PendingIntent.getActivity(ctx, code, i, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    }
}
