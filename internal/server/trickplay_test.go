package server

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ScotyLucas/kinora/internal/store"
	"github.com/ScotyLucas/kinora/internal/trickplay"
)

func TestTrickplayAPI(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	c, st, srv := newTestServerFull(t)
	gen := trickplay.New(st, "ffmpeg", filepath.Join(t.TempDir(), "cache"), srv.log)
	srv.Trickplay = gen
	selectFirstProfile(t, c)
	movie := itemID(st, "Big Test Movie")
	var tp struct {
		Ready    bool                `json:"ready"`
		Manifest *trickplay.Manifest `json:"manifest"`
	}
	if code := c.do("GET", "/api/items/"+itoa(movie)+"/trickplay", nil, &tp); code != 200 || tp.Ready {
		t.Fatalf("before generation: %d %+v", code, tp)
	}
	if gen.Status().Queue != 1 {
		t.Fatal("opening the item must queue it")
	}
	src, err := st.TrickplaySource(movie)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gen.Generate(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if code := c.do("GET", "/api/items/"+itoa(movie)+"/trickplay", nil, &tp); code != 200 || !tp.Ready || tp.Manifest.Sheets < 1 || tp.Manifest.Count < 1 {
		t.Fatalf("after generation: %d %+v", code, tp)
	}
	resp, _ := c.raw("GET", "/api/items/"+itoa(movie)+"/trickplay/0.jpg", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("sheet: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, bad := range []string{"99.jpg", "x.jpg", "..%2fmanifest.json"} {
		if code := c.do("GET", "/api/items/"+itoa(movie)+"/trickplay/"+bad, nil, nil); code == 200 {
			t.Fatalf("sheet %s served", bad)
		}
	}

	// a kids profile does not see the R film's pictures either
	rID := itemID(st, "Space Drama")
	if src, err := st.TrickplaySource(rID); err == nil {
		if _, err := gen.Generate(context.Background(), src); err != nil {
			t.Fatal(err)
		}
	}
	var kid store.Profile
	c.do("POST", "/api/profiles", map[string]any{"name": "Kicsi", "is_kids": true, "max_rating_level": 2}, &kid)
	if code := c.do("POST", "/api/profiles/"+itoa(kid.ID)+"/select", map[string]string{}, nil); code != 200 {
		t.Fatal("select kid")
	}
	if code := c.do("GET", "/api/items/"+itoa(rID)+"/trickplay", nil, nil); code != 404 {
		t.Fatalf("kid manifest: %d", code)
	}
	if code := c.do("GET", "/api/items/"+itoa(rID)+"/trickplay/0.jpg", nil, nil); code != 404 {
		t.Fatalf("kid sheet: %d", code)
	}
}
