package io.github.scotylucas.kinora;

import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.ComponentName;
import android.content.Context;
import android.graphics.Bitmap;
import android.net.Uri;
import android.view.View;
import android.widget.RemoteViews;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.Calendar;

/**
 * The home screen widgets: Continue watching and New in the library (three posters each, a tap plays or
 * opens the title), and the Journal (what this month was watched). SyncJob fills them; a widget that was
 * just added asks for a run.
 */
final class Widgets {
    private Widgets() {}

    private static final int[] POSTERS = {R.id.poster1, R.id.poster2, R.id.poster3};

    public static class Continue extends AppWidgetProvider {
        @Override
        public void onUpdate(Context ctx, AppWidgetManager m, int[] ids) {
            SyncJob.schedule(ctx);
            SyncJob.now(ctx);
        }
    }

    public static class New extends Continue {}

    public static class Journal extends Continue {}

    private static int[] ids(Context ctx, Class<?> c) {
        return AppWidgetManager.getInstance(ctx).getAppWidgetIds(new ComponentName(ctx, c));
    }

    static boolean any(Context ctx) {
        return ids(ctx, Continue.class).length + ids(ctx, New.class).length + ids(ctx, Journal.class).length > 0;
    }

    /** No session (signed out, or the app was never opened): the widgets ask to open the app. */
    static void signedOut(Context ctx) {
        AppWidgetManager m = AppWidgetManager.getInstance(ctx);
        for (Class<?> c : new Class<?>[]{Continue.class, New.class}) {
            int[] ids = ids(ctx, c);
            if (ids.length == 0) continue;
            RemoteViews v = posters(ctx, c == Continue.class ? R.string.widget_continue : R.string.widget_new);
            v.setTextViewText(R.id.empty, ctx.getString(R.string.widget_open));
            v.setViewVisibility(R.id.empty, View.VISIBLE);
            m.updateAppWidget(ids, v);
        }
        int[] j = ids(ctx, Journal.class);
        if (j.length > 0) {
            RemoteViews v = new RemoteViews(ctx.getPackageName(), R.layout.widget_journal);
            v.setTextViewText(R.id.count, "–");
            v.setTextViewText(R.id.caption, ctx.getString(R.string.widget_open));
            v.setTextViewText(R.id.hours, "");
            v.setOnClickPendingIntent(R.id.root, SyncJob.open(ctx, Uri.parse("kinora://home"), 300));
            m.updateAppWidget(j, v);
        }
    }

    static void render(Context ctx, Api api, JSONObject home) {
        AppWidgetManager m = AppWidgetManager.getInstance(ctx);
        JSONArray rows = home.optJSONArray("rows");
        JSONArray cont = null, fresh = null;
        for (int i = 0; rows != null && i < rows.length(); i++) {
            JSONObject r = rows.optJSONObject(i);
            if (r == null) continue;
            String id = r.optString("id");
            if ("continue".equals(id)) cont = r.optJSONArray("items");
            else if (fresh == null && id.startsWith("new")) fresh = r.optJSONArray("items");
        }
        int[] ids = ids(ctx, Continue.class);
        if (ids.length > 0) m.updateAppWidget(ids, row(ctx, api, R.string.widget_continue, cont, "play", 100));
        ids = ids(ctx, New.class);
        if (ids.length > 0) m.updateAppWidget(ids, row(ctx, api, R.string.widget_new, fresh, "item", 200));
        ids = ids(ctx, Journal.class);
        if (ids.length > 0) {
            try {
                m.updateAppWidget(ids, journal(ctx, api));
            } catch (Exception e) {
                // keep the last one
            }
        }
    }

    private static RemoteViews posters(Context ctx, int title) {
        RemoteViews v = new RemoteViews(ctx.getPackageName(), R.layout.widget_posters);
        v.setTextViewText(R.id.title, ctx.getString(title));
        v.setOnClickPendingIntent(R.id.title, SyncJob.open(ctx, Uri.parse("kinora://home"), title));
        for (int p : POSTERS) v.setViewVisibility(p, View.GONE);
        v.setViewVisibility(R.id.empty, View.GONE);
        return v;
    }

    private static RemoteViews row(Context ctx, Api api, int title, JSONArray items, String open, int code) {
        RemoteViews v = posters(ctx, title);
        int shown = 0;
        for (int i = 0; items != null && i < items.length() && shown < POSTERS.length; i++) {
            JSONObject it = items.optJSONObject(i);
            if (it == null || it.optLong("id") <= 0) continue;
            int p = POSTERS[shown++];
            Bitmap b = api.image(Api.imagePath(it, "poster"), 360);
            if (b != null) v.setImageViewBitmap(p, b);
            else v.setImageViewResource(p, R.drawable.widget_noart);
            v.setContentDescription(p, label(it));
            v.setViewVisibility(p, View.VISIBLE);
            v.setOnClickPendingIntent(p, SyncJob.open(ctx, Uri.parse("kinora://" + open + "/" + it.optLong("id")), code + shown));
        }
        if (shown == 0) {
            v.setTextViewText(R.id.empty, ctx.getString(R.string.widget_empty));
            v.setViewVisibility(R.id.empty, View.VISIBLE);
        }
        return v;
    }

    static String label(JSONObject it) {
        if ("episode".equals(it.optString("kind")) && !it.optString("series_title").isEmpty()) {
            return it.optString("series_title") + " – " + it.optString("title");
        }
        return it.optString("title");
    }

    private static RemoteViews journal(Context ctx, Api api) throws Exception {
        Calendar c = Calendar.getInstance();
        c.set(Calendar.DAY_OF_MONTH, 1);
        c.set(Calendar.HOUR_OF_DAY, 0);
        c.set(Calendar.MINUTE, 0);
        c.set(Calendar.SECOND, 0);
        c.set(Calendar.MILLISECOND, 0);
        long from = c.getTimeInMillis() / 1000;
        c.add(Calendar.MONTH, 1);
        long to = c.getTimeInMillis() / 1000;
        JSONArray evs = api.get("/api/journal?from=" + from + "&to=" + to).optJSONArray("events");
        int n = evs == null ? 0 : evs.length();
        long sec = 0;
        for (int i = 0; i < n; i++) {
            JSONObject e = evs.optJSONObject(i);
            if (e != null) sec += e.optLong("runtime_sec");
        }
        RemoteViews v = new RemoteViews(ctx.getPackageName(), R.layout.widget_journal);
        v.setTextViewText(R.id.count, String.valueOf(n));
        v.setTextViewText(R.id.caption, ctx.getResources().getQuantityString(R.plurals.widget_viewings, n));
        long hours = Math.round(sec / 3600.0);
        v.setTextViewText(R.id.hours, hours > 0 ? ctx.getResources().getQuantityString(R.plurals.widget_hours, (int) hours, (int) hours) : "");
        v.setOnClickPendingIntent(R.id.root, SyncJob.open(ctx, Uri.parse("kinora://journal"), 300));
        return v;
    }
}
