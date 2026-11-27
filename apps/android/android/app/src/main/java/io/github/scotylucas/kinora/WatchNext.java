package io.github.scotylucas.kinora;

import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.media.tv.TvContract;
import android.media.tv.TvContract.WatchNextPrograms;
import android.net.Uri;
import android.os.Build;

import androidx.annotation.RequiresApi;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

/**
 * Android TV's "Play next" row on the home screen: what the profile is in the middle of, and the next
 * episodes, as the web client's home shows them. A tap opens kinora://play/ID. The launcher fetches the
 * posters itself, so their addresses carry an artwork-only token (media_token of /api/me).
 */
@RequiresApi(api = Build.VERSION_CODES.O)
final class WatchNext {
    private WatchNext() {}

    static void sync(Context ctx, Api api, JSONObject home) {
        ContentResolver cr = ctx.getContentResolver();
        String token;
        try {
            token = api.get("/api/me").optString("media_token");
        } catch (Exception e) {
            token = "";
        }
        // what is there from this app: internal id (the item) → row
        Map<String, Long> have = new HashMap<>();
        try (Cursor c = cr.query(WatchNextPrograms.CONTENT_URI,
                new String[]{WatchNextPrograms._ID, WatchNextPrograms.COLUMN_INTERNAL_PROVIDER_ID}, null, null, null)) {
            while (c != null && c.moveToNext()) {
                if (!c.isNull(1)) have.put(c.getString(1), c.getLong(0));
            }
        } catch (Exception e) {
            return; // no TV provider (not an Android TV home screen)
        }

        Set<String> keep = new HashSet<>();
        JSONArray rows = home.optJSONArray("rows");
        long now = System.currentTimeMillis();
        int order = 0;
        for (int r = 0; rows != null && r < rows.length(); r++) {
            JSONObject row = rows.optJSONObject(r);
            if (row == null) continue;
            String id = row.optString("id");
            int type;
            if ("continue".equals(id)) type = WatchNextPrograms.WATCH_NEXT_TYPE_CONTINUE;
            else if ("nextup".equals(id)) type = WatchNextPrograms.WATCH_NEXT_TYPE_NEXT;
            else continue;
            JSONArray items = row.optJSONArray("items");
            for (int i = 0; items != null && i < items.length() && keep.size() < 20; i++) {
                JSONObject it = items.optJSONObject(i);
                if (it == null || it.optLong("id") <= 0) continue;
                String key = String.valueOf(it.optLong("id"));
                if (!keep.add(key)) continue;
                ContentValues v = values(api, it, type, token, now - (order++) * 1000L);
                try {
                    Long row0 = have.get(key);
                    if (row0 != null) cr.update(TvContract.buildWatchNextProgramUri(row0), v, null, null);
                    else cr.insert(WatchNextPrograms.CONTENT_URI, v);
                } catch (Exception e) {
                    // the launcher refused this one
                }
            }
        }
        for (Map.Entry<String, Long> e : have.entrySet()) {
            if (keep.contains(e.getKey())) continue;
            try {
                cr.delete(TvContract.buildWatchNextProgramUri(e.getValue()), null, null);
            } catch (Exception ex) {
                // gone already
            }
        }
    }

    private static ContentValues values(Api api, JSONObject it, int watchType, String token, long engaged) {
        ContentValues v = new ContentValues();
        boolean episode = "episode".equals(it.optString("kind"));
        v.put(WatchNextPrograms.COLUMN_INTERNAL_PROVIDER_ID, String.valueOf(it.optLong("id")));
        v.put(WatchNextPrograms.COLUMN_TYPE, episode ? WatchNextPrograms.TYPE_TV_EPISODE : WatchNextPrograms.TYPE_MOVIE);
        v.put(WatchNextPrograms.COLUMN_WATCH_NEXT_TYPE, watchType);
        v.put(WatchNextPrograms.COLUMN_LAST_ENGAGEMENT_TIME_UTC_MILLIS, engaged);
        if (episode) {
            v.put(WatchNextPrograms.COLUMN_TITLE, it.optString("series_title", it.optString("title")));
            v.put(WatchNextPrograms.COLUMN_EPISODE_TITLE, it.optString("title"));
            if (!it.isNull("season_num")) v.put(WatchNextPrograms.COLUMN_SEASON_DISPLAY_NUMBER, String.valueOf(it.optInt("season_num")));
            if (!it.isNull("episode_num")) v.put(WatchNextPrograms.COLUMN_EPISODE_DISPLAY_NUMBER, String.valueOf(it.optInt("episode_num")));
        } else {
            v.put(WatchNextPrograms.COLUMN_TITLE, it.optString("title"));
        }
        long dur = it.optLong("runtime_sec") * 1000;
        if (dur > 0) v.put(WatchNextPrograms.COLUMN_DURATION_MILLIS, (int) Math.min(Integer.MAX_VALUE, dur));
        JSONObject ud = it.optJSONObject("user_data");
        long pos = ud == null ? 0 : (long) (ud.optDouble("position", 0) * 1000);
        if (pos > 0 && watchType == WatchNextPrograms.WATCH_NEXT_TYPE_CONTINUE) {
            v.put(WatchNextPrograms.COLUMN_LAST_PLAYBACK_POSITION_MILLIS, (int) Math.min(Integer.MAX_VALUE, pos));
        }
        String poster = api.origin + Api.imagePath(it, "poster") + (token.isEmpty() ? "" : "&st=" + Uri.encode(token));
        v.put(WatchNextPrograms.COLUMN_POSTER_ART_URI, poster);
        v.put(WatchNextPrograms.COLUMN_POSTER_ART_ASPECT_RATIO, WatchNextPrograms.ASPECT_RATIO_2_3);
        v.put(WatchNextPrograms.COLUMN_INTENT_URI, "kinora://play/" + it.optLong("id"));
        return v;
    }
}
