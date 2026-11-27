package io.github.scotylucas.kinora;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

public class LinksTest {
    @Test
    public void hashOfOpensTheKnownScreens() {
        assertEquals("#/play/42", Links.hashOf("kinora://play/42"));
        assertEquals("#/item/7", Links.hashOf("kinora://item/7/"));
        assertEquals("#/party/ABC234", Links.hashOf("kinora://party/ABC234"));
        assertEquals("#/journal?recap=2026-10", Links.hashOf("kinora://journal?recap=2026-10"));
        assertEquals("#/resume", Links.hashOf("kinora://RESUME"));
        assertEquals("#/downloads", Links.hashOf("kinora://downloads"));
    }

    @Test
    public void hashOfRefusesEverythingElse() {
        assertNull(Links.hashOf(null));
        assertNull(Links.hashOf("https://evil.example/#/play/42"));
        assertNull(Links.hashOf("kinora://admin/users"));
        assertNull(Links.hashOf("kinora://play/42abc"));
        assertNull(Links.hashOf("kinora://play/42?x=<script>"));
        assertNull(Links.hashOf("kinora://user@play/42"));
        assertNull(Links.hashOf("kinora://play:80/42"));
        assertNull(Links.hashOf("kinora://item/1/../../admin"));
    }

    @Test
    public void notificationLinks() {
        assertEquals("kinora://journal?recap=2026-10", Links.ofNotification("#/journal?recap=2026-10", 0));
        assertEquals("kinora://item/5", Links.ofNotification("#/admin/requests", 5));
        assertEquals("kinora://item/5", Links.ofNotification("", 5));
        assertEquals("kinora://home", Links.ofNotification(null, 0));
    }

    @Test
    public void originOfServerPages() {
        assertEquals("http://192.168.1.10:8096", Links.originOf("http://192.168.1.10:8096/?app=android#/home"));
        assertEquals("https://kinora.example.org", Links.originOf("https://Kinora.Example.org/#/play/1"));
        assertEquals("http://[fd00::1]:8096", Links.originOf("http://[fd00::1]:8096/"));
        assertNull(Links.originOf("http://localhost/"));
        assertNull(Links.originOf("file:///android_asset/index.html"));
        assertNull(Links.originOf("javascript:alert(1)"));
        assertNull(Links.originOf(null));
    }
}
