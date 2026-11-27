package io.github.scotylucas.kinora;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.webkit.CookieManager;

import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Locale;

/**
 * The server's API for the parts of the app that run without the web client (widgets, notifications,
 * Watch Next). They use the web client's own session: the WebView's kinora_session cookie for the
 * server that was open last, sent as a Bearer token. Nothing is stored here; logging out in the web
 * client ends it for these too.
 */
final class Api {
    final String origin;
    private final String token;

    private Api(String origin, String token) {
        this.origin = origin;
        this.token = token;
    }

    /** null when no server was opened yet or the session is gone. */
    static Api open(Context ctx) {
        String origin = Prefs.origin(ctx);
        if (origin == null) return null;
        String cookies;
        try {
            cookies = CookieManager.getInstance().getCookie(origin);
        } catch (Exception e) { // no WebView on this device
            return null;
        }
        if (cookies == null) return null;
        for (String c : cookies.split(";")) {
            String[] kv = c.trim().split("=", 2);
            if (kv.length == 2 && "kinora_session".equals(kv[0]) && !kv[1].isEmpty()) return new Api(origin, kv[1]);
        }
        return null;
    }

    static final class HttpError extends IOException {
        final int status;

        HttpError(int status) {
            super("HTTP " + status);
            this.status = status;
        }
    }

    private HttpURLConnection connect(String path) throws IOException {
        HttpURLConnection c = (HttpURLConnection) new URL(origin + path).openConnection();
        c.setConnectTimeout(10000);
        c.setReadTimeout(20000);
        c.setRequestProperty("Authorization", "Bearer " + token);
        c.setRequestProperty("X-Kinora-Lang", Locale.getDefault().getLanguage());
        return c;
    }

    private static byte[] read(HttpURLConnection c, int max) throws IOException {
        int code = c.getResponseCode();
        if (code < 200 || code >= 300) {
            c.disconnect();
            throw new HttpError(code);
        }
        try (InputStream in = c.getInputStream()) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[16384];
            int n;
            while ((n = in.read(buf)) > 0) {
                out.write(buf, 0, n);
                if (out.size() > max) throw new IOException("response too large");
            }
            return out.toByteArray();
        } finally {
            c.disconnect();
        }
    }

    JSONObject get(String path) throws Exception {
        return new JSONObject(new String(read(connect(path), 8 << 20), StandardCharsets.UTF_8));
    }

    /** An image of this server, scaled down to about maxPx on its longer side; null when there is none. */
    Bitmap image(String path, int maxPx) {
        try {
            byte[] b = read(connect(path), 12 << 20);
            BitmapFactory.Options o = new BitmapFactory.Options();
            o.inJustDecodeBounds = true;
            BitmapFactory.decodeByteArray(b, 0, b.length, o);
            int sample = 1;
            while (Math.max(o.outWidth, o.outHeight) / (sample * 2) >= maxPx) sample *= 2;
            o = new BitmapFactory.Options();
            o.inSampleSize = sample;
            return BitmapFactory.decodeByteArray(b, 0, b.length, o);
        } catch (Exception e) {
            return null;
        }
    }

    /** The address of an image the TV launcher can load on its own: artwork-only stream token (/api/me media_token). */
    static String imagePath(JSONObject item, String kind) {
        return "/api/images/" + item.optLong("id") + "/" + kind + "?v=" + item.optLong("updated_at");
    }
}
