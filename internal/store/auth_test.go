package store

import (
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPasswordHash(t *testing.T) {
	h := HashPassword("titok123")
	if !CheckPassword(h, "titok123") || CheckPassword(h, "titok124") {
		t.Fatal("password check broken")
	}
	if h == HashPassword("titok123") {
		t.Fatal("salt not random")
	}
}

func TestUsersSessionsProfiles(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("Admin", "secret1", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("admin", "x123456", false); err != ErrExists {
		t.Fatalf("case-insensitive unique expected, got %v", err)
	}
	if _, err := s.Authenticate("ADMIN", "secret1"); err != nil {
		t.Fatal("login failed", err)
	}
	if _, err := s.Authenticate("admin", "bad"); err == nil {
		t.Fatal("bad password accepted")
	}
	tok, _ := s.CreateSession(u.ID)
	gu, gp, err := s.Session(tok)
	if err != nil || gu.ID != u.ID || gp != nil {
		t.Fatal("session lookup", err)
	}
	ps, _ := s.Profiles(u.ID)
	if len(ps) != 1 {
		t.Fatalf("default profile expected, got %d", len(ps))
	}
	name, pin, kids := "Gyerek", "1234", true
	kp, err := s.CreateProfile(u.ID, ProfileInput{Name: &name, PIN: &pin, IsKids: &kids})
	if err != nil {
		t.Fatal(err)
	}
	if !kp.HasPIN || !kp.CheckPIN("1234") || kp.CheckPIN("0000") {
		t.Fatal("pin")
	}
	if lim, strict := kp.RatingLimit(); lim != 1 || !strict {
		t.Fatalf("kids limit = %d %v", lim, strict)
	}
	if ps[0].Snow || kp.Snow {
		t.Fatal("snowfall should be off by default")
	}
	on, off := true, false
	if err := s.UpdateProfile(kp.ID, ProfileInput{Snow: &on}); err != nil {
		t.Fatal(err)
	}
	if kp, _ = s.Profile(kp.ID); !kp.Snow {
		t.Fatal("snowfall not switched on")
	}
	if err := s.UpdateProfile(kp.ID, ProfileInput{Snow: &off}); err != nil {
		t.Fatal(err)
	}
	if kp, _ = s.Profile(kp.ID); kp.Snow {
		t.Fatal("snowfall not switched off")
	}
	s.SetSessionProfile(tok, kp.ID)
	_, gp, _ = s.Session(tok)
	if gp == nil || gp.ID != kp.ID {
		t.Fatal("profile select")
	}
	for i := 0; i < MaxProfiles-2; i++ {
		n := "p"
		if _, err := s.CreateProfile(u.ID, ProfileInput{Name: &n}); err != nil {
			t.Fatal(err)
		}
	}
	n := "too many"
	if _, err := s.CreateProfile(u.ID, ProfileInput{Name: &n}); err == nil {
		t.Fatal("profile limit not enforced")
	}
	s.DeleteSession(tok)
	if _, _, err := s.Session(tok); err == nil {
		t.Fatal("session survived logout")
	}
}

func TestInvites(t *testing.T) {
	s := testStore(t)
	a, _ := s.CreateUser("admin", "secret1", true)
	inv, _ := s.CreateInvite(a.ID, "család", 1, 24)
	if _, err := s.RedeemInvite(inv.Code, "anna", "secret2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemInvite(inv.Code, "bela", "secret2"); err != ErrInviteInvalid {
		t.Fatalf("used invite accepted: %v", err)
	}
	if n, _ := s.CountUsers(); n != 2 {
		t.Fatal("user count", n)
	}
}

func TestSettingsOverride(t *testing.T) {
	s := testStore(t)
	if s.Setting("metadata_lang") != "hu-HU" {
		t.Fatal("default")
	}
	s.SetSetting("server_name", "Otthon")
	if s.Setting("server_name") != "Otthon" {
		t.Fatal("stored")
	}
	s.SetOverride("server_name", "Env")
	if s.Setting("server_name") != "Env" || !s.Overridden("server_name") {
		t.Fatal("override")
	}
}
