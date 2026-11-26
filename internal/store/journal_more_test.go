package store

import "testing"

func TestJournalRatingManualAndCredits(t *testing.T) {
	f := fixture(t)
	s := f.s
	s.DB.Exec(`UPDATE items SET tmdb_id=603 WHERE id=?`, f.ids["matrix"])
	matrix, _ := s.Item(f.ids["matrix"], f.p)
	if err := s.SetPlayed(f.p.ID, matrix, true); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.Journal(f.p.ID, 0, now()+1)
	if len(evs) != 1 || evs[0].TmdbID != 603 || evs[0].Rating != 0 || len(evs[0].Directors) != 0 {
		t.Fatalf("snapshot: %+v", evs)
	}
	// stars on the viewing reach the title in the library too
	if err := s.SetEventRating(f.p.ID, evs[0].ID, 4); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(f.ids["matrix"], f.p); it.UserData.Stars == nil || *it.UserData.Stars != 4 {
		t.Fatalf("item stars: %+v", it.UserData)
	}
	if err := s.SetEventRating(f.kid.ID, evs[0].ID, 2); err != ErrNotFound {
		t.Fatalf("another profile rated it: %v", err)
	}

	// credits fill the breakdowns once fetched
	miss, _ := s.MissingCredits(10)
	if len(miss) != 1 || miss[0] != (CreditKey{"movie", 603}) {
		t.Fatalf("missing credits: %+v", miss)
	}
	s.PutCredits(miss[0], Credits{Directors: []string{"Lana Wachowski", "Lilly Wachowski"}, Cast: []string{"Keanu Reeves"}, Countries: []string{"US"}})
	if miss, _ := s.MissingCredits(10); len(miss) != 0 {
		t.Fatalf("still missing: %+v", miss)
	}

	// a film seen at the cinema, added by hand; the same one again at the same time is refused
	at := now() - 3*86400
	id, err := s.AddManualEvent(f.p.ID, ManualEvent{TmdbID: 27205, Title: "Inception", Year: 2010, RuntimeSec: 8880, Genres: []string{"Sci-Fi"}, At: at, Rating: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddManualEvent(f.p.ID, ManualEvent{TmdbID: 27205, Title: "Inception", At: at + 60}); err != ErrDuplicate {
		t.Fatalf("duplicate: %v", err)
	}
	evs, _ = s.Journal(f.p.ID, 0, now()+1)
	if len(evs) != 2 || evs[0].Source != "manual" || evs[0].Rating != 5 || evs[0].Year == nil || *evs[0].Year != 2010 {
		t.Fatalf("manual: %+v", evs)
	}
	if evs[1].Directors[1] != "Lilly Wachowski" || evs[1].Countries[0] != "US" || evs[1].Rating != 4 {
		t.Fatalf("credits: %+v", evs[1])
	}
	if err := s.DeleteEvent(f.p.ID, evs[1].ID); err != ErrNotManual {
		t.Fatalf("a played viewing was deleted: %v", err)
	}
	if err := s.DeleteEvent(f.p.ID, id); err != nil {
		t.Fatal(err)
	}
}

func TestJournalAbandoned(t *testing.T) {
	f := fixture(t)
	s := f.s
	matrix, _ := s.Item(f.ids["matrix"], f.p)
	if _, err := s.ReportProgress(f.p.ID, matrix, 300, false); err != nil {
		t.Fatal(err)
	}
	// still fresh: Continue Watching, not abandoned
	if a, _ := s.Abandoned(f.p); len(a) != 0 {
		t.Fatalf("fresh: %+v", a)
	}
	s.DB.Exec(`UPDATE user_data SET last_played_at=? WHERE profile_id=? AND item_id=?`, now()-30*86400, f.p.ID, matrix.ID)
	a, err := s.Abandoned(f.p)
	if err != nil || len(a) != 1 || a[0].ID != matrix.ID {
		t.Fatalf("abandoned: %v %+v", err, a)
	}
	s.HideAbandoned(f.p.ID, matrix.ID)
	if a, _ := s.Abandoned(f.p); len(a) != 0 {
		t.Fatalf("hidden: %+v", a)
	}
}

func TestJournalGoalsAndChallenges(t *testing.T) {
	f := fixture(t)
	s := f.s
	if _, err := s.AddJournalGoal(f.p.ID, JournalGoal{Period: "month", Metric: "hours", Target: 20}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.AddJournalGoal(f.p.ID, JournalGoal{Period: "year", Metric: "genre", Genre: "Drama", Target: 10})
	if gs, _ := s.JournalGoals(f.p.ID); len(gs) != 2 || gs[1].Genre != "Drama" {
		t.Fatalf("goals: %+v", gs)
	}
	if err := s.DeleteJournalGoal(f.kid.ID, gid); err != ErrNotFound {
		t.Fatalf("another profile deleted a goal: %v", err)
	}

	s.DB.Exec(`UPDATE items SET tmdb_id=603 WHERE id=?`, f.ids["matrix"])
	matrix, _ := s.Item(f.ids["matrix"], f.p)
	s.SetPlayed(f.p.ID, matrix, true)
	cid, err := s.AddChallenge(f.p.ID, "Wachowski", []ChallengeItem{
		{Kind: "movie", TmdbID: 603, Title: "The Matrix", Year: 1999},
		{Kind: "movie", TmdbID: 604, Title: "The Matrix Reloaded", Year: 2003},
		{Kind: "movie", TmdbID: 603, Title: "dupe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := s.Challenges(f.p)
	if len(cs) != 1 || cs[0].Total != 2 || cs[0].Done != 1 || !cs[0].Items[0].Watched || cs[0].Items[0].ItemID != matrix.ID {
		t.Fatalf("challenge: %+v", cs)
	}
	if err := s.AddChallengeItems(f.kid.ID, cid, []ChallengeItem{{Kind: "movie", TmdbID: 1, Title: "x"}}); err != ErrNotFound {
		t.Fatalf("another profile added: %v", err)
	}
	s.RemoveChallengeItem(f.p.ID, cid, "movie", 604)
	if cs, _ := s.Challenges(f.p); cs[0].Total != 1 || cs[0].Done != 1 {
		t.Fatalf("after remove: %+v", cs)
	}
	if kc, _ := s.Challenges(f.kid); len(kc) != 0 {
		t.Fatalf("kid sees: %+v", kc)
	}
	if err := s.DeleteChallenge(f.p.ID, cid); err != nil {
		t.Fatal(err)
	}
}
