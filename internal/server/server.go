// Package server is Kinora's HTTP API and web client host.
//
// Jellyfin problems it addresses: confusing user onboarding and remote access
// (invite links with QR codes, a "Connect" page listing every reachable
// address), no real household profiles / kids mode (profiles with PIN and a
// rating filter applied to *every* endpoint), and opaque transcoding (the
// playback decision returns human readable reasons).
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ScotyLucas/kinora/internal/acquire"
	"github.com/ScotyLucas/kinora/internal/intro"
	"github.com/ScotyLucas/kinora/internal/media"
	"github.com/ScotyLucas/kinora/internal/meta"
	"github.com/ScotyLucas/kinora/internal/party"
	"github.com/ScotyLucas/kinora/internal/push"
	"github.com/ScotyLucas/kinora/internal/scanner"
	"github.com/ScotyLucas/kinora/internal/store"
	"github.com/ScotyLucas/kinora/internal/trickplay"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Config holds runtime configuration (flags / environment).
type Config struct {
	Addr      string
	DataDir   string
	CacheDir  string
	WebDir    string // serve the web client from disk (development)
	MediaRoot string // default start folder of the admin folder picker
	// CORSOrigins lists extra app origins allowed for Bearer-token CORS (comma separated)
	CORSOrigins string
	// DiscoveryPort is the UDP port of LAN server discovery (0 = off)
	DiscoveryPort int
	// AllowRemoteSetup lets the first admin be created from any address (KINORA_ALLOW_REMOTE_SETUP=1);
	// by default only from the local network, loopback or Tailscale (setupAllowed).
	AllowRemoteSetup bool
}

// Server is the HTTP application.
type Server struct {
	cfg     Config
	st      *store.Store
	log     *slog.Logger
	started time.Time
	mux     *http.ServeMux
	limiter *rateLimiter
	qc      *quickConnect
	updates *releaseChecker

	serverIDMu    sync.Mutex
	streamKeyOnce sync.Once
	streamKey     []byte
	extraOrigins  map[string]bool
	legacy        legacyJS

	Deps
	bgCtx      context.Context
	pushTx     pushTexts  // the notifier's texts per language
	imports    importRuns // Jellyfin import previews
	arrImports arrRuns    // Sonarr / Radarr / Prowlarr import previews
}

// Deps are the subsystems the server drives (wired in main).
type Deps struct {
	Ctx       context.Context // cancelled on shutdown; background work started by requests uses it
	Scanner   *scanner.Scanner
	ImgDir    string
	Media     *media.Manager
	Encoders  *media.Encoders
	Subs      *media.Subtitles
	Downloads *media.Downloader
	Party     *party.Hub
	Meta      *meta.Fetcher
	Intro     *intro.Analyzer      // skip intro / credits detection (nil = off)
	Trickplay *trickplay.Generator // timeline preview pictures (nil = off)
	OpenSubs  *meta.OpenSubtitles  // subtitle search and download (created by New when nil)
	Acquire   *acquire.Manager     // built-in acquisition (created by New when nil and Meta is set; main runs it)
	Push      *push.Notifier       // iOS push through the relay (created by New when nil; main runs it)
}

// New builds the server and its routes.
func New(cfg Config, st *store.Store, log *slog.Logger, deps Deps) *Server {
	s := &Server{cfg: cfg, st: st, log: log, started: time.Now(), mux: http.NewServeMux(), limiter: newRateLimiter(), Deps: deps, bgCtx: deps.Ctx,
		qc: newQuickConnect(), updates: newReleaseChecker(), extraOrigins: parseOrigins(cfg.CORSOrigins)}
	if s.bgCtx == nil {
		s.bgCtx = context.Background()
	}
	if s.OpenSubs == nil {
		s.OpenSubs = meta.NewOpenSubtitles(st, "Kinora v"+Version)
	}
	if s.Push == nil {
		s.Push = push.NewNotifier(st, push.NewRelay(st), log)
	}
	if s.Push.Text == nil {
		s.Push.Text = s.pushText
	}
	if s.Acquire == nil && s.Meta != nil {
		s.Acquire = acquire.New(st, s.Meta, func(libID int64) {
			if s.Scanner != nil {
				s.Scanner.Trigger(s.bgCtx, libID)
			}
		}, log)
	}
	s.routes()
	return s
}

// Handler returns the root handler with global middleware applied.
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.logRequests(csrfGuard(s.cors(s.mux))))
}

// ------------------------------------------------------------------ context

type reqCtx struct {
	User        *store.User
	Profile     *store.Profile
	Token       string
	DeviceID    string
	ProfileLock int64 // >0: session pinned to this profile (Quick Connect kids device)
	// set when the request was authorised by a ?st= stream token instead of a session
	ViaStreamToken bool
	StreamItem     int64 // item the stream token is scoped to (0 = artwork only)
}

// tokenAllows reports whether a stream-token request may touch itemID.
func (c *reqCtx) tokenAllows(itemID int64) bool { return !c.ViaStreamToken || c.StreamItem == itemID }

type handler func(w http.ResponseWriter, r *http.Request, c *reqCtx)

const cookieName = "kinora_session"

// tokenFrom returns the session token: the Bearer header, or the cookie for
// same-origin web requests. Cross-origin app requests never use the cookie
// (the CORS middleware strips it), so cookie auth cannot ride along with CORS.
func tokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) resolve(r *http.Request) (*reqCtx, error) {
	tok := tokenFrom(r)
	ss, err := s.st.SessionFull(tok, clientIP(r))
	if err != nil {
		return &reqCtx{Token: tok}, err
	}
	if ss.Profile != nil && s.Meta != nil {
		// titles and overviews follow the client's UI language
		if code := r.Header.Get(langHeader); code != "" {
			ss.Profile.MetaLang = s.Meta.ViewLang(s.bgCtx, code)
		}
	}
	return &reqCtx{User: ss.User, Profile: ss.Profile, Token: tok, DeviceID: ss.DeviceID, ProfileLock: ss.ProfileLock}, nil
}

// langHeader carries the client's UI language (hu, en, …); metadata is returned in it when fetched.
const langHeader = "X-Kinora-Lang"

func (s *Server) public(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, _ := s.resolve(r)
		h(w, r, c)
	}
}

func (s *Server) authed(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := s.resolve(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "bejelentkezés szükséges")
			return
		}
		h(w, r, c)
	}
}

func (s *Server) withProfile(h handler) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		if c.Profile == nil {
			writeErr(w, http.StatusPreconditionRequired, "válassz profilt")
			return
		}
		h(w, r, c)
	})
}

func (s *Server) admin(h handler) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		if !c.User.IsAdmin || c.ProfileLock > 0 {
			writeErr(w, http.StatusForbidden, "ehhez adminisztrátori jog kell")
			return
		}
		h(w, r, c)
	})
}

// notKids rejects modifications from kids profiles.
func notKids(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, c *reqCtx) {
		if c.Profile != nil && c.Profile.IsKids {
			writeErr(w, http.StatusForbidden, "gyerekprofilból ez nem módosítható")
			return
		}
		h(w, r, c)
	}
}

// ------------------------------------------------------------------ helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func ok(w http.ResponseWriter) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) }

// internalErr logs the real error and returns a generic message with a reference that appears in the
// log line. The error text itself (file paths, SQL, upstream URLs) never reaches the client.
func (s *Server) internalErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "nem található")
		return
	}
	ref := store.RandomToken(4)
	s.log.Error("request failed", "path", r.URL.Path, "ref", ref, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "belső hiba (hivatkozás a naplóban: " + ref + ")", "ref": ref})
}

const maxBody = 1 << 20

// decode reads a JSON body (max 1 MB); on failure it writes a 400 and returns false.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, "a kérés túl nagy (max. 1 MB)")
		} else {
			writeErr(w, http.StatusBadRequest, "hibás kérés: "+err.Error())
		}
		return false
	}
	return true
}

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// clientIP returns the caller IP; forwarding headers are trusted only from private/loopback peers.
// X-Forwarded-For is read from the right: each proxy appends the address it saw, so the rightmost
// public entry was written by our own proxy, while entries left of it come from the client and can
// be forged (a forged leftmost entry would otherwise reset every per-IP rate limit).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trustedHop(host) {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			hops := strings.Split(strings.Join(xff, ","), ",")
			for i := len(hops) - 1; i >= 0; i-- {
				if h := strings.TrimSpace(hops[i]); h != "" && (!trustedHop(h) || i == 0) {
					return h
				}
			}
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return strings.TrimSpace(xr)
		}
	}
	return host
}

// trustedHop reports whether an address is loopback or private: a local proxy, or a LAN hop.
func trustedHop(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// peerIP is the TCP peer address, ignoring forwarding headers.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: int(store.SessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r), MaxAge: -1})
}

// ------------------------------------------------------------------ middleware

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (sr *statusRecorder) Unwrap() http.ResponseWriter { return sr.ResponseWriter }

// Hijack lets WebSocket upgrades pass through the logging middleware.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := sr.ResponseWriter.(http.Hijacker); ok {
		c, rw, err := hj.Hijack()
		if err == nil {
			sr.status = http.StatusSwitchingProtocols
		}
		return c, rw, err
	}
	return nil, nil, errors.New("hijack not supported")
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		t := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		// HLS segments and progress pings are noisy; log them at debug.
		lvl := slog.LevelInfo
		if strings.HasPrefix(r.URL.Path, "/api/hls/") || strings.HasPrefix(r.URL.Path, "/api/images/") ||
			strings.HasSuffix(r.URL.Path, "/progress") || r.URL.Path == "/api/status" || strings.HasPrefix(r.URL.Path, "/api/stream/") {
			lvl = slog.LevelDebug
		}
		if sr.status >= 500 {
			lvl = slog.LevelWarn
		}
		s.log.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", sr.status, "ms", time.Since(t).Milliseconds())
	})
}

// ------------------------------------------------------------------ rate limit

type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{hits: map[string][]time.Time{}} }

// count returns the hits of key inside window without recording a new one.
func (rl *rateLimiter) count(key string, window time.Duration) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cut := time.Now().Add(-window)
	n := 0
	for _, t := range rl.hits[key] {
		if !t.Before(cut) {
			n++
		}
	}
	return n
}

// allow records a hit and reports whether key is still under limit within window.
func (rl *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	t := time.Now()
	cut := t.Add(-window)
	h := rl.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= limit {
		rl.hits[key] = h
		return false
	}
	rl.hits[key] = append(h, t)
	if len(rl.hits) > 10000 { // crude memory bound
		for k, v := range rl.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(rl.hits, k)
			}
		}
	}
	return true
}

// ------------------------------------------------------------------ lifecycle

// Background runs periodic housekeeping until ctx is done.
func (s *Server) Background(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.st.PurgeSessions()
			s.qc.purge()
		}
	}
}

// Shutdown stops background subsystems (ffmpeg processes, scanners, parties).
func (s *Server) Shutdown(ctx context.Context) {
	if s.Media != nil {
		s.Media.StopAll()
	}
}
