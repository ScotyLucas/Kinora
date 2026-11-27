package io.github.scotylucas.kinora;

import android.app.Activity;
import android.content.Context;
import android.net.Uri;
import android.view.ContextThemeWrapper;

import androidx.annotation.NonNull;
import androidx.mediarouter.app.MediaRouteChooserDialog;
import androidx.mediarouter.app.MediaRouteControllerDialog;

import com.google.android.gms.cast.CastMediaControlIntent;
import com.google.android.gms.cast.MediaInfo;
import com.google.android.gms.cast.MediaLoadRequestData;
import com.google.android.gms.cast.MediaMetadata;
import com.google.android.gms.cast.MediaSeekOptions;
import com.google.android.gms.cast.MediaStatus;
import com.google.android.gms.cast.framework.CastContext;
import com.google.android.gms.cast.framework.CastOptions;
import com.google.android.gms.cast.framework.CastSession;
import com.google.android.gms.cast.framework.OptionsProvider;
import com.google.android.gms.cast.framework.SessionManagerListener;
import com.google.android.gms.cast.framework.SessionProvider;
import com.google.android.gms.cast.framework.media.CastMediaOptions;
import com.google.android.gms.cast.framework.media.NotificationOptions;
import com.google.android.gms.cast.framework.media.RemoteMediaClient;
import com.google.android.gms.common.images.WebImage;

import org.json.JSONObject;

import java.util.List;

/**
 * Chromecast for the web client's player (the AirPlay of the iOS app). The page asks for a stream the
 * receiver can play (H.264/AAC, MP4 or HLS), and hands its address here; the address carries a stream
 * token, so the Chromecast fetches it from the server on its own. The Default Media Receiver plays it,
 * the Cast SDK shows its own notification and lock screen controls meanwhile.
 *
 * Events reach the page as kinoraCast({event: 'connected' | 'disconnected', device}); the state is read
 * with state() (polled by the page while it casts).
 */
final class KinoraCast {
    /** The Cast SDK's options (manifest: com.google.android.gms.cast.framework.OPTIONS_PROVIDER_CLASS_NAME). */
    public static final class Options implements OptionsProvider {
        @NonNull
        @Override
        public CastOptions getCastOptions(@NonNull Context ctx) {
            NotificationOptions n = new NotificationOptions.Builder().setTargetActivityClassName(MainActivity.class.getName()).build();
            CastMediaOptions media = new CastMediaOptions.Builder().setNotificationOptions(n).setExpandedControllerActivityClassName(null).build();
            return new CastOptions.Builder()
                    .setReceiverApplicationId(CastMediaControlIntent.DEFAULT_MEDIA_RECEIVER_APPLICATION_ID)
                    .setCastMediaOptions(media)
                    .setStopReceiverApplicationWhenEndingSession(true)
                    .build();
        }

        @Override
        public List<SessionProvider> getAdditionalSessionProviders(@NonNull Context ctx) {
            return null;
        }
    }

    private final Activity activity;
    private CastContext cast; // null: no Google Play services (or no Cast) on this device
    private volatile String state = "{\"connected\":false}";

    KinoraCast(Activity activity) {
        this.activity = activity;
        try {
            cast = CastContext.getSharedInstance(activity);
            cast.getSessionManager().addSessionManagerListener(listener, CastSession.class);
        } catch (Throwable t) {
            cast = null;
        }
    }

    boolean available() {
        return cast != null && !KinoraAndroid.isTv(activity);
    }

    private CastSession session() {
        return cast == null ? null : cast.getSessionManager().getCurrentCastSession();
    }

    boolean connected() {
        CastSession s = session();
        return s != null && s.isConnected();
    }

    /** The device picker, or the disconnect dialog while casting. */
    void pick() {
        if (cast == null) return;
        Context themed = new ContextThemeWrapper(activity, androidx.appcompat.R.style.Theme_AppCompat_DayNight_Dialog);
        if (connected()) {
            new MediaRouteControllerDialog(themed).show();
            return;
        }
        MediaRouteChooserDialog d = new MediaRouteChooserDialog(themed);
        d.setRouteSelector(cast.getMergedSelector());
        d.show();
    }

    /** {url, mime, title, subtitle, image, start (seconds)}: the page checked that url is its own server's. */
    void load(JSONObject in) {
        CastSession s = session();
        RemoteMediaClient c = s == null ? null : s.getRemoteMediaClient();
        if (c == null) return;
        MediaMetadata md = new MediaMetadata(MediaMetadata.MEDIA_TYPE_MOVIE);
        md.putString(MediaMetadata.KEY_TITLE, in.optString("title"));
        md.putString(MediaMetadata.KEY_SUBTITLE, in.optString("subtitle"));
        String img = in.optString("image");
        if (!img.isEmpty()) md.addImage(new WebImage(Uri.parse(img)));
        MediaInfo info = new MediaInfo.Builder(in.optString("url"))
                .setStreamType(MediaInfo.STREAM_TYPE_BUFFERED)
                .setContentType(in.optString("mime", "video/mp4"))
                .setMetadata(md)
                .build();
        c.registerCallback(progress);
        c.addProgressListener(ticks, 1000);
        c.load(new MediaLoadRequestData.Builder().setMediaInfo(info).setAutoplay(true)
                .setCurrentTime((long) (in.optDouble("start", 0) * 1000)).build());
    }

    void command(String cmd, double arg) {
        CastSession s = session();
        RemoteMediaClient c = s == null ? null : s.getRemoteMediaClient();
        if (c == null) return;
        switch (cmd) {
            case "play": c.play(); break;
            case "pause": c.pause(); break;
            case "seek": c.seek(new MediaSeekOptions.Builder().setPosition((long) (arg * 1000)).build()); break;
            case "stop": c.stop(); break;
            default: break;
        }
    }

    /** {connected, device, position, duration, playing, ended} as JSON; cheap, read by the page every second. */
    String state() {
        return state;
    }

    private void refresh() {
        CastSession s = session();
        JSONObject o = new JSONObject();
        try {
            o.put("connected", s != null && s.isConnected());
            if (s != null && s.getCastDevice() != null) o.put("device", s.getCastDevice().getFriendlyName());
            RemoteMediaClient c = s == null ? null : s.getRemoteMediaClient();
            if (c != null) {
                o.put("position", c.getApproximateStreamPosition() / 1000.0);
                o.put("duration", c.getStreamDuration() / 1000.0);
                o.put("playing", c.isPlaying() || c.isBuffering());
                o.put("buffering", c.isBuffering());
                MediaStatus ms = c.getMediaStatus();
                o.put("ended", ms != null && ms.getPlayerState() == MediaStatus.PLAYER_STATE_IDLE
                        && ms.getIdleReason() == MediaStatus.IDLE_REASON_FINISHED);
                o.put("failed", ms != null && ms.getPlayerState() == MediaStatus.PLAYER_STATE_IDLE
                        && ms.getIdleReason() == MediaStatus.IDLE_REASON_ERROR);
            }
        } catch (Exception e) {
            // keep what could be put
        }
        state = o.toString();
    }

    private final RemoteMediaClient.ProgressListener ticks = (progress, duration) -> refresh();

    private final RemoteMediaClient.Callback progress = new RemoteMediaClient.Callback() {
        @Override
        public void onStatusUpdated() {
            refresh();
        }
    };

    private void tell(String event) {
        refresh();
        CastSession s = session();
        String device = s != null && s.getCastDevice() != null ? s.getCastDevice().getFriendlyName() : "";
        try {
            MainActivity.page("kinoraCast(" + new JSONObject().put("event", event).put("device", device) + ")");
        } catch (Exception e) {
            // nothing to tell
        }
    }

    private final SessionManagerListener<CastSession> listener = new SessionManagerListener<CastSession>() {
        @Override public void onSessionStarted(@NonNull CastSession s, @NonNull String id) { tell("connected"); }
        @Override public void onSessionResumed(@NonNull CastSession s, boolean wasSuspended) { tell("connected"); }
        @Override public void onSessionEnded(@NonNull CastSession s, int error) { tell("disconnected"); }
        @Override public void onSessionSuspended(@NonNull CastSession s, int reason) { tell("disconnected"); }
        @Override public void onSessionStarting(@NonNull CastSession s) {}
        @Override public void onSessionStartFailed(@NonNull CastSession s, int error) {}
        @Override public void onSessionEnding(@NonNull CastSession s) {}
        @Override public void onSessionResuming(@NonNull CastSession s, @NonNull String id) {}
        @Override public void onSessionResumeFailed(@NonNull CastSession s, int error) {}
    };
}
