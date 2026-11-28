package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ScotyLucas/kinora/internal/store"
)

// IdleTimeout stops a session nobody fetched from for this long.
const IdleTimeout = 90 * time.Second

// FinishedIdleTimeout is the same for a session whose ffmpeg already finished. A remux
// finishes long before the film does: the playlist is closed, the player fills its buffer
// and goes quiet for minutes (or the viewer pauses), yet it still needs the segments.
const FinishedIdleTimeout = 30 * time.Minute

// Session is one running HLS ffmpeg process.
type Session struct {
	ID       string
	UserID   int64
	ItemID   int64
	Title    string
	Mode     string
	Encoder  string
	Decode   string // GPU decode pipeline, "" = CPU decoding
	Start    float64
	Dir      string
	Started  time.Time
	cmd      *exec.Cmd
	done     chan struct{}
	stderr   *tailBuffer
	err      error
	lastSeen atomic.Int64
	stopping atomic.Bool // set by Stop: an exit after this is expected
}

// Finished reports whether ffmpeg exited.
func (s *Session) Finished() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) touch() { s.lastSeen.Store(time.Now().UnixNano()) }

// SessionInfo is a dashboard view of a session.
type SessionInfo struct {
	ID      string `json:"id"`
	ItemID  int64  `json:"item_id"`
	Title   string `json:"title"`
	Mode    string `json:"mode"`
	Encoder string `json:"encoder"`
	Decode  string `json:"decode,omitempty"`
	UserID  int64  `json:"user_id"`
	Running bool   `json:"running"`
	Seconds int    `json:"seconds"`
}

// MaxSessionsPerUser bounds one account's ffmpeg processes. An account is shared by up to 8 household
// profiles, so this allows a stream on every profile at once; past it the least recently used session
// of the account stops (a client that forgot to stop its old session must not pile up processes).
const MaxSessionsPerUser = 8

// ErrBusy is returned when the server-wide session limit is reached.
var ErrBusy = errors.New("a szerver most nem tud több átkódolást indítani, próbáld később")

// Manager owns transcode sessions.
type Manager struct {
	dir  string
	log  *slog.Logger
	Enc  *Encoders
	mu   sync.Mutex
	sess map[string]*Session
	// MaxSessions limits sessions across all users (0 = no limit; KINORA_MAX_TRANSCODES).
	MaxSessions int
	// EncoderSetting returns the "encoder" setting; with auto ("" too) a session
	// may move to another GPU family that decodes the source on the card.
	EncoderSetting func() string
}

// NewManager creates the manager and wipes leftovers of previous runs.
func NewManager(cacheDir string, enc *Encoders, log *slog.Logger) *Manager {
	dir := filepath.Join(cacheDir, "transcode")
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	return &Manager{dir: dir, log: log, Enc: enc, sess: map[string]*Session{}}
}

// Sessions lists active sessions.
func (m *Manager) Sessions() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SessionInfo{}
	for _, s := range m.sess {
		out = append(out, SessionInfo{ID: s.ID, ItemID: s.ItemID, Title: s.Title, Mode: s.Mode, Encoder: s.Encoder, Decode: s.Decode, UserID: s.UserID,
			Running: !s.Finished(), Seconds: int(time.Since(s.Started).Seconds())})
	}
	return out
}

// HLSSpec describes what to produce.
type HLSSpec struct {
	Item     *store.Item
	UserID   int64
	Decision Decision
	Start    float64
	Encoder  Encoder
	// GPU, when set, decodes and scales on the graphics card (from Manager.Start's plan).
	GPU *GPUDecode
}

// BuildHLSArgs returns the ffmpeg command line for a session writing into dir.
func BuildHLSArgs(sp HLSSpec, dir string) []string {
	d := sp.Decision
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	gpu := sp.GPU
	if d.VideoCopy || d.BurnSubIdx != nil || d.ToneMap {
		gpu = nil // these need the frames on the CPU
	}
	if gpu != nil {
		args = append(args, gpu.Pre...)
	} else if !d.VideoCopy {
		args = append(args, sp.Encoder.InitArgs()...)
	}
	if sp.Start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", sp.Start))
	}
	args = append(args, "-i", sp.Item.Path)

	opts := EncodeOpts{Bitrate: d.Bitrate, Width: d.Width, ToneMap: d.ToneMap, GOP: true}
	// HEVC/AV1 output can't go in MPEG-TS for Apple players / hls.js either.
	fmp4 := d.FMP4 || (!d.VideoCopy && sp.Encoder.Codec != "" && sp.Encoder.Codec != "h264")
	if d.VideoCopy {
		args = append(args, "-map", "0:v:0", "-c:v", "copy")
		if codecName(sp.Item.VideoCodec) == "hevc" {
			args = append(args, "-tag:v", "hvc1") // Apple players only accept the hvc1 sample entry
		}
	} else if d.BurnSubIdx != nil {
		fc := fmt.Sprintf("[0:v:0][0:s:%d]overlay=eof_action=pass,%s[v]", *d.BurnSubIdx, sp.Encoder.Filter(opts))
		args = append(args, "-filter_complex", fc, "-map", "[v]")
		args = append(args, sp.Encoder.Args(opts)...)
	} else {
		vf := sp.Encoder.Filter(opts)
		if gpu != nil {
			vf = gpu.Filter(opts)
		}
		args = append(args, "-map", "0:v:0", "-vf", vf)
		args = append(args, sp.Encoder.Args(opts)...)
	}
	if d.AudioIdx >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", d.AudioIdx))
		if d.AudioCopy {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
		}
	}
	args = append(args, "-sn", "-dn", "-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "2048",
		"-f", "hls", "-hls_time", "4", "-hls_list_size", "0", "-hls_playlist_type", "event",
		"-hls_flags", hlsFlags)
	// Output paths use forward slashes: ffmpeg's HLS muxer finds the playlist folder by the last '/',
	// so with Windows backslashes it wrote init.mp4 into the working directory instead of the session.
	if fmp4 {
		args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.ToSlash(filepath.Join(dir, "seg%05d.m4s")))
	} else {
		args = append(args, "-hls_segment_filename", filepath.ToSlash(filepath.Join(dir, "seg%05d.ts")))
	}
	args = append(args, filepath.ToSlash(filepath.Join(dir, "index.m3u8")))
	return args
}

// Start launches ffmpeg and waits (max 30 s) until the playlist has a segment.
// When a GPU pipeline decodes the source, it is used; if that session fails
// before its first segment, it is started again with CPU decoding and the
// pipeline is not used for that source codec any more.
func (m *Manager) Start(ctx context.Context, sp HLSSpec) (*Session, error) {
	d := sp.Decision
	src := codecName(sp.Item.VideoCodec)
	orig := sp.Encoder
	if !d.VideoCopy && sp.GPU == nil && m.Enc != nil {
		auto := true
		if m.EncoderSetting != nil {
			v := m.EncoderSetting()
			auto = v == "" || v == "auto"
		}
		sp.Encoder, sp.GPU = m.Enc.PlanGPU(sp.Encoder, src, d.ToneMap || d.BurnSubIdx != nil, auto)
	}
	s, err := m.start(ctx, sp)
	if err != nil && sp.GPU != nil && ctx.Err() == nil && !errors.Is(err, ErrBusy) {
		m.log.Warn("gpu decoding failed, retrying with cpu decoding", "item", sp.Item.Title, "pipeline", sp.GPU.Name,
			"codec", src, "encoder", sp.Encoder.Name, "err", err)
		m.Enc.MarkGPUBroken(sp.GPU.Name, src)
		sp.GPU, sp.Encoder = nil, orig
		return m.start(ctx, sp)
	}
	return s, err
}

func (m *Manager) start(ctx context.Context, sp HLSSpec) (*Session, error) {
	if err := m.makeRoom(sp.UserID); err != nil {
		return nil, err
	}
	id := store.RandomToken(12)
	dir := filepath.Join(m.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	args := BuildHLSArgs(sp, dir)
	cmd := exec.Command(FFmpeg, args...)
	tb := &tailBuffer{max: 8192}
	cmd.Stderr = tb
	s := &Session{ID: id, UserID: sp.UserID, ItemID: sp.Item.ID, Title: sp.Item.Title, Mode: sp.Decision.Mode,
		Encoder: sp.Encoder.Name, Start: sp.Start, Dir: dir, Started: time.Now(), cmd: cmd, done: make(chan struct{}), stderr: tb}
	if sp.Decision.VideoCopy {
		s.Encoder = "copy"
	} else if sp.GPU != nil && !sp.Decision.ToneMap && sp.Decision.BurnSubIdx == nil {
		s.Decode = sp.GPU.Name
	}
	s.touch()
	m.log.Debug("ffmpeg", "args", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("az ffmpeg nem indult el: %w", err)
	}
	go func() {
		s.err = cmd.Wait()
		pl, _ := readShared(filepath.Join(dir, "index.m3u8"))
		m.log.Info("ffmpeg done", "session", id, "took", time.Since(s.Started).Round(time.Millisecond), "err", s.err,
			"segments", bytes.Count(pl, []byte("#EXTINF")), "endlist", bytes.Contains(pl, []byte("#EXT-X-ENDLIST")),
			"stderr", lastLine(tb.String()))
		if s.err == nil {
			if n := settleTemp(dir); n > 0 {
				m.log.Warn("ffmpeg left files under their temporary name; put in place", "session", id, "files", n, "stderr", lastLine(tb.String()))
			}
		}
		close(s.done)
		if s.err != nil && !s.stopping.Load() {
			m.log.Warn("ffmpeg exited", "session", id, "err", s.err, "stderr", lastLine(tb.String()))
		} else if s.err == nil && strings.TrimSpace(tb.String()) != "" {
			m.log.Info("ffmpeg finished with messages", "session", id, "stderr", lastLine(tb.String()))
		}
	}()
	m.mu.Lock()
	m.sess[id] = s
	m.mu.Unlock()
	m.log.Info("transcode started", "session", id, "item", sp.Item.Title, "mode", s.Mode, "encoder", s.Encoder, "decode", firstNonEmpty(s.Decode, "cpu"), "start", sp.Start)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if hasSegment(filepath.Join(dir, "index.m3u8")) {
			return s, nil
		}
		if s.Finished() {
			if hasSegment(filepath.Join(dir, "index.m3u8")) {
				return s, nil
			}
			m.Stop(id)
			msg := lastLine(tb.String())
			if msg == "" && s.err != nil {
				msg = s.err.Error()
			}
			return nil, fmt.Errorf("az átkódolás nem sikerült: %s", msg)
		}
		select {
		case <-ctx.Done():
			m.Stop(id)
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	msg := lastLine(tb.String())
	m.Stop(id)
	return nil, fmt.Errorf("az átkódolás 30 mp alatt sem adott első szegmenst (lassú encoder?) %s", msg)
}

// makeRoom enforces the per-user and server-wide session limits before a new session starts.
func (m *Manager) makeRoom(userID int64) error {
	m.mu.Lock()
	var oldest *Session
	mine := 0
	for _, s := range m.sess {
		if s.UserID != userID {
			continue
		}
		mine++
		if oldest == nil || s.lastSeen.Load() < oldest.lastSeen.Load() {
			oldest = s
		}
	}
	total := len(m.sess)
	m.mu.Unlock()
	if mine >= MaxSessionsPerUser && oldest != nil {
		m.log.Info("transcode limit per user, stopping the oldest", "user", userID, "session", oldest.ID)
		m.Stop(oldest.ID)
		total--
	}
	if m.MaxSessions > 0 && total >= m.MaxSessions {
		return ErrBusy
	}
	return nil
}

func hasSegment(playlist string) bool {
	b, err := readShared(playlist)
	return err == nil && (bytes.Contains(b, []byte(".ts")) || bytes.Contains(b, []byte(".m4s")))
}

// Get returns a session owned by userID.
func (m *Manager) Get(id string, userID int64) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess[id]
	if s == nil || s.UserID != userID {
		return nil
	}
	s.touch()
	return s
}

// TouchItem keeps userID's sessions of itemID alive: a position report says the
// player is still on it even while it plays from its buffer and fetches nothing.
func (m *Manager) TouchItem(userID, itemID int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sess {
		if s.UserID == userID && s.ItemID == itemID {
			s.touch()
		}
	}
}

// Stop kills a session and removes its files.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	s := m.sess[id]
	delete(m.sess, id)
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.stopping.Store(true)
	if !s.Finished() {
		gracefulStop(s.cmd)
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			s.cmd.Process.Kill()
			<-s.done
		}
	}
	os.RemoveAll(s.Dir)
	m.log.Debug("transcode stopped", "session", id)
}

// StopAll stops every session (shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sess))
	for id := range m.sess {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); m.Stop(id) }(id)
	}
	wg.Wait()
}

// Reap stops idle sessions periodically until ctx ends.
func (m *Manager) Reap(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			cut := now.Add(-IdleTimeout).UnixNano()
			cutFinished := now.Add(-FinishedIdleTimeout).UnixNano()
			m.mu.Lock()
			var idle []string
			for id, s := range m.sess {
				c := cut
				if s.Finished() {
					c = cutFinished
				}
				if s.lastSeen.Load() < c {
					idle = append(idle, id)
				}
			}
			m.mu.Unlock()
			for _, id := range idle {
				m.log.Info("transcode idle, stopping", "session", id)
				m.Stop(id)
			}
		}
	}
}

// hlsFlags: the playlist is written to a .tmp and renamed over the old one, so a reader never sees half
// of it. On Windows a rename onto a file someone has open fails, and a failed playlist rename leaves an
// early playlist behind (the player stops a minute in), so there ffmpeg rewrites it in place; Playlist
// cuts a half-written last line.
var hlsFlags = func() string {
	if runtime.GOOS == "windows" {
		return "independent_segments"
	}
	return "independent_segments+temp_file"
}()

// settleTemp runs once ffmpeg has exited cleanly. ffmpeg writes the playlist and every segment to a .tmp
// and renames it into place; on Windows that rename fails now and then (an antivirus scan or a reader
// holds the target), and ffmpeg only logs it. A failed last playlist rename leaves an early playlist,
// which the ENDLIST added by Playlist turns into a short video: the player stops a minute in. The .tmp
// files are complete at this point, so they are put in place here. It returns how many it moved.
func settleTemp(dir string) int {
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	n := 0
	for _, t := range tmps {
		for i := 0; i < 20; i++ {
			if err := os.Rename(t, strings.TrimSuffix(t, ".tmp")); err == nil {
				n++
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return n
}

// file is the path to serve for name: once ffmpeg is done, a file that is still under its temporary name
// (settleTemp could not move it) is served from there.
func (s *Session) file(name string) string {
	p := filepath.Join(s.Dir, name)
	if s.Finished() {
		if _, err := os.Stat(p + ".tmp"); err == nil {
			return p + ".tmp"
		}
	}
	return p
}

// SegmentPath is the file to serve for a segment or init.mp4 of this session.
func (s *Session) SegmentPath(name string) string { return s.file(name) }

// Playlist returns the playlist, appending #EXT-X-ENDLIST once ffmpeg is done.
func (s *Session) Playlist() ([]byte, error) {
	b, err := readShared(s.file("index.m3u8"))
	if err != nil {
		return nil, err
	}
	if !s.Finished() {
		// rewritten in place (Windows): drop a line ffmpeg is still writing
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			b = b[:i+1]
		}
	} else if !bytes.Contains(b, []byte("#EXT-X-ENDLIST")) {
		if !bytes.HasSuffix(b, []byte("\n")) {
			b = append(b, '\n')
		}
		b = append(b, []byte("#EXT-X-ENDLIST\n")...)
	}
	return b, nil
}

// TranscodedSeconds sums the EXTINF durations written so far.
func (s *Session) TranscodedSeconds() float64 {
	f, err := os.Open(filepath.Join(s.Dir, "index.m3u8"))
	if err != nil {
		return 0
	}
	defer f.Close()
	total := 0.0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var d float64
		if _, err := fmt.Sscanf(sc.Text(), "#EXTINF:%f,", &d); err == nil {
			total += d
		}
	}
	return total
}

// tailBuffer keeps the last max bytes written (ffmpeg stderr).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

var _ io.Writer = (*tailBuffer)(nil)
