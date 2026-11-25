package scanner

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ScotyLucas/kinora/internal/store"
)

// testMedia returns a private copy of the generated test library (scripts/mkmedia.sh).
func testMedia(t *testing.T) string {
	t.Helper()
	src, _ := filepath.Abs("../../testmedia")
	if _, err := os.Stat(filepath.Join(src, ".complete")); err != nil {
		t.Skip("testmedia missing – run scripts/mkmedia.sh testmedia")
	}
	dst := filepath.Join(t.TempDir(), "media")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		if err == nil {
			os.Chtimes(target, info.ModTime(), info.ModTime())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func setup(t *testing.T) (*store.Store, *Scanner, string) {
	t.Helper()
	media := testMedia(t)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return st, New(st, filepath.Join(dir, "images"), log), media
}

func count(t *testing.T, st *store.Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestScanMoviesAndShows(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	movies, _ := st.CreateLibrary("Filmek", filepath.Join(media, "Movies"), "movies")
	shows, _ := st.CreateLibrary("Sorozatok", filepath.Join(media, "Shows"), "shows")

	s1, err := sc.ScanNow(ctx, movies.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Changed != 3 || s1.Errors != 0 {
		t.Fatalf("first movie scan: %+v", s1)
	}
	if _, err := sc.ScanNow(ctx, shows.ID); err != nil {
		t.Fatal(err)
	}

	// movie metadata from NFO + probe
	var title, rating, vcodec, acodec, container string
	var year, level int
	var credits float64
	var tmdb int64
	err = st.DB.QueryRow(`SELECT title,year,content_rating,rating_level,video_codec,audio_codec,container,credits_start,tmdb_id FROM items WHERE kind='movie' AND title='Big Test Movie'`).
		Scan(&title, &year, &rating, &level, &vcodec, &acodec, &container, &credits, &tmdb)
	if err != nil {
		t.Fatal(err)
	}
	if year != 2019 || rating != "PG-13" || level != 2 || vcodec != "h264" || acodec != "aac" || container != "mp4" || credits < 49 || credits > 51 || tmdb != -1 {
		t.Fatalf("big test movie: %s %d %s %d %s %s %s %v %d", title, year, rating, level, vcodec, acodec, container, credits, tmdb)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM item_genres g JOIN items i ON i.id=g.item_id WHERE i.title='Big Test Movie'`); n != 2 {
		t.Fatalf("genres split by '/': %d", n)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM media_streams s JOIN items i ON i.id=s.item_id WHERE i.title='Big Test Movie' AND s.type='subtitle' AND s.lang='hun' AND s.external_path<>''`); n != 1 {
		t.Fatalf("external hun subtitle not found")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE title='Space Drama' AND rating_level=3 AND video_codec='hevc' AND audio_codec='ac3'`); n != 1 {
		t.Fatal("space drama")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE title='Kids Cartoon' AND year=2018 AND rating_level IS NULL`); n != 1 {
		t.Fatal("kids cartoon")
	}

	// shows: Test Show (2 seasons, 4 episodes, TV-PG inherited), Other Series
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND kind='series'`, shows.ID); n != 2 {
		t.Fatalf("series = %d", n)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND kind='season'`, shows.ID); n != 3 {
		t.Fatalf("seasons = %d", n)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items e JOIN items s ON s.id=e.series_id WHERE e.kind='episode' AND s.title='Test Show' AND e.rating_level=1`); n != 4 {
		t.Fatalf("Test Show episodes with inherited rating = %d", n)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE kind='episode' AND title='Pilot Part 2' AND season_num=1 AND episode_num=2`); n != 1 {
		t.Fatal("episode title parse")
	}
	// thumbnails generated
	var anyID int64
	st.DB.QueryRow(`SELECT id FROM items WHERE kind='episode' LIMIT 1`).Scan(&anyID)
	if _, err := os.Stat(filepath.Join(sc.imgDir, itoa(anyID), "thumb.jpg")); err != nil {
		t.Fatal("thumbnail missing", err)
	}

	// second scan: nothing changes, and it is fast
	t0 := time.Now()
	s2, err := sc.ScanNow(ctx, movies.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Changed != 0 || s2.Total != 0 || s2.Removed != 0 {
		t.Fatalf("rescan should be a no-op: %+v", s2)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("rescan too slow: %v", d)
	}

	// rename/move keeps id and watch state
	var id int64
	st.DB.QueryRow(`SELECT id FROM items WHERE title='Kids Cartoon'`).Scan(&id)
	prof, _ := st.CreateUser("u", "secret1", false)
	ps, _ := st.Profiles(prof.ID)
	st.DB.Exec(`INSERT INTO user_data(profile_id,item_id,played,play_count) VALUES(?,?,1,1)`, ps[0].ID, id)
	os.MkdirAll(filepath.Join(media, "Movies", "Kids Cartoon (2018)"), 0o755)
	if err := os.Rename(filepath.Join(media, "Movies", "Kids.Cartoon.2018.mp4"), filepath.Join(media, "Movies", "Kids Cartoon (2018)", "Kids Cartoon (2018).mp4")); err != nil {
		t.Fatal(err)
	}
	s3, err := sc.ScanNow(ctx, movies.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s3.Moved != 1 || s3.Removed != 0 {
		t.Fatalf("move not detected: %+v", s3)
	}
	var id2 int64
	var missing int
	st.DB.QueryRow(`SELECT id,missing FROM items WHERE path LIKE '%Kids Cartoon (2018).mp4'`).Scan(&id2, &missing)
	if id2 != id || missing != 0 {
		t.Fatalf("moved item id %d→%d missing=%d", id, id2, missing)
	}
	if ud := st.GetUserData(ps[0].ID, id); !ud.Played {
		t.Fatal("watch state lost after move")
	}

	// deleting a file marks it missing (not deleted)
	os.Remove(filepath.Join(media, "Shows", "Test Show (2020)", "Season 02", "Test.Show.S02E01.mkv"))
	s4, _ := sc.ScanNow(ctx, shows.ID)
	if s4.Removed != 1 {
		t.Fatalf("removed=%d", s4.Removed)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE kind='season' AND season_num=2 AND missing=1`); n != 1 {
		t.Fatal("empty season should be missing")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE kind='episode' AND season_num=2`); n != 1 {
		t.Fatal("episode row must survive as missing")
	}
}

func TestUnavailableRootDeletesNothing(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary("Filmek", filepath.Join(media, "Movies"), "movies")
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	before := count(t, st, `SELECT COUNT(*) FROM items WHERE missing=0`)

	// share unmounted: root gone
	os.Rename(filepath.Join(media, "Movies"), filepath.Join(media, "Movies.offline"))
	if _, err := sc.ScanNow(ctx, lib.ID); err == nil {
		t.Fatal("scan of unavailable root must fail")
	}
	// share mounted but empty mountpoint
	os.MkdirAll(filepath.Join(media, "Movies"), 0o755)
	if _, err := sc.ScanNow(ctx, lib.ID); err == nil {
		t.Fatal("scan of suddenly empty root must fail")
	}
	if after := count(t, st, `SELECT COUNT(*) FROM items WHERE missing=0`); after != before {
		t.Fatalf("items changed: %d → %d", before, after)
	}
}

// A library of two folders scans both, and one of them going empty (an unmounted disk) changes nothing.
func TestLibraryOfSeveralFolders(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	one, _ := st.CreateLibrary("Egy", filepath.Join(media, "Movies"), "movies")
	if _, err := sc.ScanNow(ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	want := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND missing=0`, one.ID)
	st.DeleteLibrary(one.ID)

	// move the first movie folder onto a second "disk"
	ents, _ := os.ReadDir(filepath.Join(media, "Movies"))
	var moved string
	for _, e := range ents {
		if e.IsDir() {
			moved = e.Name()
			break
		}
	}
	second := filepath.Join(media, "Movies2")
	os.MkdirAll(second, 0o755)
	if err := os.Rename(filepath.Join(media, "Movies", moved), filepath.Join(second, moved)); err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibraryPaths("Két mappa", []string{filepath.Join(media, "Movies"), second}, "movies")
	if err != nil || len(lib.Paths) != 2 || lib.Path != filepath.Join(media, "Movies") {
		t.Fatalf("library: %+v %v", lib, err)
	}
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if got := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND missing=0`, lib.ID); got != want {
		t.Fatalf("two folders: %d items, one folder had %d", got, want)
	}

	// the second disk is unmounted: an empty mountpoint must not mark its films missing
	os.Rename(second, second+".offline")
	os.MkdirAll(second, 0o755)
	if _, err := sc.ScanNow(ctx, lib.ID); err == nil {
		t.Fatal("scan with a suddenly empty folder must fail")
	}
	if got := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND missing=0`, lib.ID); got != want {
		t.Fatalf("items changed: %d → %d", want, got)
	}
}

func itoa(i int64) string { return filepath.Base(ImageDir("", i)) }

// Episodes downloaded one by one (a folder per release) form one show, and episodes an
// earlier scan put under a show of their own move over on the next scan.
func TestReleaseFoldersFormOneShow(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	src := filepath.Join(media, "Shows", "Test Show (2020)", "Season 01", "Test.Show.S01E01.Pilot.Part.1.mkv")
	root := filepath.Join(media, "Downloads")
	for _, rel := range []string{
		"Line.of.Fire.S01E01.1080p.WEB-GRP/Line.of.Fire.S01E01.1080p.WEB-GRP.mkv",
		"Line.of.Fire.S01E02.1080p.WEB-GRP/Line.of.Fire.S01E02.1080p.WEB-GRP.mkv",
		"Line.of.Fire.S01E03.720p.HDTV/x7f2a.mkv",
	} {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.Link(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	lib, _ := st.CreateLibrary("Letöltések", root, "shows")
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND kind='series' AND missing=0`, lib.ID); n != 1 {
		t.Fatalf("series = %d, want 1", n)
	}
	if n := count(t, st, `SELECT COUNT(DISTINCT episode_num) FROM items WHERE library_id=? AND kind='episode'`, lib.ID); n != 3 {
		t.Fatalf("distinct episodes = %d, want 3", n)
	}

	// simulate the old split: one episode under a show of its own
	if _, err := st.UpsertContainer(&store.ContainerRecord{LibraryID: lib.ID, Kind: "series", Key: "series:stale", Title: "Stale"}, SortTitle); err != nil {
		t.Fatal(err)
	}
	var stale int64
	st.DB.QueryRow(`SELECT id FROM items WHERE key='series:stale'`).Scan(&stale)
	if res, err := st.DB.Exec(`UPDATE items SET series_id=? WHERE library_id=? AND kind='episode' AND episode_num=2`, stale, lib.ID); err != nil {
		t.Fatal(err)
	} else if n, _ := res.RowsAffected(); n != 1 || stale == 0 {
		t.Fatalf("setup: moved %d, stale id %d", n, stale)
	}
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE kind='episode' AND series_id=?`, stale); n != 0 {
		t.Fatalf("episode still under the stale show")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND kind='series' AND missing=0`, lib.ID); n != 1 {
		t.Fatalf("visible series = %d, want 1", n)
	}
}

// A downloaded release folder and the copy Kinora imported into the show's folder are one
// show once metadata matched both to the same TMDB id: one card, each episode once.
func TestSameTmdbShowIsOneCard(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	src := filepath.Join(media, "Shows", "Test Show (2020)", "Season 01", "Test.Show.S01E01.Pilot.Part.1.mkv")
	root := filepath.Join(media, "Media")
	for _, rel := range []string{
		"Downloads/A.Test.S01.1080p.WEB-GRP/A.Test.S01E01.1080p.WEB-GRP.mkv",
		"Downloads/A.Test.S01.1080p.WEB-GRP/A.Test.S01E02.1080p.WEB-GRP.mkv",
		"Downloads/A.Test.S01.1080p.WEB-GRP/A.Test.S01E03.1080p.WEB-GRP.mkv",
		"Sorozatok/A Test (2025)/Season 01/A Test (2025) - S01E01 - 1080p.mkv",
		"Sorozatok/A Test (2025)/Season 01/A Test (2025) - S01E02 - 1080p.mkv",
		"Sorozatok/A Test (2025)/Season 01/A Test (2025) - S01E03 - 1080p.mkv",
	} {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.Link(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	lib, _ := st.CreateLibrary("Média", root, "mixed")
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	visible := func() int {
		return count(t, st, `SELECT COUNT(*) FROM items WHERE library_id=? AND kind='series' AND missing=0`, lib.ID)
	}
	if n := visible(); n != 2 {
		t.Fatalf("before matching: series = %d, want 2 (different names)", n)
	}
	// metadata matched both to the same show
	st.DB.Exec(`UPDATE items SET tmdb_id=4242 WHERE library_id=? AND kind='series'`, lib.ID)
	if !st.HasSplitSeries(lib.ID) {
		t.Fatal("HasSplitSeries = false")
	}
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if n := visible(); n != 1 || st.HasSplitSeries(lib.ID) {
		t.Fatalf("after joining: series = %d, want 1", n)
	}
	var sid int64
	st.DB.QueryRow(`SELECT id FROM items WHERE library_id=? AND kind='series' AND missing=0`, lib.ID).Scan(&sid)
	var path string
	st.DB.QueryRow(`SELECT path FROM items WHERE id=?`, sid).Scan(&path)
	if filepath.Base(path) != "A Test (2025)" {
		t.Errorf("kept series %q, want the show's own folder", path)
	}

	name := "P"
	u, _ := st.CreateUser("u", "secret12", true)
	p, _ := st.CreateProfile(u.ID, store.ProfileInput{Name: &name})
	series, err := st.ItemFull(sid, p)
	if err != nil {
		t.Fatal(err)
	}
	if series.EpisodeCount != 3 {
		t.Errorf("episode count = %d, want 3", series.EpisodeCount)
	}
	seasons, _ := st.Children(p, series)
	if len(seasons) != 1 {
		t.Fatalf("seasons = %d, want 1", len(seasons))
	}
	eps, _ := st.Children(p, seasons[0])
	if len(eps) != 3 {
		t.Errorf("episodes listed = %d, want 3", len(eps))
	}
	// stable: another scan keeps it joined and re-probes nothing
	s3, err := sc.ScanNow(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := visible(); n != 1 || s3.Changed != 0 {
		t.Errorf("rescan: series = %d, changed = %d", n, s3.Changed)
	}
}

// A re-scan of a file (a new subtitle, a migration clearing sidecar_sig) must not wipe the
// artwork TMDB downloaded for a matched item, while an unmatched item still follows its folder.
func TestRescanKeepsOnlineArtwork(t *testing.T) {
	st, sc, media := setup(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary("Filmek", filepath.Join(media, "Movies"), "movies")
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	var matched, plain int64
	st.DB.QueryRow(`SELECT id FROM items WHERE kind='movie' AND path LIKE '%Kids.Cartoon%'`).Scan(&matched)
	st.DB.QueryRow(`SELECT id FROM items WHERE kind='movie' AND path LIKE '%Space.Drama%'`).Scan(&plain)
	if matched == 0 || plain == 0 {
		t.Fatal("test movies not found")
	}
	for _, id := range []int64{matched, plain} {
		dir := ImageDir(sc.imgDir, id)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("jpg"), 0o644)
		os.WriteFile(filepath.Join(dir, "backdrop.jpg"), []byte("jpg"), 0o644)
		st.SetImageFlags(id, true, true)
	}
	st.DB.Exec(`UPDATE items SET tmdb_id=42 WHERE id=?`, matched)
	st.DB.Exec(`UPDATE items SET tmdb_id=NULL WHERE id=?`, plain)
	st.DB.Exec(`UPDATE items SET sidecar_sig='' WHERE kind='movie'`)
	if _, err := sc.ScanNow(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	if p, b := st.ImageFlags(matched); !p || !b {
		t.Fatalf("matched item lost its TMDB artwork: poster=%v backdrop=%v", p, b)
	}
	if _, err := os.Stat(filepath.Join(ImageDir(sc.imgDir, matched), "poster.jpg")); err != nil {
		t.Fatal("matched item's poster.jpg was deleted")
	}
	if p, _ := st.ImageFlags(plain); p {
		t.Fatal("unmatched item kept a poster its folder no longer has")
	}
}
