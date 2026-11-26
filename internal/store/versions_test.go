package store

import "testing"

func TestNormalizeImdbID(t *testing.T) {
	for in, want := range map[string]string{"tt0133093": "tt0133093", " TT0133093 ": "tt0133093", "0133093": "tt0133093",
		"https://www.imdb.com/title/tt0133093/": "tt0133093", "tt12": "", "": "", "abc": ""} {
		if got := NormalizeImdbID(in); got != want {
			t.Errorf("NormalizeImdbID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionsGroupByImdb(t *testing.T) {
	f := fixture(t)
	s := f.s
	matrix := f.ids["matrix"]
	add := func(path string, height int) int64 {
		res, err := s.DB.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,year,runtime_sec,height,width,path,size,added_at,updated_at)
			VALUES(?,?,?,?,?,1999,6000,?,?,?,?,?,?)`, f.libID, "movie", path, "The Matrix", "matrix", height, height*16/9, path, height*1000, now(), now())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	uhd := add("/m/The Matrix (1999) - 2160p.mkv", 2160)
	other := add("/m/Other.mkv", 720)
	for _, id := range []int64{matrix, uhd} {
		if err := s.SetImdbID(id, "tt0133093"); err != nil {
			t.Fatal(err)
		}
	}

	// the list shows one card: the lowest id
	items, _, err := s.Browse(f.p, RuleSet{}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]*Item{}
	for _, it := range items {
		seen[it.ID] = it
	}
	if seen[uhd] != nil || seen[matrix] == nil || seen[other] == nil {
		t.Fatalf("alt version should be hidden from the list, lead and others shown: %v", seen)
	}
	if seen[matrix].VersionCount != 2 {
		t.Fatalf("version_count = %d, want 2", seen[matrix].VersionCount)
	}

	lead, _ := s.Item(matrix, f.p)
	vs, err := s.Versions(f.p, lead)
	if err != nil || len(vs) != 2 || vs[0].ID != uhd {
		t.Fatalf("versions: %v %v (want the 4K first)", vs, err)
	}

	// watch state follows across versions
	if err := s.SetFlag(f.p.ID, uhd, "favorite", true); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(matrix, f.p); !it.UserData.Favorite {
		t.Fatal("favorite not mirrored to the other version")
	}
	u4k, _ := s.Item(uhd, f.p)
	if _, err := s.ReportProgress(f.p.ID, u4k, 1200, false); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(matrix, f.p); it.UserData.Position != 1200 {
		t.Fatalf("position not mirrored: %v", it.UserData.Position)
	}
	if pref := s.VersionPref(f.p.ID, "tt0133093"); pref != uhd {
		t.Fatalf("preferred version = %d, want %d", pref, uhd)
	}

	// a different id ungroups it again
	if err := s.SetImdbID(uhd, "tt9999999"); err != nil {
		t.Fatal(err)
	}
	items, _, _ = s.Browse(f.p, RuleSet{}, 0, 100)
	found := false
	for _, it := range items {
		found = found || it.ID == uhd
	}
	if !found {
		t.Fatal("ungrouped version should show in the list again")
	}
}

// a film with two versions counts once in the genre chips, as the grid shows one card
func TestFacetsCountVersionsOnce(t *testing.T) {
	f := fixture(t)
	s := f.s
	matrix := f.ids["matrix"]
	res, err := s.DB.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,year,runtime_sec,height,width,path,size,added_at,updated_at)
		VALUES(?,'movie',?,?,?,1999,6000,2160,3840,?,1,?,?)`, f.libID, "/m/uhd.mkv", "The Matrix", "matrix", "/m/uhd.mkv", now(), now())
	if err != nil {
		t.Fatal(err)
	}
	uhd, _ := res.LastInsertId()
	for _, id := range []int64{matrix, uhd} {
		if err := s.SetImdbID(id, "tt0133093"); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		genres, _, err := s.Facets(f.p, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range genres {
			if g.Value == "FacetTest" {
				return g.Count
			}
		}
		return 0
	}
	for _, id := range []int64{matrix, uhd} {
		if _, err := s.DB.Exec(`INSERT INTO item_genres(item_id,genre) VALUES(?,'FacetTest')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(); n != 1 {
		t.Fatalf("genre count = %d, want 1", n)
	}
}
