package io.github.scotylucas.kinora;

import android.app.Activity;
import android.app.PictureInPictureParams;
import android.content.Intent;
import android.content.res.Configuration;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.webkit.WebView;

import androidx.activity.OnBackPressedCallback;

import com.getcapacitor.BridgeActivity;

import java.lang.ref.WeakReference;

public class MainActivity extends BridgeActivity {
    // window.kinoraBack() of the launcher and of the web client (appshell.js): true = handled
    // (a dialog or the player closed, one page back), false = first screen (leave the app),
    // anything else = a page without it (e.g. the WebView's error page): go back in the history.
    private static final String ASK_PAGE =
            "(function(){try{return window.kinoraBack?!!window.kinoraBack():null}catch(e){return null}})()";

    private static WeakReference<WebView> current = new WeakReference<>(null);
    private static String pendingLink = "";

    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(KinoraNativePlugin.class);
        super.onCreate(savedInstanceState);
        current = new WeakReference<>(getBridge().getWebView());
        // added after Capacitor's own handler, so it runs first: Back (button, gesture, TV remote) asks the page
        getOnBackPressedDispatcher().addCallback(this, new OnBackPressedCallback(true) {
            @Override
            public void handleOnBackPressed() {
                WebView wv = getBridge().getWebView();
                wv.evaluateJavascript(ASK_PAGE, v -> {
                    if ("true".equals(v)) return;
                    if (!"false".equals(v) && wv.canGoBack()) wv.goBack();
                    else finish();
                });
            }
        });
        link(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        link(intent);
    }

    @Override
    public void onStop() {
        super.onStop();
        SyncJob.now(this); // what was just watched shows on the widgets and the TV's Watch Next row
    }

    /** Runs a call on the open page (window.kinoraMedia, kinoraCast, kinoraPip…), when there is one. */
    static void page(String js) {
        new Handler(Looper.getMainLooper()).post(() -> {
            WebView wv = current.get();
            if (wv != null) wv.evaluateJavascript("(function(){try{" + js + "}catch(e){}})()", null);
        });
    }

    // ------------------------------------------------------------------ kinora:// links

    private void link(Intent intent) {
        if (intent == null || !Intent.ACTION_VIEW.equals(intent.getAction())) return;
        String hash = intent.getData() == null ? null : Links.hashOf(intent.getData().toString());
        intent.setData(null);
        if (hash == null) return;
        WebView wv = getBridge().getWebView();
        String url = wv.getUrl();
        if (Links.originOf(url) != null) {
            // a server's web client is open: go there now (the router does the rest: login, profile, PIN)
            final String js = "location.hash=" + org.json.JSONObject.quote(hash);
            wv.post(() -> wv.evaluateJavascript(js, null));
        } else {
            pendingLink = hash; // the launcher opens the server first; its web client asks for it (hello)
        }
    }

    /** The link the app was opened with, once ("" when there is none). */
    static synchronized String takeLink() {
        String l = pendingLink;
        pendingLink = "";
        return l;
    }

    // ------------------------------------------------------------------ picture-in-picture

    /** Android 12+: leaving the app while a video plays shrinks it into picture-in-picture by itself. */
    static void autoPip(Activity a, boolean playing) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S || !KinoraAndroid.canPip(a)) return;
        a.runOnUiThread(() -> {
            try {
                a.setPictureInPictureParams(new PictureInPictureParams.Builder()
                        .setAspectRatio(KinoraAndroid.aspect).setAutoEnterEnabled(playing).setSeamlessResizeEnabled(true).build());
            } catch (Exception e) {
                // not in this window mode
            }
        });
    }

    /** Android 8–11: the same, when the user goes to the home screen. */
    @Override
    protected void onUserLeaveHint() {
        super.onUserLeaveHint();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && Build.VERSION.SDK_INT < Build.VERSION_CODES.S
                && KinoraAndroid.playing && KinoraAndroid.canPip(this)) {
            try {
                enterPictureInPictureMode(new PictureInPictureParams.Builder().setAspectRatio(KinoraAndroid.aspect).build());
            } catch (Exception e) {
                // not now
            }
        }
    }

    @Override
    public void onPictureInPictureModeChanged(boolean inPip, Configuration cfg) {
        super.onPictureInPictureModeChanged(inPip, cfg);
        page("window.kinoraPip&&kinoraPip(" + inPip + ")"); // the page shows only the picture meanwhile
    }
}
