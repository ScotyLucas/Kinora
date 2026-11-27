package io.github.scotylucas.kinora;

import android.Manifest;
import android.app.Activity;
import android.app.PictureInPictureParams;
import android.app.UiModeManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.os.Build;
import android.util.Rational;
import android.view.HapticFeedbackConstants;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;
import android.webkit.WebView;

import org.json.JSONObject;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * window.KinoraAndroid: what the launcher and the server's web client (app mode, web/static/appshell.js)
 * need from the phone or TV. The server's pages are remote, so they get no Capacitor bridge; this small
 * synchronous interface is all they can reach. Nothing in it reads the user's data or changes settings:
 * it vibrates, shares a text through the system sheet, shows what plays outside the app, enters
 * picture-in-picture, casts a stream of the page's own server, and turns this device's notifications on.
 * Which server the native parts (widgets, notifications) talk to is taken from the page's own address.
 */
public class KinoraAndroid {
    private final Activity activity;
    private final WebView webView;
    private final KinoraCast cast;

    /** What the page's player last reported (MediaService), for picture-in-picture when the user leaves. */
    static volatile boolean playing = false;
    static volatile Rational aspect = new Rational(16, 9);

    KinoraAndroid(Activity activity, WebView webView) {
        this.activity = activity;
        this.webView = webView;
        this.cast = new KinoraCast(activity);
    }

    static boolean isTv(Context ctx) {
        UiModeManager ui = (UiModeManager) ctx.getSystemService(Context.UI_MODE_SERVICE);
        return (ui != null && ui.getCurrentModeType() == Configuration.UI_MODE_TYPE_TELEVISION)
                || ctx.getPackageManager().hasSystemFeature(PackageManager.FEATURE_LEANBACK);
    }

    static boolean canPip(Context ctx) {
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !isTv(ctx)
                && ctx.getPackageManager().hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE);
    }

    /** The top-level page's address (read on the UI thread; the bridge calls arrive on another one). */
    private String pageUrl() {
        final String[] url = new String[1];
        final CountDownLatch done = new CountDownLatch(1);
        activity.runOnUiThread(() -> {
            url[0] = webView.getUrl();
            done.countDown();
        });
        try {
            done.await(2, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        return url[0];
    }

    /** The page's server ("" for the launcher). */
    private String pageOrigin() {
        String o = Prefs.originOf(pageUrl());
        return o == null ? "" : o;
    }

    @JavascriptInterface
    public boolean isTv() {
        return isTv(activity);
    }

    @JavascriptInterface
    public String model() {
        return Build.MANUFACTURER + " " + Build.MODEL;
    }

    /** Keeps the screen on while a video plays. */
    @JavascriptInterface
    public void keepAwake(final boolean on) {
        activity.runOnUiThread(() -> {
            if (on) activity.getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            else activity.getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        });
    }

    /** Back on the first screen: leave the app. */
    @JavascriptInterface
    public void exit() {
        activity.runOnUiThread(activity::finish);
    }

    // ------------------------------------------------------------------ the server's web client started

    /**
     * A server's web client is up: its address becomes the server of the widgets and notifications, and it
     * gets the link the app was opened with (kinora://…, from a shortcut, a widget, a notification or a QR
     * code) as a hash such as "#/play/42" ("" when there is none).
     */
    @JavascriptInterface
    public String hello() {
        String origin = pageOrigin();
        if (origin.isEmpty()) return "";
        Prefs.setOrigin(activity, origin);
        SyncJob.schedule(activity);
        return MainActivity.takeLink();
    }

    // ------------------------------------------------------------------ haptics

    /**
     * Haptic feedback for the viewer's own actions, as the iOS app has it: tick (a choice in a menu),
     * light (play/pause, seeking), medium (favourite, watched), heavy (skip, next episode, download),
     * success, warning, error. It follows the system's touch feedback setting.
     */
    @JavascriptInterface
    public void haptic(String kind) {
        final int c;
        switch (kind == null ? "" : kind) {
            case "tick": c = Build.VERSION.SDK_INT >= 27 ? HapticFeedbackConstants.TEXT_HANDLE_MOVE : HapticFeedbackConstants.CLOCK_TICK; break;
            case "medium": c = HapticFeedbackConstants.VIRTUAL_KEY; break;
            case "heavy": c = HapticFeedbackConstants.LONG_PRESS; break;
            case "success": c = Build.VERSION.SDK_INT >= 30 ? HapticFeedbackConstants.CONFIRM : HapticFeedbackConstants.VIRTUAL_KEY; break;
            case "warning":
            case "error": c = Build.VERSION.SDK_INT >= 30 ? HapticFeedbackConstants.REJECT : HapticFeedbackConstants.LONG_PRESS; break;
            default: c = HapticFeedbackConstants.KEYBOARD_TAP; break;
        }
        activity.runOnUiThread(() -> webView.performHapticFeedback(c));
    }

    // ------------------------------------------------------------------ sharing

    /** The system share sheet with a text (a watch party link, a title's address). */
    @JavascriptInterface
    public void share(String title, String text) {
        Intent send = new Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text == null ? "" : text);
        if (title != null && !title.isEmpty()) send.putExtra(Intent.EXTRA_SUBJECT, title);
        Intent chooser = Intent.createChooser(send, title);
        activity.runOnUiThread(() -> {
            try {
                activity.startActivity(chooser);
            } catch (Exception e) {
                // no app to share with
            }
        });
    }

    // ------------------------------------------------------------------ picture-in-picture

    @JavascriptInterface
    public boolean canPip() {
        return canPip(activity);
    }

    /** The player shrinks into a window over other apps; the page hears kinoraPip(true/false). */
    @JavascriptInterface
    public boolean pip() {
        if (!canPip(activity)) return false;
        activity.runOnUiThread(() -> {
            try {
                activity.enterPictureInPictureMode(new PictureInPictureParams.Builder().setAspectRatio(aspect).build());
            } catch (Exception e) {
                // not now (another window mode)
            }
        });
        return true;
    }

    // ------------------------------------------------------------------ playback outside the app

    /**
     * What the player plays, from navigator.mediaSession (appshell.js): {state: playing|paused|none, title,
     * artist, album, art (a path on the server), duration, position, rate, next, w, h}.
     */
    @JavascriptInterface
    public void media(String json) {
        if (json == null || pageOrigin().isEmpty()) return;
        try {
            JSONObject s = new JSONObject(json);
            playing = "playing".equals(s.optString("state"));
            int w = s.optInt("w"), h = s.optInt("h");
            if (w > 0 && h > 0) {
                // Android takes 1:2.39 … 2.39:1
                double r = Math.max(1 / 2.39, Math.min(2.39, (double) w / h));
                aspect = new Rational((int) Math.round(r * 1000), 1000);
            }
            if ("none".equals(s.optString("state"))) playing = false;
            MainActivity.autoPip(activity, playing);
        } catch (Exception e) {
            return;
        }
        MediaService.update(activity, json);
    }

    // ------------------------------------------------------------------ notifications, widgets

    /**
     * This device's notifications were turned on or off in the web client (the server keeps them for the
     * device, GET /api/push/android/inbox); the app fetches them in the background from then on.
     */
    @JavascriptInterface
    public void push(boolean on) {
        if (pageOrigin().isEmpty()) return;
        Prefs.setPush(activity, on);
        if (on && Build.VERSION.SDK_INT >= 33
                && activity.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            activity.runOnUiThread(() -> activity.requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 7));
        }
        SyncJob.schedule(activity);
        if (on) SyncJob.now(activity);
    }

    /** Something changed (a title was watched, a favourite was set): widgets and Watch Next catch up. */
    @JavascriptInterface
    public void refresh() {
        if (!pageOrigin().isEmpty()) SyncJob.now(activity);
    }

    // ------------------------------------------------------------------ Chromecast

    @JavascriptInterface
    public boolean castAvailable() {
        return cast.available();
    }

    @JavascriptInterface
    public boolean castConnected() {
        final boolean[] r = new boolean[1];
        final CountDownLatch done = new CountDownLatch(1);
        activity.runOnUiThread(() -> {
            r[0] = cast.connected();
            done.countDown();
        });
        try {
            done.await(2, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        return r[0];
    }

    @JavascriptInterface
    public void castPick() {
        activity.runOnUiThread(cast::pick);
    }

    /** Plays a stream on the Chromecast; only an address of the page's own server is taken. */
    @JavascriptInterface
    public boolean castLoad(String json) {
        try {
            JSONObject in = new JSONObject(json);
            String origin = pageOrigin();
            String url = in.optString("url");
            if (origin.isEmpty() || !(url.equals(origin) || url.startsWith(origin + "/"))) return false;
            String img = in.optString("image");
            if (!img.isEmpty() && !img.startsWith(origin + "/")) in.remove("image");
            activity.runOnUiThread(() -> cast.load(in));
            return true;
        } catch (Exception e) {
            return false;
        }
    }

    /** play | pause | seek (seconds into the stream) | stop */
    @JavascriptInterface
    public void castCmd(String cmd, double arg) {
        activity.runOnUiThread(() -> cast.command(cmd == null ? "" : cmd, arg));
    }

    @JavascriptInterface
    public String castState() {
        return cast.state();
    }
}
