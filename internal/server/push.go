package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ScotyLucas/kinora/internal/push"
	"github.com/ScotyLucas/kinora/internal/store"
)

// handles are 32 random bytes, base64url without padding
var reHandle = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// pushSession refuses requests that cannot own a pairing (stream links, no device).
func pushSession(w http.ResponseWriter, c *reqCtx) bool {
	if c.ViaStreamToken || c.DeviceID == "" {
		writeErr(w, http.StatusForbidden, "ehhez bejelentkezett eszköz kell")
		return false
	}
	return true
}

// handlePushIOS tells the app whether push is on and which relay and server id to
// register with. The server registers with the relay on first use.
func (s *Server) handlePushIOS(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	out := map[string]any{"enabled": false}
	if !s.Push.Relay.Enabled() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	id, err := s.Push.Relay.ServerID(r.Context())
	if err != nil {
		s.log.Warn("push: relay registration", "err", err)
		writeErr(w, http.StatusBadGateway, "a push-közvetítő nem érhető el")
		return
	}
	out["enabled"], out["relay_url"], out["server_id"] = true, s.Push.Relay.URL(), id
	out["categories"] = push.Categories
	if c.DeviceID != "" {
		p, _ := s.st.PushIOSFor(c.DeviceID)
		out["paired"] = p != nil
		if p != nil {
			muted := p.Muted
			if muted == nil {
				muted = []string{}
			}
			out["muted"], out["live"] = muted, p.Live
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// pushPrefsIn is what the app may send with a pairing and on its own.
type pushPrefsIn struct {
	Muted *[]string `json:"muted"`
	Live  *bool     `json:"live"`
}

// prefs turns the request into the device's preferences on top of old: the language comes from the
// client's UI language header, the title language from the profile's view of it.
func (s *Server) pushPrefs(r *http.Request, c *reqCtx, in pushPrefsIn, old *store.PushPrefs) (store.PushPrefs, bool) {
	var p store.PushPrefs
	if old != nil {
		p = *old
	}
	if lang := strings.TrimSpace(r.Header.Get(langHeader)); lang != "" && len(lang) <= 16 {
		p.Lang = lang
	}
	if c.Profile != nil {
		p.MetaLang = c.Profile.MetaLang
	}
	if in.Muted != nil {
		p.Muted = nil
		for _, m := range *in.Muted {
			if !slices.Contains(push.Categories, m) {
				return p, false
			}
			if !slices.Contains(p.Muted, m) {
				p.Muted = append(p.Muted, m)
			}
		}
	}
	if in.Live != nil {
		p.Live = *in.Live
	}
	return p, true
}

// handlePushIOSPrefs changes the language and the categories of this device's notifications.
func (s *Server) handlePushIOSPrefs(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if !pushSession(w, c) {
		return
	}
	var in pushPrefsIn
	if !decode(w, r, &in) {
		return
	}
	cur, err := s.st.PushIOSFor(c.DeviceID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	if cur == nil {
		writeErr(w, http.StatusNotFound, "ez az eszköz nincs párosítva")
		return
	}
	prefs, valid := s.pushPrefs(r, c, in, &cur.PushPrefs)
	if !valid {
		writeErr(w, http.StatusBadRequest, "ismeretlen értesítéstípus")
		return
	}
	if cur.Inbox() {
		prefs.Live = false // live activities go through the relay only
	}
	if _, err := s.st.SetPushPrefs(c.DeviceID, prefs); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// handlePushIOSPair stores the handle the relay gave the app and the key the app made.
func (s *Server) handlePushIOSPair(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if !pushSession(w, c) {
		return
	}
	if !s.Push.Relay.Enabled() {
		writeErr(w, http.StatusConflict, "a push nincs bekapcsolva ezen a szerveren")
		return
	}
	var in struct {
		Handle string `json:"handle"`
		Key    string `json:"key"`
		pushPrefsIn
	}
	if !decode(w, r, &in) {
		return
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(in.Key, "="))
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(in.Key)
	}
	if !reHandle.MatchString(in.Handle) || err != nil || len(key) != 32 {
		writeErr(w, http.StatusBadRequest, "hibás handle vagy kulcs")
		return
	}
	old, _ := s.st.PushIOSFor(c.DeviceID)
	var oldPrefs *store.PushPrefs
	if old != nil {
		oldPrefs = &old.PushPrefs
	}
	prefs, valid := s.pushPrefs(r, c, in.pushPrefsIn, oldPrefs)
	if !valid {
		writeErr(w, http.StatusBadRequest, "ismeretlen értesítéstípus")
		return
	}
	if err := s.st.SetPushIOS(c.DeviceID, c.User.ID, in.Handle, key, prefs); err != nil {
		s.internalErr(w, r, err)
		return
	}
	if old != nil && old.Handle != in.Handle {
		s.forgetHandle(old.Handle)
	}
	ok(w)
}

// handlePushIOSUnpair stops pushing to this device.
func (s *Server) handlePushIOSUnpair(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if !pushSession(w, c) {
		return
	}
	h, err := s.st.DeletePushIOS(c.DeviceID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	if h != "" {
		s.forgetHandle(h)
	}
	ok(w)
}

// forgetHandle tells the relay to drop a handle, in the background. An inbox handle was never the relay's.
func (s *Server) forgetHandle(h string) {
	if strings.HasPrefix(h, store.InboxPrefix) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.bgCtx, 20*time.Second)
		defer cancel()
		if err := s.Push.Relay.Delete(ctx, h); err != nil && !errors.Is(err, push.ErrGone) && !errors.Is(err, push.ErrDisabled) {
			s.log.Warn("push: relay delete", "err", err)
		}
	}()
}

// pushTexts caches the apps' texts per language for the notifier (re-read every few minutes, so a
// language the admin edits shows up without a restart).
type pushTexts struct {
	mu   sync.Mutex
	at   time.Time
	byLn map[string]map[string]any
}

// pushText returns the translator of one language: the "app" table of the language the apps use,
// keyed by the Hungarian source text. Unknown languages fall back to English, missing texts to Hungarian.
func (s *Server) pushText(lang string) func(string, ...any) string {
	s.pushTx.mu.Lock()
	if s.pushTx.byLn == nil || time.Since(s.pushTx.at) > 5*time.Minute {
		s.pushTx.byLn = map[string]map[string]any{}
		for _, l := range s.appLangs() {
			if app, ok := l.Strings["app"].(map[string]any); ok {
				s.pushTx.byLn[l.Code] = app
			}
		}
		s.pushTx.at = time.Now()
	}
	tbl, ok := s.pushTx.byLn[lang]
	if !ok {
		if base, _, cut := strings.Cut(lang, "-"); cut {
			tbl, ok = s.pushTx.byLn[base]
		}
	}
	if !ok && lang != "hu" {
		tbl = s.pushTx.byLn["en"]
	}
	s.pushTx.mu.Unlock()
	return func(hu string, args ...any) string {
		if t, ok := tbl[hu].(string); ok && t != "" {
			return push.Format(t, args...)
		}
		return push.Format(hu, args...)
	}
}

// --- Android: the app fetches its notifications --------------------------------------------------

// handlePushAndroid tells the Android app whether this device takes notifications and which it turned off.
// It needs no relay: the app asks for what is waiting (handlePushAndroidInbox).
func (s *Server) handlePushAndroid(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	out := map[string]any{"enabled": true, "categories": push.Categories, "paired": false, "muted": []string{}}
	if c.DeviceID != "" {
		if p, _ := s.st.PushIOSFor(c.DeviceID); p != nil && p.Inbox() {
			out["paired"] = true
			if p.Muted != nil {
				out["muted"] = p.Muted
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePushAndroidPair turns notifications on for this device: they wait on the server until the app
// fetches them. A relay pairing the device had before is replaced.
func (s *Server) handlePushAndroidPair(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if !pushSession(w, c) {
		return
	}
	var in pushPrefsIn
	if !decode(w, r, &in) {
		return
	}
	old, _ := s.st.PushIOSFor(c.DeviceID)
	var oldPrefs *store.PushPrefs
	if old != nil {
		oldPrefs = &old.PushPrefs
	}
	prefs, valid := s.pushPrefs(r, c, in, oldPrefs)
	if !valid {
		writeErr(w, http.StatusBadRequest, "ismeretlen értesítéstípus")
		return
	}
	prefs.Live = false
	handle := store.InboxPrefix + c.DeviceID
	if err := s.st.SetPushIOS(c.DeviceID, c.User.ID, handle, []byte{}, prefs); err != nil {
		s.internalErr(w, r, err)
		return
	}
	if old != nil && old.Handle != handle {
		s.forgetHandle(old.Handle)
	}
	ok(w)
}

// handlePushAndroidInbox hands over the notifications waiting for this device, oldest first, and forgets them.
func (s *Server) handlePushAndroidInbox(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if !pushSession(w, c) {
		return
	}
	p, err := s.st.PushIOSFor(c.DeviceID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	if p == nil || !p.Inbox() {
		writeErr(w, http.StatusNotFound, "ez az eszköz nincs párosítva")
		return
	}
	bodies, err := s.st.TakePushInbox(c.DeviceID)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	msgs := make([]json.RawMessage, 0, len(bodies))
	for _, b := range bodies {
		if json.Valid([]byte(b)) {
			msgs = append(msgs, json.RawMessage(b))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}
