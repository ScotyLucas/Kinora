package io.github.scotylucas.kinora;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.Bitmap;
import android.media.MediaMetadata;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.os.Build;
import android.os.IBinder;
import android.os.SystemClock;

import org.json.JSONObject;

/**
 * Playback outside the app: the media notification, the lock screen and Bluetooth/headset buttons, and
 * keeping the app alive while a video plays with the screen off or another app in front.
 *
 * The player is the web client's <video>; it reports what plays through navigator.mediaSession, which
 * appshell.js forwards here (KinoraAndroid.media). The buttons go back to the page as kinoraMedia(action).
 */
public class MediaService extends Service {
    private static final String CHANNEL = "playback";
    private static final int ID = 1;
    static final String EXTRA = "state";

    private MediaSession session;
    private String artPath = null;
    private Bitmap art = null;
    private boolean foreground = false;

    /** Called from the page (KinoraAndroid.media), on any thread. state "none" ends it. */
    static void update(Context ctx, String json) {
        Intent i = new Intent(ctx, MediaService.class).putExtra(EXTRA, json);
        try {
            if (json.contains("\"playing\"") && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) ctx.startForegroundService(i);
            else ctx.startService(i);
        } catch (Exception e) {
            // not allowed from the background (Android 12+): the next update while the app is in front starts it
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        session = new MediaSession(this, "Kinora");
        session.setCallback(new MediaSession.Callback() {
            @Override public void onPlay() { MainActivity.page("kinoraMedia('play')"); }
            @Override public void onPause() { MainActivity.page("kinoraMedia('pause')"); }
            @Override public void onStop() { MainActivity.page("kinoraMedia('pause')"); }
            @Override public void onFastForward() { MainActivity.page("kinoraMedia('seekforward')"); }
            @Override public void onRewind() { MainActivity.page("kinoraMedia('seekbackward')"); }
            @Override public void onSkipToNext() { MainActivity.page("kinoraMedia('nexttrack')"); }
            @Override public void onSeekTo(long ms) { MainActivity.page("kinoraMedia('seekto'," + (ms / 1000.0) + ")"); }
        });
        Intent open = new Intent(this, MainActivity.class).setFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP);
        session.setSessionActivity(PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE));
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(CHANNEL, getString(R.string.channel_playback), NotificationManager.IMPORTANCE_LOW);
            ch.setShowBadge(false);
            getSystemService(NotificationManager.class).createNotificationChannel(ch);
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        JSONObject s;
        try {
            s = new JSONObject(intent == null ? "{}" : String.valueOf(intent.getStringExtra(EXTRA)));
        } catch (Exception e) {
            s = new JSONObject();
        }
        String state = s.optString("state", "none");
        if ("none".equals(state)) {
            stop();
            return START_NOT_STICKY;
        }
        boolean playing = "playing".equals(state);
        long actions = PlaybackState.ACTION_PLAY | PlaybackState.ACTION_PAUSE | PlaybackState.ACTION_PLAY_PAUSE
                | PlaybackState.ACTION_SEEK_TO | PlaybackState.ACTION_FAST_FORWARD | PlaybackState.ACTION_REWIND;
        if (s.optBoolean("next")) actions |= PlaybackState.ACTION_SKIP_TO_NEXT;
        long pos = (long) (s.optDouble("position", 0) * 1000);
        float rate = (float) s.optDouble("rate", 1);
        session.setPlaybackState(new PlaybackState.Builder().setActions(actions)
                .setState(playing ? PlaybackState.STATE_PLAYING : PlaybackState.STATE_PAUSED, pos, playing ? rate : 0, SystemClock.elapsedRealtime())
                .build());
        String path = s.optString("art", "");
        if (!path.equals(artPath)) {
            artPath = path;
            art = null;
            if (path.startsWith("/")) {
                final String want = path;
                new Thread(() -> {
                    Api api = Api.open(this);
                    Bitmap b = api == null ? null : api.image(want, 512);
                    if (b != null && want.equals(artPath)) {
                        art = b;
                        publish(lastState);
                    }
                }).start();
            }
        }
        lastState = s;
        session.setActive(true);
        publish(s);
        return START_NOT_STICKY;
    }

    private JSONObject lastState = new JSONObject();

    private synchronized void publish(JSONObject s) {
        if (session == null) return;
        MediaMetadata.Builder md = new MediaMetadata.Builder()
                .putString(MediaMetadata.METADATA_KEY_TITLE, s.optString("title"))
                .putString(MediaMetadata.METADATA_KEY_ARTIST, s.optString("artist"))
                .putString(MediaMetadata.METADATA_KEY_ALBUM, s.optString("album"))
                .putLong(MediaMetadata.METADATA_KEY_DURATION, (long) (s.optDouble("duration", 0) * 1000));
        if (art != null) md.putBitmap(MediaMetadata.METADATA_KEY_ART, art);
        session.setMetadata(md.build());

        boolean playing = "playing".equals(s.optString("state"));
        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O ? new Notification.Builder(this, CHANNEL) : new Notification.Builder(this);
        b.setSmallIcon(R.drawable.ic_stat_kinora)
                .setContentTitle(s.optString("title"))
                .setContentText(s.optString("artist"))
                .setLargeIcon(art)
                .setOngoing(playing)
                .setShowWhen(false)
                .setVisibility(Notification.VISIBILITY_PUBLIC)
                .setContentIntent(session.getController().getSessionActivity())
                .setStyle(new Notification.MediaStyle().setMediaSession(session.getSessionToken()).setShowActionsInCompactView(0, 1, 2));
        b.addAction(action(android.R.drawable.ic_media_rew, R.string.media_rewind, "seekbackward"));
        b.addAction(playing ? action(android.R.drawable.ic_media_pause, R.string.media_pause, "pause")
                : action(android.R.drawable.ic_media_play, R.string.media_play, "play"));
        b.addAction(action(android.R.drawable.ic_media_ff, R.string.media_forward, "seekforward"));
        Notification n = b.build();
        if (playing) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) startForeground(ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK);
            else startForeground(ID, n);
            foreground = true;
        } else {
            // paused: the notification stays and can be swiped away; Android may end the app meanwhile
            if (foreground) {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) stopForeground(STOP_FOREGROUND_DETACH);
                else stopForeground(false);
                foreground = false;
            }
            getSystemService(NotificationManager.class).notify(ID, n);
        }
    }

    private Notification.Action action(int icon, int label, String what) {
        PendingIntent pi = PendingIntent.getBroadcast(this, what.hashCode(),
                new Intent(this, MediaButtons.class).setAction(what), PendingIntent.FLAG_IMMUTABLE);
        return new Notification.Action.Builder(android.graphics.drawable.Icon.createWithResource(this, icon), getString(label), pi).build();
    }

    private void stop() {
        if (session != null) {
            session.setActive(false);
            session.release();
            session = null;
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) stopForeground(STOP_FOREGROUND_REMOVE);
        else stopForeground(true);
        getSystemService(NotificationManager.class).cancel(ID);
        stopSelf();
    }

    @Override
    public void onDestroy() {
        if (session != null) session.release();
        session = null;
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
