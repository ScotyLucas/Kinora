package io.github.scotylucas.kinora;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * What the native parts (widgets, notifications, Android TV's Watch Next row) need to know between starts:
 * the server whose web client was open last, and whether this device fetches its notifications.
 * The address comes from the WebView's own page, never from what a page says about itself.
 */
final class Prefs {
    private static final String FILE = "kinora";
    static final String ORIGIN = "origin";
    static final String PUSH = "push";
    static final String LINK = "link";

    private Prefs() {}

    static SharedPreferences of(Context ctx) {
        return ctx.getApplicationContext().getSharedPreferences(FILE, Context.MODE_PRIVATE);
    }

    /** scheme://host[:port] of a server page, or null for the launcher and anything that is not http(s). */
    static String originOf(String url) {
        return Links.originOf(url);
    }

    static String origin(Context ctx) {
        return of(ctx).getString(ORIGIN, null);
    }

    /** Remembers the server of the page that is open; a new server turns the old one's notifications off here. */
    static void setOrigin(Context ctx, String origin) {
        if (origin == null || origin.equals(origin(ctx))) return;
        of(ctx).edit().putString(ORIGIN, origin).putBoolean(PUSH, false).apply();
    }

    static boolean push(Context ctx) {
        return of(ctx).getBoolean(PUSH, false);
    }

    static void setPush(Context ctx, boolean on) {
        of(ctx).edit().putBoolean(PUSH, on).apply();
    }
}
