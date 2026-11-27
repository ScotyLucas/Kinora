package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ScotyLucas/kinora/internal/scanner"
)

// anon is a client without the session cookie, like a chat app fetching a link.
func anon(c *client) *client {
	return &client{t: c.t, base: c.base, hc: &http.Client{}}
}

func getPage(t *testing.T, c *client, path string) (int, string) {
	t.Helper()
	resp, b := c.raw("GET", path, nil)
	return resp.StatusCode, string(b)
}

func mustHave(t *testing.T, page string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(page, w) {
			t.Fatalf("missing %q in\n%s", w, page)
		}
	}
}

func mustNotHave(t *testing.T, page string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(page, b) {
			t.Fatalf("%q must not be in\n%s", b, page)
		}
	}
}

func TestLinkPreviewShell(t *testing.T) {
	c, st, _ := newTestServerFull(t)
	a := anon(c)
	st.SetSetting("server_name", `Mozi <b>& "Ház"`)
	code, page := getPage(t, a, "/")
	if code != 200 {
		t.Fatalf("shell %d", code)
	}
	mustHave(t, page,
		`<meta property="og:title" content="Mozi &lt;b&gt;&amp; &#34;Ház&#34;">`,
		`<meta property="og:image" content="`+c.base+`/icons/icon-512.png">`,
		`<div id="app"></div>`)
	mustNotHave(t, page, `Mozi <b>`)
	if i, j := strings.Index(page, "og:title"), strings.Index(page, "</head>"); i < 0 || i > j {
		t.Fatal("tags are not in the head")
	}
	// unknown paths get the same shell; a password link's fragment never reaches the server
	_, page = getPage(t, a, "/setpw/ABC")
	mustHave(t, page, `og:title`, `<div id="app"></div>`)

	st.SetSetting("public_url", "https://kino.example.com/")
	_, page = getPage(t, a, "/")
	mustHave(t, page, `<meta property="og:image" content="https://kino.example.com/icons/icon-512.png">`)
}

func TestPartyLinkPreview(t *testing.T) {
	c, st, srv := newTestServerFull(t)
	a := anon(c)
	st.SetSetting("server_name", "Otthon")
	movie := itemID(st, "Big Test Movie")
	room := srv.Party.Create(movie, false, 1)

	// no poster yet: the title, the server's icon
	code, page := getPage(t, a, "/party/"+strings.ToLower(room.Code))
	if code != 200 {
		t.Fatalf("party preview %d", code)
	}
	mustHave(t, page, `<meta property="og:title" content="Watch party: Big Test Movie`,
		`Otthon`, `/icons/icon-512.png"`,
		`<meta http-equiv="refresh" content="0;url=../#/party/`+room.Code+`">`, `noindex`)

	poster := filepath.Join(scanner.ImageDir(srv.ImgDir, movie), "poster.jpg")
	os.MkdirAll(filepath.Dir(poster), 0o755)
	if err := os.WriteFile(poster, []byte("\xff\xd8\xff\xe0jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, page = getPage(t, a, "/party/"+room.Code)
	mustHave(t, page, `<meta property="og:image" content="`+c.base+`/party/`+room.Code+`/poster.jpg">`,
		`<meta name="twitter:card" content="summary_large_image">`)
	resp, _ := a.raw("GET", "/party/"+room.Code+"/poster.jpg", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("poster %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// a closed or wrong code: the server's own preview, still sent on to the app
	code, page = getPage(t, a, "/party/ZZZZ99")
	if code != 200 {
		t.Fatalf("unknown party %d", code)
	}
	mustHave(t, page, `<meta property="og:title" content="Otthon">`, `url=../#/party/ZZZZ99"`)
	mustNotHave(t, page, "Big Test Movie")
	if resp, _ := a.raw("GET", "/party/ZZZZ99/poster.jpg", nil); resp.StatusCode != 404 {
		t.Fatalf("poster of an unknown party: %d", resp.StatusCode)
	}
	_, page = getPage(t, a, "/party/%3Cscript%3E")
	mustHave(t, page, `url=../#/home"`)
	mustNotHave(t, page, "<script")
}

func TestJoinLinkPreview(t *testing.T) {
	c, st, _ := newTestServerFull(t)
	a := anon(c)
	st.SetSetting("server_name", "Otthon")
	inv, err := st.CreateInvite(1, "Anyának titok", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, page := getPage(t, a, "/join/"+inv.Code)
	mustHave(t, page, `<meta property="og:title" content="Meghívó: Otthon">`, `url=../#/join/`+inv.Code+`"`)
	mustNotHave(t, page, "Anyának")

	_, page = getPage(t, a, "/join/NOPE123")
	mustHave(t, page, `<meta property="og:title" content="Otthon">`, `url=../#/join/NOPE123"`)
	mustNotHave(t, page, "Meghívó")
}
