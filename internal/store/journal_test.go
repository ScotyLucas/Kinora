package store

import "testing"

func TestJournalRecordsViewings(t *testing.T) {
	f := fixture(t)
	s := f.s
	matrix, _ := s.Item(f.ids["matrix"], f.p)
	// played to the end twice in a row (progress keeps coming past the mark): one viewing
	for i := 0; i < 2; i++ {
		if _, err := s.ReportProgress(f.p.ID, matrix, 5900, false); err != nil {
			t.Fatal(err)
		}
	}
	ep, _ := s.Item(f.ids["ep1"], f.p)
	if err := s.SetPlayed(f.p.ID, ep, true); err != nil {
		t.Fatal(err)
	}
	// a viewing from last year survives an unmark today
	s.DB.Exec(`INSERT INTO watch_events(profile_id,item_id,kind,title,runtime_sec,watched_at) VALUES(?,?,'movie','The Matrix',6000,?)`,
		f.p.ID, f.ids["matrix"], now()-400*86400)

	evs, err := s.Journal(f.p.ID, 0, now()+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("want 3 viewings, got %d: %+v", len(evs), evs)
	}
	last := evs[2]
	if last.Kind != "episode" || last.SeriesTitle != "Test Show" || last.ImageID != f.ids["show"] || len(last.Genres) != 1 || last.Genres[0] != "Comedy" {
		t.Fatalf("episode snapshot: %+v", last)
	}
	if evs[1].Title != "The Matrix" || len(evs[1].Genres) != 2 || evs[1].Source != "play" {
		t.Fatalf("film snapshot: %+v", evs[1])
	}

	s.SetPlayed(f.p.ID, matrix, false)
	evs, _ = s.Journal(f.p.ID, 0, now()+1)
	if len(evs) != 2 || evs[0].At > now()-300*86400 {
		t.Fatalf("unmark should take back only today's viewing: %+v", evs)
	}

	// the file goes, the viewing stays
	s.DB.Exec(`DELETE FROM libraries WHERE id=?`, f.libID)
	evs, _ = s.Journal(f.p.ID, 0, now()+1)
	if len(evs) != 2 || evs[1].ItemID != 0 || evs[1].Title != "Ep 1" {
		t.Fatalf("after delete: %+v", evs)
	}
	if other, _ := s.Journal(f.kid.ID, 0, now()+1); len(other) != 0 {
		t.Fatalf("another profile sees it: %+v", other)
	}
}
