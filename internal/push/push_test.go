package push_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ScotyLucas/kinora/internal/push"
	"github.com/ScotyLucas/kinora/internal/push/pushtest"
	"github.com/ScotyLucas/kinora/internal/store"
)

func setup(t *testing.T) (*store.Store, *pushtest.Relay, *push.Notifier) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rl := pushtest.New(t)
	st.SetSetting("push_enabled", "1")
	st.SetSetting("push_relay_url", rl.URL+"/")
	return st, rl, push.NewNotifier(st, push.NewRelay(st), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func pair(t *testing.T, st *store.Store, userID int64, handle string) []byte {
	t.Helper()
	return pairWith(t, st, userID, 0, handle, store.PushPrefs{})
}

func pairWith(t *testing.T, st *store.Store, userID, profileID int64, handle string, prefs store.PushPrefs) []byte {
	t.Helper()
	_, dev, err := st.CreateDeviceSession(userID, store.DeviceInfo{Platform: "ios"}, profileID, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{byte(len(handle))}, 32)
	key[0] = handle[0]
	if err := st.SetPushIOS(dev, userID, handle, key, prefs); err != nil {
		t.Fatal(err)
	}
	return key
}

// request adds a request in status and returns its id; a status change after it is what is pushed.
func request(t *testing.T, st *store.Store, userID int64, status string) int64 {
	t.Helper()
	now := time.Now().Unix()
	res, err := st.DB.Exec(`INSERT INTO acq_requests(kind,tmdb_id,title,user_id,status,created_at,updated_at) VALUES('movie',603,'Mátrix',?,?,?,?)`, userID, status, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func setStatus(t *testing.T, st *store.Store, id int64, status, note string) {
	t.Helper()
	if _, err := st.DB.Exec(`UPDATE acq_requests SET status=?, note=? WHERE id=?`, status, note, id); err != nil {
		t.Fatal(err)
	}
}

func open(t *testing.T, key, payload []byte, out any) {
	t.Helper()
	plain, err := pushtest.Open(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plain, out); err != nil {
		t.Fatal(err)
	}
}

func outbox(st *store.Store) int {
	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM push_outbox`).Scan(&n)
	return n
}

func TestSealMatchesCryptoKitLayout(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := push.Seal(key, []byte("szia"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 12+4+16 {
		t.Fatalf("len %d", len(sealed))
	}
	plain, err := pushtest.Open(key, sealed)
	if err != nil || string(plain) != "szia" {
		t.Fatalf("%q %v", plain, err)
	}
}

func TestRegistersOnceAndAgainWhenRelayChanges(t *testing.T) {
	st, rl, n := setup(t)
	ctx := context.Background()
	id1, err := n.Relay.ServerID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id2, _ := n.Relay.ServerID(ctx); id2 != id1 {
		t.Fatalf("re-registered: %s %s", id1, id2)
	}
	if _, _, regs := rl.Snapshot(); regs != 1 {
		t.Fatalf("registrations %d", regs)
	}
	other := pushtest.New(t)
	st.SetSetting("push_relay_url", other.URL)
	if _, err := n.Relay.ServerID(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, regs := other.Snapshot(); regs != 1 {
		t.Fatal("new relay not registered with")
	}
	st.SetSetting("push_enabled", "0")
	if _, err := n.Relay.ServerID(ctx); err != push.ErrDisabled {
		t.Fatalf("disabled: %v", err)
	}
}

func TestReRegistersWhenRelayForgetsServer(t *testing.T) {
	st, rl, n := setup(t)
	u, _ := st.CreateUser("anna", "secret123", false)
	pair(t, st, u.ID, strings.Repeat("a", 43))
	ctx := context.Background()
	if err := n.SendUser(ctx, u.ID, push.Message{V: 1, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	rl.Forget()
	if err := n.SendUser(ctx, u.ID, push.Message{V: 1, Title: "x"}); err == nil {
		t.Fatal("expected unauthorized")
	}
	if err := n.SendUser(ctx, u.ID, push.Message{V: 1, Title: "x"}); err != nil {
		t.Fatalf("after re-registration: %v", err)
	}
	if sent, _, regs := rl.Snapshot(); regs != 2 || len(sent) != 2 {
		t.Fatalf("regs %d sent %d", regs, len(sent))
	}
}

func TestTickPushesFulfilledRequestOnce(t *testing.T) {
	st, rl, n := setup(t)
	ctx := context.Background()
	anna, _ := st.CreateUser("anna", "secret123", false)
	bela, _ := st.CreateUser("bela", "secret123", false)
	h1, h2, hb := strings.Repeat("a", 43), strings.Repeat("b", 43), strings.Repeat("c", 43)
	k1 := pair(t, st, anna.ID, h1)
	pair(t, st, anna.ID, h2)
	pair(t, st, bela.ID, hb)
	rid := request(t, st, anna.ID, store.ReqDownloading)
	old := request(t, st, anna.ID, store.ReqDownloading)
	setStatus(t, st, old, store.ReqAvailable, "")
	st.DB.Exec(`UPDATE push_outbox SET created_at=? WHERE request_id=?`, time.Now().Add(-48*time.Hour).Unix(), old) // arrived while push was off
	setStatus(t, st, rid, store.ReqAvailable, "")

	n.Tick(ctx)
	n.Tick(ctx)
	sent, _, _ := rl.Snapshot()
	if len(sent) != 2 {
		t.Fatalf("sent %d, want one per paired device of the requester", len(sent))
	}
	for _, s := range sent {
		if s.Handle == hb {
			t.Fatal("pushed to another user")
		}
		if s.CollapseID != "req-"+itoa(rid) {
			t.Fatalf("collapse %q", s.CollapseID)
		}
	}
	var s pushtest.Sent
	for _, x := range sent {
		if x.Handle == h1 {
			s = x
		}
	}
	var m push.Message
	open(t, k1, s.Payload, &m)
	if m.V != 1 || m.Type != "request_available" || m.Title != "Mátrix" || !strings.Contains(m.Body, "film") {
		t.Fatalf("%+v", m)
	}
	if outbox(st) != 0 {
		t.Fatal("outbox not emptied")
	}
}

func TestTickKeepsPendingWhileRelayIsDown(t *testing.T) {
	st, rl, n := setup(t)
	u, _ := st.CreateUser("anna", "secret123", false)
	pair(t, st, u.ID, strings.Repeat("a", 43))
	rid := request(t, st, u.ID, store.ReqDownloading)
	setStatus(t, st, rid, store.ReqAvailable, "")
	rl.Fail(502)
	n.Tick(context.Background())
	if outbox(st) != 1 {
		t.Fatal("dropped although the relay failed")
	}
	rl.Fail(0)
	n.Tick(context.Background())
	if sent, _, _ := rl.Snapshot(); len(sent) != 1 {
		t.Fatalf("retry sent %d", len(sent))
	}
}

func TestRequestChangesInTheDevicesLanguage(t *testing.T) {
	st, rl, n := setup(t)
	n.Text = func(lang string) func(string, ...any) string {
		en := map[string]string{"Új kérés": "New request", "{0} kérte: {1}": "{0} asked for {1}",
			"Elutasították a kérésedet: {0}": "Your request was declined: {0}"}
		return func(hu string, args ...any) string {
			if lang == "en" && en[hu] != "" {
				hu = en[hu]
			}
			return push.Format(hu, args...)
		}
	}
	admin, _ := st.CreateUser("admin", "secret123", true)
	anna, _ := st.CreateUser("anna", "secret123", false)
	ka := pairWith(t, st, admin.ID, 0, strings.Repeat("a", 43), store.PushPrefs{Lang: "en"})
	kn := pairWith(t, st, anna.ID, 0, strings.Repeat("n", 43), store.PushPrefs{Lang: "en"})
	muted := pairWith(t, st, anna.ID, 0, strings.Repeat("m", 43), store.PushPrefs{Muted: []string{push.CatRequests}})
	_ = muted

	rid := request(t, st, anna.ID, store.ReqPending)
	n.Tick(context.Background())
	sent, _, _ := rl.Snapshot()
	if len(sent) != 1 || sent[0].Handle != strings.Repeat("a", 43) {
		t.Fatalf("new request went to %+v, want the admin only", sent)
	}
	var m push.Message
	open(t, ka, sent[0].Payload, &m)
	if m.Type != "request_new" || m.Title != "New request" || m.Body != "anna asked for Mátrix" {
		t.Fatalf("%+v", m)
	}

	setStatus(t, st, rid, store.ReqDenied, "már megvan DVD-n")
	n.Tick(context.Background())
	sent, _, _ = rl.Snapshot()
	if len(sent) != 2 || sent[1].Handle != strings.Repeat("n", 43) {
		t.Fatalf("denial went to %d devices", len(sent)-1)
	}
	open(t, kn, sent[1].Payload, &m)
	if m.Type != "request_denied" || m.Body != "Your request was declined: már megvan DVD-n" {
		t.Fatalf("%+v", m)
	}

	// approved after a failure (the next round) is not news, only approval of a new request is
	setStatus(t, st, rid, store.ReqFailed, "")
	setStatus(t, st, rid, store.ReqApproved, "")
	n.Tick(context.Background())
	sent, _, _ = rl.Snapshot()
	open(t, kn, sent[len(sent)-1].Payload, &m)
	if len(sent) != 3 || m.Type != "request_failed" {
		t.Fatalf("%d sent, last %+v", len(sent), m)
	}
}

func TestNewEpisodesGoToFollowersOnce(t *testing.T) {
	st, rl, n := setup(t)
	now := time.Now()
	clock := now
	n.SetClock(func() time.Time { return clock })
	u, _ := st.CreateUser("anna", "secret123", false)
	name := func(s string) *string { return &s }
	fan, err := st.CreateProfile(u.ID, store.ProfileInput{Name: name("Fan")})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := st.CreateProfile(u.ID, store.ProfileInput{Name: name("Other")})
	kf := pairWith(t, st, u.ID, fan.ID, strings.Repeat("f", 43), store.PushPrefs{})
	pairWith(t, st, u.ID, other.ID, strings.Repeat("o", 43), store.PushPrefs{})

	lib := int64(0)
	st.DB.QueryRow(`INSERT INTO libraries(name,kind,path,created_at) VALUES('Sorozatok','shows','/media',0) RETURNING id`).Scan(&lib)
	item := func(kind string, series any, season, ep any, title string, added int64) int64 {
		var id int64
		err := st.DB.QueryRow(`INSERT INTO items(library_id,kind,key,title,sort_title,series_id,season_num,episode_num,added_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?) RETURNING id`, lib, kind, title+itoa(added)+kind, title, title, series, season, ep, added, added).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	long := now.Add(-time.Hour).Unix()
	sid := item("series", nil, nil, nil, "Dark", long)
	st.DB.Exec(`UPDATE items SET series_id=id WHERE id=?`, sid)
	e1 := item("episode", sid, 1, 1, "Secrets", long)
	st.DB.Exec(`INSERT INTO user_data(profile_id,item_id,played) VALUES(?,?,1)`, fan.ID, e1)
	st.SetSetting("push_episode_id", itoa(e1)) // where migration 18 starts: the library before push is not news

	item("episode", sid, 1, 2, "Lies", now.Unix())
	item("episode", sid, 1, 3, "Past and Present", now.Unix())
	item("episode", sid, 1, 1, "Secrets", now.Unix()) // a second copy of an episode it had
	n.Tick(context.Background())
	if sent, _, _ := rl.Snapshot(); len(sent) != 0 {
		t.Fatal("sent while the season was still arriving")
	}
	clock = now.Add(3 * time.Minute)
	n.Tick(context.Background())
	n.Tick(context.Background())
	sent, _, _ := rl.Snapshot()
	if len(sent) != 1 || sent[0].Handle != strings.Repeat("f", 43) {
		t.Fatalf("sent %+v, want one to the follower", sent)
	}
	var m push.Message
	open(t, kf, sent[0].Payload, &m)
	if m.Type != "episodes" || m.Title != "Dark" || m.Body != "2 új rész" || m.Item != sid {
		t.Fatalf("%+v", m)
	}
}

func TestLiveDownloadStartsUpdatesAndEnds(t *testing.T) {
	st, rl, n := setup(t)
	now := time.Now()
	clock := now
	n.SetClock(func() time.Time { return clock })
	u, _ := st.CreateUser("anna", "secret123", false)
	h := strings.Repeat("l", 43)
	key := pairWith(t, st, u.ID, 0, h, store.PushPrefs{Live: true})
	pair(t, st, u.ID, strings.Repeat("x", 43)) // a device without live activities
	rid := request(t, st, u.ID, store.ReqDownloading)
	var gid int64
	if err := st.DB.QueryRow(`INSERT INTO acq_grabs(title,release_title,info_hash,status,progress,eta,request_id,created_at,updated_at)
		VALUES('Mátrix','Matrix.1999','abc','downloading',0.1,600,?,0,0) RETURNING id`, rid).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	rl.SetLiveToken(h, "", true)

	n.Tick(context.Background())
	lives := rl.LiveSnapshot()
	if len(lives) != 1 || lives[0].Event != "start" || lives[0].Ref != "req-"+itoa(rid) || lives[0].Priority != 10 {
		t.Fatalf("%+v", lives)
	}
	var ls push.LiveState
	open(t, key, lives[0].Payload, &ls)
	if ls.Title != "Mátrix" || ls.State != "downloading" || ls.Progress != 0.1 || ls.ETA != 600 {
		t.Fatalf("%+v", ls)
	}

	// no activity token yet: nothing to update, the progress waits
	st.DB.Exec(`UPDATE acq_grabs SET progress=0.5 WHERE id=?`, gid)
	clock = now.Add(2 * time.Minute)
	n.Tick(context.Background())
	if len(rl.LiveSnapshot()) != 1 {
		t.Fatal("updated without a token")
	}
	rl.SetLiveToken(h, "req-"+itoa(rid), true)
	n.Tick(context.Background())
	clock = now.Add(150 * time.Second) // too soon for the next one
	st.DB.Exec(`UPDATE acq_grabs SET progress=0.6 WHERE id=?`, gid)
	n.Tick(context.Background())
	lives = rl.LiveSnapshot()
	if len(lives) != 2 || lives[1].Event != "update" || lives[1].Priority != 5 {
		t.Fatalf("%+v", lives)
	}
	open(t, key, lives[1].Payload, &ls)
	if ls.Progress != 0.5 {
		t.Fatalf("%+v", ls)
	}

	setStatus(t, st, rid, store.ReqAvailable, "")
	st.DB.Exec(`UPDATE acq_grabs SET status='done' WHERE id=?`, gid)
	n.Tick(context.Background())
	n.Tick(context.Background())
	lives = rl.LiveSnapshot()
	if len(lives) != 3 || lives[2].Event != "end" || lives[2].DismissalDate <= clock.Unix() {
		t.Fatalf("%+v", lives)
	}
	open(t, key, lives[2].Payload, &ls)
	if ls.State != "available" || ls.Progress != 1 {
		t.Fatalf("%+v", ls)
	}
}

func TestGoneHandleIsDropped(t *testing.T) {
	st, rl, n := setup(t)
	u, _ := st.CreateUser("anna", "secret123", false)
	h := strings.Repeat("a", 43)
	pair(t, st, u.ID, h)
	rl.SetGone(h)
	if err := n.SendUser(context.Background(), u.ID, push.Message{V: 1, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if st.CountPushIOS() != 0 {
		t.Fatal("gone handle kept")
	}
}

func TestLongMessageIsShortened(t *testing.T) {
	st, rl, n := setup(t)
	u, _ := st.CreateUser("anna", "secret123", false)
	pair(t, st, u.ID, strings.Repeat("a", 43))
	if err := n.SendUser(context.Background(), u.ID, push.Message{V: 1, Title: strings.Repeat("ő", 900), Body: strings.Repeat("é", 2000)}); err != nil {
		t.Fatal(err)
	}
	if sent, _, _ := rl.Snapshot(); len(sent) != 1 || len(sent[0].Payload) > 2048 {
		t.Fatal("not sent within the limit")
	}
}

func itoa(n int64) string { return fmtInt(n) }

func fmtInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestRecapIsToldOncePerProfile(t *testing.T) {
	st, rl, n := setup(t)
	clock := time.Date(2026, time.November, 2, 11, 0, 0, 0, time.Local)
	n.SetClock(func() time.Time { return clock })
	u, _ := st.CreateUser("anna", "secret123", false)
	name := func(s string) *string { return &s }
	p, _ := st.CreateProfile(u.ID, store.ProfileInput{Name: name("Anna")})
	idle, _ := st.CreateProfile(u.ID, store.ProfileInput{Name: name("Idle")})
	k := pairWith(t, st, u.ID, p.ID, strings.Repeat("r", 43), store.PushPrefs{})
	pairWith(t, st, u.ID, idle.ID, strings.Repeat("i", 43), store.PushPrefs{})
	watched := func(at time.Time) {
		if _, err := st.DB.Exec(`INSERT INTO watch_events(profile_id,item_id,kind,title,runtime_sec,watched_at) VALUES(?,NULL,'movie','Heat',6000,?)`, p.ID, at.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	watched(time.Date(2026, time.October, 12, 20, 0, 0, 0, time.Local))
	watched(time.Date(2025, time.March, 3, 20, 0, 0, 0, time.Local))

	n.Tick(context.Background())
	n.Tick(context.Background())
	sent, _, _ := rl.Snapshot()
	if len(sent) != 1 || sent[0].Handle != strings.Repeat("r", 43) {
		t.Fatalf("sent %+v, want one to the profile that watched", sent)
	}
	var m push.Message
	open(t, k, sent[0].Payload, &m)
	if m.Type != "recap" || !strings.Contains(m.Body, "októberben") || m.Href != "#/journal?p=year&b=9" {
		t.Fatalf("%+v", m)
	}

	clock = time.Date(2026, time.November, 9, 11, 0, 0, 0, time.Local) // past the first week: nothing
	n.Tick(context.Background())
	clock = time.Date(2027, time.January, 3, 9, 0, 0, 0, time.Local) // too early in the day
	n.Tick(context.Background())
	if sent, _, _ := rl.Snapshot(); len(sent) != 1 {
		t.Fatalf("sent %d, want still 1", len(sent))
	}
	clock = time.Date(2026, time.January, 3, 11, 0, 0, 0, time.Local) // January: the year, not December
	n.Tick(context.Background())
	n.Tick(context.Background())
	sent, _, _ = rl.Snapshot()
	if len(sent) != 2 {
		t.Fatalf("sent %d, want 2", len(sent))
	}
	open(t, k, sent[1].Payload, &m)
	if m.Type != "recap_year" || !strings.Contains(m.Title, "2025") || m.Href != "#/journal?p=last&recap=1" {
		t.Fatalf("%+v", m)
	}
}
