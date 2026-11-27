package io.github.scotylucas.kinora;

import android.app.Activity;
import android.app.PictureInPictureParams;
import android.content.Context;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.util.Rational;
import android.view.WindowManager;

import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import org.json.JSONObject;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.SocketTimeoutException;
import java.nio.charset.StandardCharsets;
import java.util.HashSet;
import java.util.Set;

/**
 * Native helpers for the Kinora web UI: Android TV detection, LAN discovery over
 * UDP broadcast (port 7359, "who is KinoraServer?"), picture-in-picture and
 * keeping the screen on during playback.
 */
@CapacitorPlugin(name = "KinoraNative")
public class KinoraNativePlugin extends Plugin {

    /** Runs before the first page loads, so the launcher already sees window.KinoraAndroid. */
    @Override
    public void load() {
        getBridge().getWebView().addJavascriptInterface(new KinoraAndroid(getActivity(), getBridge().getWebView()), "KinoraAndroid");
        // offline downloads; what each page may do is decided by its address (KinoraOffline)
        getBridge().getWebView().addJavascriptInterface(new KinoraOffline(getActivity(), getBridge().getWebView()), "KinoraOffline");
    }

    /**
     * The launcher opens the chosen server's web client inside the app. From the launcher any
     * http(s) address may load in the WebView (it is the server the user picked); on a server's
     * pages only that server's own; other links (IMDb, GitHub…) open in the browser as before.
     */
    @Override
    public Boolean shouldOverrideLoad(Uri url) {
        String scheme = url.getScheme();
        String target = url.getHost();
        if (target == null || !("http".equals(scheme) || "https".equals(scheme)) || "localhost".equals(target)) return null;
        String cur = getBridge().getWebView().getUrl();
        String curHost = cur == null ? null : Uri.parse(cur).getHost();
        if (curHost == null || "localhost".equals(curHost) || target.equalsIgnoreCase(curHost)) return false;
        return null;
    }

    @PluginMethod
    public void info(PluginCall call) {
        boolean tv = KinoraAndroid.isTv(getContext());
        JSObject r = new JSObject();
        r.put("tv", tv);
        r.put("model", Build.MANUFACTURER + " " + Build.MODEL);
        call.resolve(r);
    }

    @PluginMethod
    public void discover(PluginCall call) {
        final int timeout = call.getInt("timeoutMs", 2500);
        final int port = call.getInt("port", 7359);
        getBridge().execute(() -> {
            WifiManager wifi = (WifiManager) getContext().getApplicationContext().getSystemService(Context.WIFI_SERVICE);
            WifiManager.MulticastLock lock = null;
            JSArray servers = new JSArray();
            Set<String> seen = new HashSet<>();
            try {
                if (wifi != null) {
                    lock = wifi.createMulticastLock("kinora-discovery");
                    lock.setReferenceCounted(false);
                    lock.acquire();
                }
                DatagramSocket sock = new DatagramSocket();
                sock.setBroadcast(true);
                sock.setSoTimeout(400);
                byte[] q = "who is KinoraServer?".getBytes(StandardCharsets.UTF_8);
                sock.send(new DatagramPacket(q, q.length, InetAddress.getByName("255.255.255.255"), port));
                long end = System.currentTimeMillis() + timeout;
                byte[] buf = new byte[2048];
                while (System.currentTimeMillis() < end) {
                    DatagramPacket p = new DatagramPacket(buf, buf.length);
                    try {
                        sock.receive(p);
                    } catch (SocketTimeoutException e) {
                        continue;
                    }
                    try {
                        JSONObject j = new JSONObject(new String(p.getData(), 0, p.getLength(), StandardCharsets.UTF_8));
                        if (!"kinora".equals(j.optString("product"))) continue;
                        String addr = j.optString("address");
                        if (addr.isEmpty()) addr = "http://" + p.getAddress().getHostAddress() + ":8096";
                        String id = j.optString("id", addr);
                        if (!seen.add(id)) continue;
                        JSObject s = new JSObject();
                        s.put("id", id);
                        s.put("name", j.optString("name", "Kinora"));
                        s.put("address", addr);
                        s.put("version", j.optString("version"));
                        s.put("public_url", j.optString("public_url"));
                        servers.put(s);
                    } catch (Exception ignored) {
                        // not a Kinora reply
                    }
                }
                sock.close();
            } catch (Exception e) {
                // no network: return what we have
            } finally {
                if (lock != null && lock.isHeld()) lock.release();
            }
            JSObject r = new JSObject();
            r.put("servers", servers);
            call.resolve(r);
        });
    }

    @PluginMethod
    public void enterPip(PluginCall call) {
        Activity a = getActivity();
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O
                || !a.getPackageManager().hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)) {
            call.reject("A kép a képben mód nem támogatott ezen az eszközön.");
            return;
        }
        a.runOnUiThread(() -> {
            try {
                PictureInPictureParams params = new PictureInPictureParams.Builder()
                        .setAspectRatio(new Rational(16, 9)).build();
                a.enterPictureInPictureMode(params);
                call.resolve();
            } catch (Exception e) {
                call.reject(e.getMessage());
            }
        });
    }

    @PluginMethod
    public void keepAwake(PluginCall call) {
        final boolean on = call.getBoolean("on", false);
        Activity a = getActivity();
        a.runOnUiThread(() -> {
            if (on) a.getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            else a.getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            call.resolve();
        });
    }
}
