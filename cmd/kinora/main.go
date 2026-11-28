// Command kinora is a self-hosted media server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ScotyLucas/kinora/internal/intro"
	"github.com/ScotyLucas/kinora/internal/jfcompat"
	"github.com/ScotyLucas/kinora/internal/media"
	"github.com/ScotyLucas/kinora/internal/meta"
	"github.com/ScotyLucas/kinora/internal/party"
	"github.com/ScotyLucas/kinora/internal/safe"
	"github.com/ScotyLucas/kinora/internal/scanner"
	"github.com/ScotyLucas/kinora/internal/selfupdate"
	"github.com/ScotyLucas/kinora/internal/server"
	"github.com/ScotyLucas/kinora/internal/store"
	"github.com/ScotyLucas/kinora/internal/trickplay"
)

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("kinora", server.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "service" {
		os.Exit(serviceCommand(os.Args[2:])) // Windows: install / uninstall / start / stop
	}

	cfg := server.Config{}
	flag.StringVar(&cfg.Addr, "addr", env("KINORA_ADDR", ":8096"), "listen address (KINORA_ADDR)")
	flag.StringVar(&cfg.DataDir, "data", env("KINORA_DATA", "./data"), "data directory: database, images (KINORA_DATA)")
	flag.StringVar(&cfg.CacheDir, "cache", env("KINORA_CACHE", ""), "cache directory: transcodes, downloads, subtitles (KINORA_CACHE, default <data>/cache)")
	flag.StringVar(&cfg.WebDir, "web", env("KINORA_WEB_DIR", ""), "serve the web client from this directory instead of the embedded copy (KINORA_WEB_DIR)")
	flag.StringVar(&cfg.MediaRoot, "media-root", env("KINORA_MEDIA_ROOT", ""), "start folder of the admin folder picker (KINORA_MEDIA_ROOT)")
	flag.StringVar(&cfg.CORSOrigins, "cors-origins", env("KINORA_CORS_ORIGINS", ""), "extra app origins allowed for token CORS, comma separated (KINORA_CORS_ORIGINS)")
	discPort, _ := strconv.Atoi(env("KINORA_DISCOVERY_PORT", strconv.Itoa(server.DefaultDiscoveryPort)))
	flag.IntVar(&cfg.DiscoveryPort, "discovery-port", discPort, "UDP port for LAN server discovery, 0 = off (KINORA_DISCOVERY_PORT)")
	// installers pass the first-start branding as flags (Windows service arguments)
	initialName := flag.String("initial-name", "", "server name applied on the first start only (KINORA_INITIAL_SERVER_NAME)")
	initialLogo := flag.String("initial-logo", "", "logo file applied on the first start only (KINORA_INITIAL_LOGO)")
	initialMedia := flag.String("initial-media", "", "media folders added on the first start, ';' separated, optionally Name=path (KINORA_INITIAL_MEDIA)")
	flag.Parse()
	for k, v := range map[string]string{"KINORA_INITIAL_SERVER_NAME": *initialName, "KINORA_INITIAL_LOGO": *initialLogo, "KINORA_INITIAL_MEDIA": *initialMedia} {
		if v != "" {
			os.Setenv(k, v)
		}
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(cfg.DataDir, "cache")
	}

	if isWindowsService() {
		os.Setenv("KINORA_INSTALL", "windows")
		if err := runAsService(cfg); err != nil {
			os.Exit(1)
		}
		return
	}
	log := newLogger(os.Stdout)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log); err != nil {
		log.Error("kinora stopped", "err", err)
		os.Exit(1)
	}
}

func newLogger(w io.Writer) *slog.Logger {
	lvl := slog.LevelInfo
	if strings.EqualFold(os.Getenv("KINORA_LOG_LEVEL"), "debug") {
		lvl = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if strings.EqualFold(os.Getenv("KINORA_LOG_FORMAT"), "json") {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func run(ctx context.Context, cfg server.Config, log *slog.Logger) error {
	ffmpeg := env("KINORA_FFMPEG", bundledTool("ffmpeg"))
	ffprobe := env("KINORA_FFPROBE", bundledTool("ffprobe"))
	for _, bin := range []string{ffmpeg, ffprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%q nem található a PATH-on – telepítsd az ffmpeg csomagot (vagy add meg: KINORA_FFMPEG / KINORA_FFPROBE): %w", bin, err)
		}
	}
	for _, d := range []string{cfg.DataDir, cfg.CacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mappa létrehozása (%s): %w", d, err)
		}
	}

	store.AllowNewerSchema = os.Getenv("KINORA_ALLOW_NEWER_DB") == "1"
	cfg.AllowRemoteSetup = os.Getenv("KINORA_ALLOW_REMOTE_SETUP") == "1"
	st, err := store.Open(filepath.Join(cfg.DataDir, "kinora.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	for key, envName := range server.SettingEnv {
		if v := os.Getenv(envName); v != "" {
			st.SetOverride(key, v)
		}
	}

	media.FFmpeg, media.FFprobe = ffmpeg, ffprobe
	imgDir := filepath.Join(cfg.DataDir, "images")
	sc := scanner.New(st, imgDir, log)

	if v := os.Getenv("KINORA_VAAPI_DEVICE"); v != "" {
		media.VAAPIDevice = v
	}
	enc := media.NewEncoders()
	go func() {
		res := enc.SelfTest(ctx)
		for _, r := range res {
			if r.OK {
				log.Info("encoder ok", "encoder", r.Name, "ms", r.Ms)
			} else if r.Hardware && !r.Skipped {
				// ffmpeg has it but it failed: worth seeing without debug logging
				log.Info("encoder unavailable", "encoder", r.Name, "reason", r.Error)
			} else {
				log.Debug("encoder unavailable", "encoder", r.Name, "reason", r.Error)
			}
			if r.Name == "h264_qsv" && r.OK {
				log.Info("qsv device", "init", media.QSVInit())
			}
		}
		log.Info("encoder selected", "encoder", enc.Pick(st.Setting("encoder")).Name, "tonemap", enc.ToneMap())
	}()
	mgr := media.NewManager(cfg.CacheDir, enc, log)
	if n, err := strconv.Atoi(os.Getenv("KINORA_MAX_TRANSCODES")); err == nil && n > 0 {
		mgr.MaxSessions = n
	}
	safe.Go(ctx, log, "transcode-reaper", mgr.Reap)
	dl := media.NewDownloader(cfg.CacheDir, enc, log)
	dl.EncoderSetting = func() string { return st.Setting("encoder") }
	safe.Go(ctx, log, "downloads", dl.Run)

	hub := party.NewHub(log)
	hub.CanSee = func(profileID, itemID int64) bool {
		p, err := st.Profile(profileID)
		return err == nil && st.ItemVisible(itemID, p)
	}
	hub.Report = func(r party.Report) error {
		return st.AddPartyReport(store.PartyReport{Room: r.Room, ReporterUser: r.Reporter.UserID, ReporterName: r.Reporter.Name,
			SenderUser: r.Sender.UserID, SenderName: r.Sender.Name, Text: r.Text, Reason: r.Reason, CreatedAt: r.At})
	}
	safe.Go(ctx, log, "party", hub.Run)

	fetcher := meta.New(st, imgDir, log)
	if v := os.Getenv("KINORA_TMDB_API"); v != "" { // a TMDB proxy or mirror (also used by tests)
		fetcher.BaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("KINORA_OMDB_API"); v != "" { // an OMDb proxy or mirror (also used by tests)
		fetcher.OmdbURL = v
	}
	var rejoin atomic.Bool // one joining rescan at a time, never a loop
	sc.OnScanned = func(libID int64) {
		if !fetcher.Enabled() {
			return
		}
		go func() { // never block the scanner on network I/O
			defer safe.Recover(log, "metadata-refresh")
			if err := fetcher.Refresh(ctx, false); err != nil && !errors.Is(err, meta.ErrRunning) {
				log.Warn("metadata refresh", "err", err)
			}
			// a new copy of a show matched an existing one: scan once more to join them
			if st.HasSplitSeries(libID) && rejoin.CompareAndSwap(false, true) {
				defer rejoin.Store(false)
				sc.ScanNow(ctx, libID)
			}
		}()
	}

	// skip intro / credits: Chromaprint fingerprints, one season at a time, paused during playback
	analyzer := intro.NewAnalyzer(st, intro.Detect(ffmpeg), cfg.CacheDir, log)
	analyzer.Busy = func() bool { return len(mgr.Sessions()) > 0 }
	analyzer.Settings = func() (bool, int) { return st.Setting("intro_detection") != "0", st.SettingInt("intro_window_min", 10) }
	log.Info("intro detection", "backend", analyzer.Status().Backend)
	safe.Go(ctx, log, "intro", analyzer.Run)
	prevScanned := sc.OnScanned
	sc.OnScanned = func(libID int64) {
		if prevScanned != nil {
			prevScanned(libID)
		}
		analyzer.Kick()
	}

	// trickplay: preview pictures above the player's timeline, made in the background
	thumbs := trickplay.New(st, ffmpeg, cfg.CacheDir, log)
	thumbs.ToneMap = enc.ToneMap()
	if _, err := exec.LookPath("nice"); err == nil && runtime.GOOS != "windows" {
		thumbs.Nice = true
	}
	thumbs.Busy = func() bool { return len(mgr.Sessions()) > 0 }
	thumbs.Enabled = func() bool { return st.Setting("trickplay") != "0" }
	safe.Go(ctx, log, "trickplay", thumbs.Run)
	prevScanned2 := sc.OnScanned
	sc.OnScanned = func(libID int64) {
		if prevScanned2 != nil {
			prevScanned2(libID)
		}
		thumbs.Kick()
	}

	srv := server.New(cfg, st, log, server.Deps{Ctx: ctx, Scanner: sc, ImgDir: imgDir, Media: mgr, Encoders: enc,
		Subs: media.NewSubtitles(cfg.CacheDir), Downloads: dl, Party: hub, Meta: fetcher, Intro: analyzer, Trickplay: thumbs})
	srv.ApplyStartupBranding()
	safe.Go(ctx, log, "housekeeping", srv.Background)
	safe.Go(ctx, log, "discovery", srv.RunDiscovery)
	safe.Go(ctx, log, "scanner", sc.Run)
	safe.Go(ctx, log, "acquire", srv.Acquire.Run) // built-in acquisition: follows downloads, imports finished ones
	safe.Go(ctx, log, "push", srv.Push.Run)       // iOS push notifications through the relay

	// the Jellyfin-compatible API: its own port, started only with KINORA_JELLYFIN_COMPAT=1 (and the admin
	// setting must be on as well; while it is off the port answers 503)
	var jfSrv *http.Server
	if os.Getenv("KINORA_JELLYFIN_COMPAT") == "1" {
		addr := env("KINORA_JELLYFIN_ADDR", ":8097")
		jf, err := jfcompat.New(jfcompat.Deps{Ctx: ctx, Store: st, Media: mgr, Encoders: enc, Subs: srv.Subs, ImgDir: imgDir, Log: log,
			Version: server.Version, QC: srv.JFCompatQC()})
		if err != nil {
			return fmt.Errorf("a jellyfin-kompatibilis API: %w", err)
		}
		jln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("a jellyfin-kompatibilis API: nem sikerült figyelni a(z) %s címen: %w", addr, err)
		}
		server.JFCompatAddr, server.JFCompatServerID = addr, jf.ServerID
		jfSrv = &http.Server{Handler: jf, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second}
		go func() {
			if err := jfSrv.Serve(jln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("Jellyfin-compatible API stopped", "err", err)
			}
		}()
		log.Info("Jellyfin-compatible API listening", "addr", addr, "enabled", jf.Enabled())
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("nem sikerült figyelni a(z) %s címen: %w", cfg.Addr, err)
	}
	log.Info("kinora started", "version", server.Version, "addr", cfg.Addr, "data", cfg.DataDir, "cache", cfg.CacheDir)

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	if install := selfupdate.InstallDir(); install != "" {
		// a version on probation after a self-update counts as good once it has served for a while
		go func() {
			select {
			case <-time.After(30 * time.Second):
				selfupdate.Confirm(install)
			case <-ctx.Done():
			}
		}()
	}

	var result error
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	case <-selfupdate.Restart():
		log.Info("shutting down for the update", "to", selfupdate.StagedTag())
		result = selfupdate.ErrRestart
	}
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shCtx)
	if jfSrv != nil {
		jfSrv.Shutdown(shCtx)
	}
	srv.Shutdown(shCtx)
	waitOrTimeout(shCtx, sc.Wait)
	log.Info("bye")
	return result
}

// bundledTool prefers an ffmpeg shipped next to the executable (Windows installer:
// <app>fmpegfmpeg.exe), else the name is looked up on the PATH.
func bundledTool(name string) string {
	if exe, err := os.Executable(); err == nil {
		for _, p := range []string{filepath.Join(filepath.Dir(exe), "ffmpeg", name+".exe"), filepath.Join(filepath.Dir(exe), name+".exe")} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return name
}

// healthcheck is used by the Docker HEALTHCHECK (no curl needed in the image).
func healthcheck() int {
	addr := env("KINORA_ADDR", ":8096")
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/api/status")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 1
	}
	return 0
}

// waitOrTimeout runs f (a blocking wait) but gives up when ctx expires.
func waitOrTimeout(ctx context.Context, f func()) {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
