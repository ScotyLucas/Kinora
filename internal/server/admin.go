package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ScotyLucas/kinora/internal/meta"
	"github.com/ScotyLucas/kinora/internal/safe"
	"github.com/ScotyLucas/kinora/internal/store"
)

// ------------------------------------------------------------------ users

type adminUser struct {
	*store.User
	Profiles int `json:"profiles"`
	// NeedsPassword: an imported account whose password link was not used yet
	NeedsPassword bool `json:"needs_password"`
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	users, err := s.st.ListUsers()
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	out := []adminUser{}
	for _, u := range users {
		ps, _ := s.st.Profiles(u.ID)
		out = append(out, adminUser{u, len(ps), !u.HasPassword()})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if !decode(w, r, &in) {
		return
	}
	if msg := validCredentials(in.Username, in.Password); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.st.CreateUser(strings.TrimSpace(in.Username), in.Password, in.IsAdmin)
	if errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := pathID(r, "id")
	if _, err := s.st.UserByID(id); err != nil {
		writeErr(w, http.StatusNotFound, "a felhasználó nem található")
		return
	}
	var in struct {
		Password  *string         `json:"password"`
		IsAdmin   *bool           `json:"is_admin"`
		Libraries json.RawMessage `json:"libraries"` // null = all libraries, [ids] = only these
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Libraries) > 0 {
		var ids []int64
		if err := json.Unmarshal(in.Libraries, &ids); err != nil {
			writeErr(w, http.StatusBadRequest, "érvénytelen könyvtárlista")
			return
		}
		if err := s.st.SetUserLibraries(id, ids); err != nil {
			s.internalErr(w, r, err)
			return
		}
	}
	if in.Password != nil {
		if msg := validCredentials("placeholder", *in.Password); msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		keep := ""
		if id == c.User.ID {
			keep = c.Token
		}
		if err := s.st.SetPassword(id, *in.Password, keep); err != nil {
			s.internalErr(w, r, err)
			return
		}
	}
	if in.IsAdmin != nil {
		if id == c.User.ID && !*in.IsAdmin {
			writeErr(w, http.StatusBadRequest, "saját magadtól nem veheted el az adminjogot")
			return
		}
		s.st.SetAdmin(id, *in.IsAdmin)
	}
	ok(w)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := pathID(r, "id")
	if id == c.User.ID {
		writeErr(w, http.StatusBadRequest, "saját magadat nem törölheted")
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		s.internalErr(w, r, err)
		return
	}
	ok(w)
}

// ------------------------------------------------------------------ invites

func (s *Server) handleInvites(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	inv, err := s.st.Invites()
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleInviteCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Note  string `json:"note"`
		Uses  int    `json:"uses"`
		Hours int    `json:"hours"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Uses > 100 {
		in.Uses = 100
	}
	inv, err := s.st.CreateInvite(c.User.ID, strings.TrimSpace(in.Note), in.Uses, in.Hours)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) handleInviteDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	s.st.DeleteInvite(r.PathValue("code"))
	ok(w)
}

// ------------------------------------------------------------------ settings

var settingKeys = []string{"server_name", "public_url", "scan_interval_min", "tmdb_key", "metadata_lang", "encoder", "video_codec",
	"quickconnect_enabled", "discovery_enabled", "brand_accent", "intro_detection", "intro_window_min", "trickplay",
	"opensubtitles_key", "opensubtitles_user", "opensubtitles_pass", "watch_region", "omdb_key", "push_enabled", "push_relay_url",
	"jellyfin_compat"}

// secretSettings are shown masked and kept when the masked value comes back.
var secretSettings = map[string]bool{"tmdb_key": true, "opensubtitles_key": true, "opensubtitles_pass": true, "omdb_key": true}

// SettingEnv maps settings to the environment variables that can force them.
var SettingEnv = map[string]string{
	"server_name":          "KINORA_SERVER_NAME",
	"public_url":           "KINORA_PUBLIC_URL",
	"scan_interval_min":    "KINORA_SCAN_INTERVAL_MIN",
	"tmdb_key":             "KINORA_TMDB_KEY",
	"metadata_lang":        "KINORA_METADATA_LANG",
	"encoder":              "KINORA_ENCODER",
	"video_codec":          "KINORA_VIDEO_CODEC",
	"quickconnect_enabled": "KINORA_QUICKCONNECT",
	"discovery_enabled":    "KINORA_DISCOVERY",
	"brand_accent":         "KINORA_ACCENT",
	"intro_detection":      "KINORA_INTRO_DETECTION",
	"intro_window_min":     "KINORA_INTRO_WINDOW_MIN",
	"trickplay":            "KINORA_TRICKPLAY",
	"opensubtitles_key":    "KINORA_OPENSUBTITLES_KEY",
	"opensubtitles_user":   "KINORA_OPENSUBTITLES_USER",
	"opensubtitles_pass":   "KINORA_OPENSUBTITLES_PASS",
	"watch_region":         "KINORA_WATCH_REGION",
	"omdb_key":             "KINORA_OMDB_KEY",
	"push_enabled":         "KINORA_PUSH",
	"push_relay_url":       "KINORA_PUSH_RELAY",
}

const maskChar = "•"

func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return strings.Repeat(maskChar, 4)
	}
	return strings.Repeat(maskChar, 8) + v[len(v)-4:]
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	vals := map[string]string{}
	env := map[string]string{}
	for _, k := range settingKeys {
		v := s.st.Setting(k)
		if secretSettings[k] {
			v = maskSecret(v)
		}
		if k == "jellyfin_compat" && v == "" {
			v = "0" // off unless switched on (the toggle reads an empty value as on)
		}
		vals[k] = v
		if s.st.Overridden(k) {
			env[k] = SettingEnv[k]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": vals, "env": env, "jellyfin_compat_addr": JFCompatAddr})
}

func (s *Server) handleSettingsPatch(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	var changed []string
	oldLang := s.st.Setting("metadata_lang")
	for _, k := range settingKeys {
		v, present := in[k]
		if !present {
			continue
		}
		v = strings.TrimSpace(v)
		if s.st.Overridden(k) {
			continue // environment wins; the UI shows the field as locked
		}
		switch k {
		case "tmdb_key", "opensubtitles_key", "opensubtitles_pass", "omdb_key":
			if strings.Contains(v, maskChar) {
				continue // masked value echoed back: keep the stored key
			}
			if k != "opensubtitles_pass" {
				v = meta.ExtractKey(v) // a whole example URL was pasted: keep only its key
			}
		case "scan_interval_min":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 10080 {
				writeErr(w, http.StatusBadRequest, "a szkennelési intervallum 0–10080 perc lehet (0 = kikapcsolva)")
				return
			}
		case "public_url":
			if v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				writeErr(w, http.StatusBadRequest, "a nyilvános URL http:// vagy https:// kezdetű legyen")
				return
			}
			v = strings.TrimRight(v, "/")
		case "push_relay_url":
			if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
				writeErr(w, http.StatusBadRequest, "a push-közvetítő URL-je http:// vagy https:// kezdetű legyen")
				return
			}
			v = strings.TrimRight(v, "/")
		case "intro_window_min":
			n, err := strconv.Atoi(v)
			if err != nil || n < 2 || n > 30 {
				writeErr(w, http.StatusBadRequest, "az intró keresési ablaka 2–30 perc lehet")
				return
			}
		case "quickconnect_enabled", "discovery_enabled", "intro_detection", "push_enabled", "trickplay":
			if v != "0" && v != "1" {
				writeErr(w, http.StatusBadRequest, "a kapcsoló értéke 0 vagy 1 lehet")
				return
			}
		case "brand_accent":
			if v != "" && !reHexColor.MatchString(v) {
				writeErr(w, http.StatusBadRequest, "az akcentszín #rrggbb formátumú legyen")
				return
			}
		case "watch_region":
			v = strings.ToUpper(v)
			if v != "" && !reRegion.MatchString(v) {
				writeErr(w, http.StatusBadRequest, "az ország kétbetűs kód legyen (pl. HU), vagy üres")
				return
			}
		case "server_name":
			if v == "" {
				writeErr(w, http.StatusBadRequest, "a szerver neve nem lehet üres")
				return
			}
		}
		if err := s.st.SetSetting(k, v); err != nil {
			s.internalErr(w, r, err)
			return
		}
		changed = append(changed, k)
	}
	if len(changed) > 0 {
		s.log.Info("settings changed", "keys", strings.Join(changed, ","))
	}
	if s.st.Setting("metadata_lang") != oldLang && s.Meta.Enabled() {
		// re-fetch matched titles in the new language; a refresh already running picks it up when it ends
		go func() {
			defer safe.Recover(s.log, "metadata-refresh")
			if err := s.Meta.Refresh(s.bgCtx, false); err != nil && !errors.Is(err, meta.ErrRunning) {
				s.log.Warn("metadata refresh", "err", err)
			}
		}()
	}
	s.handleSettings(w, r, c)
}

// handleKeyTest checks whether an API key works. The key in the body is the one in the form
// (it may be unsaved); empty or masked means the stored one.
func (s *Server) handleKeyTest(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Service string `json:"service"` // tmdb | omdb | opensubtitles
		Key     string `json:"key"`
		User    string `json:"user"`
		Pass    string `json:"pass"`
	}
	if !decode(w, r, &in) {
		return
	}
	pick := func(v, setting string) string {
		if v = strings.TrimSpace(v); v == "" || strings.Contains(v, maskChar) {
			return strings.TrimSpace(s.st.Setting(setting))
		}
		return v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var res meta.KeyCheck
	switch in.Service {
	case "tmdb", "omdb":
		if s.Meta == nil {
			writeErr(w, http.StatusServiceUnavailable, "a metaadat-modul nem fut")
			return
		}
		if in.Service == "tmdb" {
			res = s.Meta.CheckTMDB(ctx, pick(in.Key, "tmdb_key"))
		} else {
			res = s.Meta.CheckOmdb(ctx, pick(in.Key, "omdb_key"))
		}
	case "opensubtitles":
		res = s.OpenSubs.CheckKey(ctx, pick(in.Key, "opensubtitles_key"), pick(in.User, "opensubtitles_user"),
			pick(in.Pass, "opensubtitles_pass"))
	default:
		writeErr(w, http.StatusBadRequest, "ismeretlen szolgáltatás")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ------------------------------------------------------------------ backup

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	tmp := filepath.Join(s.cfg.CacheDir, fmt.Sprintf("backup-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmp)
	if err := s.st.Backup(r.Context(), tmp); err != nil {
		s.internalErr(w, r, fmt.Errorf("mentés sikertelen: %w", err))
		return
	}
	f, err := os.Open(tmp)
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	defer f.Close()
	name := "kinora-backup-" + time.Now().Format("20060102-150405") + ".db"
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, time.Now(), f)
}

// ------------------------------------------------------------------ connect

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	_, port, _ := net.SplitHostPort(s.cfg.Addr)
	if port == "" {
		port = "8096"
	}
	type addr struct {
		Iface string `json:"iface"`
		URL   string `json:"url"`
		Kind  string `json:"kind"` // lan | tailscale | other
	}
	addrs := []addr{}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := ifc.Addrs()
		for _, a := range as {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
				continue
			}
			kind := "other"
			ip := ipn.IP.To4()
			switch {
			case isTailscaleIP(ip): // 100.64.0.0/10 CGNAT = Tailscale
				kind = "tailscale"
			case ip.IsPrivate():
				kind = "lan"
			}
			addrs = append(addrs, addr{Iface: ifc.Name, URL: "http://" + ip.String() + ":" + port, Kind: kind})
		}
	}
	sort.SliceStable(addrs, func(i, j int) bool { return addrs[i].Kind < addrs[j].Kind })
	writeJSON(w, http.StatusOK, map[string]any{
		"public_url":   s.st.Setting("public_url"),
		"addresses":    addrs,
		"port":         port,
		"in_container": fileExists("/.dockerenv"),
	})
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ------------------------------------------------------------------ folder picker

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = s.cfg.MediaRoot
		if (p == "" || !fileExists(p)) && installMode() != "docker" {
			p = browseRoot() // native install: / (or the system drive), shortcuts lead to the other roots / drives
		}
		if p == "" || !fileExists(p) {
			p, _ = os.UserHomeDir()
		}
		if p == "" {
			p = "/"
		}
	}
	p = filepath.Clean(p)
	entries, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "a mappa nem olvasható: "+err.Error())
		return
	}
	dirs := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(p, e.Name()))) {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j]) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	// folders the service user cannot enter are shown locked, so the admin knows before adding them
	locked := []string{}
	for _, d := range dirs {
		if f, err := os.Open(filepath.Join(p, d)); err != nil {
			locked = append(locked, d)
		} else {
			f.Close()
		}
	}
	mode := installMode()
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "parent": parent, "dirs": dirs, "locked": locked, "sep": string(filepath.Separator),
		"mode": mode, "shortcuts": browseShortcuts(mode, s.cfg.MediaRoot), "host_path": hostPath(mode, p)})
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// ------------------------------------------------------------------ dashboard

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d := map[string]any{
		"version":    Version,
		"go":         runtime.Version(),
		"os":         runtime.GOOS + "/" + runtime.GOARCH,
		"uptime_sec": int(time.Since(s.started).Seconds()),
		"started_at": s.started.Unix(),
		"schema":     s.st.SchemaVersion(),
		"update":     s.updates.Status(s.bgCtx),
	}
	s.dashboardExtras(d)
	writeJSON(w, http.StatusOK, d)
}

// handleUpdateApply is the dashboard's "update now" button on Windows: it starts update.ps1 in the background.
// The service stops for the swap, so the answer comes before that; the dashboard then waits for the new version.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if st := s.updates.Status(s.bgCtx); !st.Available {
		s.updates.CheckNow(r.Context()) // the dashboard may be showing an older answer than this server has
	}
	if ok, reason := s.updates.Apply(); !ok {
		writeErr(w, http.StatusConflict, reason)
		return
	}
	s.log.Info("update started from the dashboard", "user", c.User.Username)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// handleUpdateCheck is the dashboard's "check now" button: it asks GitHub for the latest release and returns the result.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	writeJSON(w, http.StatusOK, s.updates.CheckNow(r.Context()))
}
