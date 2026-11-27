package io.github.scotylucas.kinora;

import java.net.URI;
import java.util.Locale;
import java.util.regex.Pattern;

/**
 * The addresses the native parts work with, in plain Java (unit tested without a device):
 * kinora:// links → the web client's screen, notifications → kinora:// links, page address → server.
 */
final class Links {
    private Links() {}

    // the screens a link may open: kinora://item/42, play/42, party/ABC234, journal, downloads, search, resume, home
    private static final Pattern LINK = Pattern.compile(
            "^#/(item/\\d+|play/\\d+|party/[A-Za-z0-9]{4,12}|journal|downloads|search|resume|home)(\\?[A-Za-z0-9=&%._-]*)?$");

    /** kinora://play/42?x=1 → "#/play/42?x=1"; null for anything the app does not open. */
    static String hashOf(String link) {
        if (link == null) return null;
        URI u;
        try {
            u = new URI(link);
        } catch (Exception e) {
            return null;
        }
        if (!"kinora".equalsIgnoreCase(u.getScheme()) || u.getHost() == null || u.getRawUserInfo() != null || u.getPort() != -1) return null;
        String path = u.getRawPath() == null ? "" : u.getRawPath().replaceAll("/+$", "");
        String hash = "#/" + u.getHost().toLowerCase(Locale.ROOT) + path + (u.getRawQuery() == null ? "" : "?" + u.getRawQuery());
        return LINK.matcher(hash).matches() ? hash : null;
    }

    /** A notification's link: its web client address (#/journal?…), else the title it is about, else home. */
    static String ofNotification(String href, long item) {
        if (href != null && href.startsWith("#/") && href.length() > 2 && hashOf("kinora://" + href.substring(2)) != null) {
            return "kinora://" + href.substring(2);
        }
        return item > 0 ? "kinora://item/" + item : "kinora://home";
    }

    /** scheme://host[:port] of a server page, or null for the launcher and anything that is not http(s). */
    static String originOf(String url) {
        if (url == null) return null;
        URI u;
        try {
            u = new URI(url);
        } catch (Exception e) {
            return null;
        }
        String scheme = u.getScheme() == null ? "" : u.getScheme().toLowerCase(Locale.ROOT), host = u.getHost();
        if (host == null || !("http".equals(scheme) || "https".equals(scheme))) return null;
        host = host.toLowerCase(Locale.ROOT);
        if ("localhost".equals(host)) return null; // the launcher (Capacitor serves it as http://localhost)
        if (host.contains(":") && !host.startsWith("[")) host = "[" + host + "]";
        return scheme + "://" + host + (u.getPort() > 0 ? ":" + u.getPort() : "");
    }
}
