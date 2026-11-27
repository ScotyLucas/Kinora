// Package trickplay makes the small preview pictures shown above the player's timeline
// while it is dragged or hovered: one frame every Interval seconds, scaled to Width
// pixels, laid out Cols×Rows to a JPEG sheet, so a client loads a handful of files.
//
// The frames come from ffmpeg reading only the keyframes (-skip_frame nokey), which
// is fast but puts a frame up to one keyframe distance (a few seconds) before its slot.
package trickplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ScotyLucas/kinora/internal/store"
)

const (
	Interval = 10  // seconds between frames
	Width    = 320 // thumbnail width in pixels
	Cols     = 10
	Rows     = 10
)

// Manifest describes an item's sheets. Frame i (at i*Interval seconds) is on sheet
// i/(Cols*Rows), in column i%Cols and row (i%(Cols*Rows))/Cols.
type Manifest struct {
	Interval int    `json:"interval"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Count    int    `json:"count"`
	Sheets   int    `json:"sheets"`
	Sig      string `json:"sig"` // the source file's size-mtime
}

// Source is what the generator needs to know about the store.
type Source interface {
	TrickplaySources() ([]store.TrickplaySource, error)
	TrickplaySource(id int64) (store.TrickplaySource, error)
}

// Status is shown on the admin page.
type Status struct {
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Queue     int    `json:"queue"`
	Running   int64  `json:"running"`
	Done      int    `json:"done"`
	Ready     int    `json:"ready"`
	LastError string `json:"last_error"`
}

// Generator makes sheets in the background, newest items first, and an item that is
// opened in the player jumps the queue.
type Generator struct {
	src    Source
	dir    string
	log    *slog.Logger
	FFmpeg string
	// ToneMap: ffmpeg has zscale+tonemap, so HDR frames are mapped to SDR instead of looking grey.
	ToneMap bool
	// Nice runs ffmpeg at the lowest CPU priority (not on Windows).
	Nice bool
	// Busy is true while something plays or transcodes: the backlog waits (a requested item does not).
	Busy func() bool
	// Enabled is read before every item (the admin setting).
	Enabled func() bool

	mu      sync.Mutex
	prio    []int64
	backlog []store.TrickplaySource
	have    map[int64]string // id → sig of finished sheets
	failed  map[int64]string // id → sig that failed: not retried until the file changes
	kick    chan struct{}
	running int64
	done    int
	lastErr string
}

// New makes a generator that writes to <cacheDir>/trickplay.
func New(src Source, ffmpeg, cacheDir string, log *slog.Logger) *Generator {
	return &Generator{src: src, dir: filepath.Join(cacheDir, "trickplay"), log: log, FFmpeg: ffmpeg,
		have: map[int64]string{}, failed: map[int64]string{}, kick: make(chan struct{}, 1)}
}

func (g *Generator) itemDir(id int64) string { return filepath.Join(g.dir, strconv.FormatInt(id, 10)) }

// Available reports whether ffmpeg can be run.
func (g *Generator) Available() bool {
	if g == nil || g.FFmpeg == "" {
		return false
	}
	_, err := exec.LookPath(g.FFmpeg)
	return err == nil
}

func (g *Generator) enabled() bool { return g.Enabled == nil || g.Enabled() }

// Manifest returns the item's sheets when they are made for its current file. When they
// are not, the item is put at the front of the queue and nil is returned. Turned off, it
// returns nil (the sheets made so far stay on disk for when it is turned back on).
func (g *Generator) Manifest(id int64) *Manifest {
	if !g.enabled() {
		return nil
	}
	src, err := g.src.TrickplaySource(id)
	if err != nil {
		return nil
	}
	if m := g.read(id); m != nil && m.Sig == src.Sig {
		return m
	}
	g.Request(id)
	return nil
}

// SheetPath is the file of sheet n, or "" when there is none for the item's current file.
func (g *Generator) SheetPath(id int64, n int) string {
	m := g.read(id)
	if m == nil || n < 0 || n >= m.Sheets {
		return ""
	}
	return filepath.Join(g.itemDir(id), strconv.Itoa(n)+".jpg")
}

func (g *Generator) read(id int64) *Manifest {
	b, err := os.ReadFile(filepath.Join(g.itemDir(id), "manifest.json"))
	if err != nil {
		return nil
	}
	var m Manifest
	if json.Unmarshal(b, &m) != nil || m.Sheets == 0 {
		return nil
	}
	return &m
}

// Request puts an item at the front of the queue (it is being watched).
func (g *Generator) Request(id int64) {
	if !g.enabled() {
		return
	}
	g.mu.Lock()
	if g.running == id {
		g.mu.Unlock()
		return
	}
	for _, q := range g.prio {
		if q == id {
			g.mu.Unlock()
			return
		}
	}
	g.prio = append(g.prio, id)
	g.mu.Unlock()
	g.Kick()
}

// Kick wakes the worker (after a scan).
func (g *Generator) Kick() {
	select {
	case g.kick <- struct{}{}:
	default:
	}
}

// Status snapshot.
func (g *Generator) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Status{Available: g.Available(), Enabled: g.enabled(), Queue: len(g.prio) + len(g.backlog), Running: g.running,
		Done: g.done, Ready: len(g.have), LastError: g.lastErr}
}

// Run works through the queue until ctx ends.
func (g *Generator) Run(ctx context.Context) {
	if !g.Available() {
		g.log.Info("trickplay disabled", "reason", "ffmpeg not found")
		return
	}
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	g.refill(true)
	for {
		g.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-g.kick: // after a scan or a request: new files may be there
			g.refill(false)
		case <-t.C:
			g.refill(true)
		}
	}
}

// refill rebuilds the backlog from the library; prune also deletes sheets of items that are gone.
func (g *Generator) refill(prune bool) {
	srcs, err := g.src.TrickplaySources()
	if err != nil {
		return
	}
	g.mu.Lock()
	known := len(g.have) > 0
	g.mu.Unlock()
	if !known {
		g.loadHave()
	}
	g.mu.Lock()
	g.backlog = g.backlog[:0]
	for _, s := range srcs {
		if g.have[s.ID] != s.Sig && g.failed[s.ID] != s.Sig {
			g.backlog = append(g.backlog, s)
		}
	}
	g.mu.Unlock()
	if prune {
		g.prune(srcs)
	}
}

// loadHave reads the manifests on disk once, so a restart does not redo finished items.
func (g *Generator) loadHave() {
	ents, _ := os.ReadDir(g.dir)
	for _, e := range ents {
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() {
			continue
		}
		if m := g.read(id); m != nil {
			g.mu.Lock()
			g.have[id] = m.Sig
			g.mu.Unlock()
		}
	}
}

func (g *Generator) prune(srcs []store.TrickplaySource) {
	keep := make(map[int64]bool, len(srcs))
	for _, s := range srcs {
		keep[s.ID] = true
	}
	ents, _ := os.ReadDir(g.dir)
	for _, e := range ents {
		name := e.Name()
		if strings.Contains(name, ".tmp") { // a run that was cut off
			_ = os.RemoveAll(filepath.Join(g.dir, name))
			continue
		}
		id, err := strconv.ParseInt(name, 10, 64)
		if err != nil || keep[id] {
			continue
		}
		if _, err := g.src.TrickplaySource(id); err == nil {
			continue // another version of a film, made on demand
		}
		_ = os.RemoveAll(filepath.Join(g.dir, name))
		g.mu.Lock()
		delete(g.have, id)
		g.mu.Unlock()
	}
}

// next takes the next item: a requested one, else (when nothing plays) the backlog's first.
func (g *Generator) next(ctx context.Context) (store.TrickplaySource, bool) {
	for ctx.Err() == nil {
		g.mu.Lock()
		if len(g.prio) > 0 {
			id := g.prio[0]
			g.prio = g.prio[1:]
			g.mu.Unlock()
			s, err := g.src.TrickplaySource(id)
			if err != nil {
				continue
			}
			if m := g.read(id); m != nil && m.Sig == s.Sig {
				continue
			}
			g.mu.Lock()
			bad := g.failed[id] == s.Sig
			g.mu.Unlock()
			if bad {
				continue
			}
			return s, true
		}
		if len(g.backlog) == 0 {
			g.mu.Unlock()
			return store.TrickplaySource{}, false
		}
		g.mu.Unlock()
		if g.Busy != nil && g.Busy() {
			select { // wait, but a request wakes us
			case <-ctx.Done():
			case <-g.kick:
			case <-time.After(20 * time.Second):
			}
			continue
		}
		g.mu.Lock()
		if len(g.backlog) == 0 {
			g.mu.Unlock()
			continue
		}
		s := g.backlog[0]
		g.backlog = g.backlog[1:]
		dup := g.have[s.ID] == s.Sig // made on request since the backlog was built
		g.mu.Unlock()
		if dup {
			continue
		}
		return s, true
	}
	return store.TrickplaySource{}, false
}

func (g *Generator) drain(ctx context.Context) {
	for ctx.Err() == nil && g.enabled() {
		s, ok := g.next(ctx)
		if !ok {
			return
		}
		g.mu.Lock()
		g.running = s.ID
		g.mu.Unlock()
		start := time.Now()
		m, err := g.Generate(ctx, s)
		g.mu.Lock()
		g.running = 0
		if err != nil {
			if ctx.Err() == nil {
				g.failed[s.ID] = s.Sig
				g.lastErr = fmt.Sprintf("#%d: %v", s.ID, err)
				g.log.Warn("trickplay", "item", s.ID, "err", err)
			}
		} else {
			g.have[s.ID] = s.Sig
			g.done++
			g.log.Debug("trickplay made", "item", s.ID, "frames", m.Count, "took", time.Since(start).Round(time.Millisecond))
		}
		g.mu.Unlock()
	}
}

// Generate makes the sheets of one file and replaces the item's old ones.
func (g *Generator) Generate(ctx context.Context, s store.TrickplaySource) (*Manifest, error) {
	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(g.dir, strconv.FormatInt(s.ID, 10)+".tmp")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	frames := filepath.Join(tmp, "f")
	if err := os.Mkdir(frames, 0o755); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "2", "-skip_frame", "nokey", "-i", s.Path,
		"-map", "0:v:0", "-an", "-sn", "-dn", "-vf", g.filter(s), "-q:v", "4", "-start_number", "0",
		filepath.Join(frames, "%05d.jpg")}
	name := g.FFmpeg
	if g.Nice {
		args, name = append([]string{"-n", "19", g.FFmpeg}, args...), "nice"
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return nil, fmt.Errorf("ffmpeg: %v %s", err, msg)
	}
	m, err := tile(frames, tmp)
	if err != nil {
		return nil, err
	}
	m.Sig = s.Sig
	if err := os.RemoveAll(frames); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), b, 0o644); err != nil {
		return nil, err
	}
	final := g.itemDir(s.ID)
	_ = os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	return m, nil
}

// filter: one frame per Interval, scaled down first (cheap), tone-mapped when HDR.
func (g *Generator) filter(s store.TrickplaySource) string {
	f := []string{fmt.Sprintf("fps=1/%d", Interval), fmt.Sprintf("scale=%d:-2", Width)}
	if s.HDR != "" && g.ToneMap {
		f = append(f, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
			"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv")
	}
	return strings.Join(append(f, "format=yuvj420p"), ",")
}

// tile lays the frames of dir out to sheets in out (0.jpg, 1.jpg, …).
func tile(dir, out string) (*Manifest, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".jpg") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, errors.New("nem készült képkocka")
	}
	sort.Strings(names)
	m := &Manifest{Interval: Interval, Cols: Cols, Rows: Rows, Count: len(names)}
	per := Cols * Rows
	var sheet *image.RGBA
	flush := func(n int) error {
		if sheet == nil {
			return nil
		}
		used := m.Count - n*per // frames on this sheet
		h := m.Height * ((min(used, per) + Cols - 1) / Cols)
		f, err := os.Create(filepath.Join(out, strconv.Itoa(n)+".jpg"))
		if err != nil {
			return err
		}
		err = jpeg.Encode(f, sheet.SubImage(image.Rect(0, 0, m.Width*Cols, h)), &jpeg.Options{Quality: 72})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		sheet = nil
		return err
	}
	for i, name := range names {
		img, err := decode(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if i == 0 {
			b := img.Bounds()
			m.Width, m.Height = b.Dx(), b.Dy()
		}
		if i%per == 0 {
			if err := flush(i/per - 1); err != nil {
				return nil, err
			}
			sheet = image.NewRGBA(image.Rect(0, 0, m.Width*Cols, m.Height*Rows))
		}
		k := i % per
		at := image.Pt(k%Cols*m.Width, k/Cols*m.Height)
		draw.Draw(sheet, image.Rectangle{Min: at, Max: at.Add(image.Pt(m.Width, m.Height))}, img, img.Bounds().Min, draw.Src)
	}
	m.Sheets = (m.Count + per - 1) / per
	if err := flush(m.Sheets - 1); err != nil {
		return nil, err
	}
	return m, nil
}

func decode(p string) (image.Image, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}
