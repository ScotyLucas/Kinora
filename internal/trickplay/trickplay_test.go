package trickplay

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ScotyLucas/kinora/internal/store"
)

type fakeSrc map[int64]store.TrickplaySource

func (f fakeSrc) TrickplaySources() ([]store.TrickplaySource, error) {
	var out []store.TrickplaySource
	for _, s := range f {
		out = append(out, s)
	}
	return out, nil
}

func (f fakeSrc) TrickplaySource(id int64) (store.TrickplaySource, error) {
	s, ok := f[id]
	if !ok {
		return s, store.ErrNotFound
	}
	return s, nil
}

func writeJPEG(t *testing.T, p string, w, h int, c color.Color) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

func size(t *testing.T, p string) (int, int) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

// 150 frames: one full sheet and a second one with five rows; frame 105 sits in row 0, column 5 of sheet 1.
func TestTileSheets(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	for i := 0; i < 150; i++ {
		c := color.RGBA{A: 255}
		if i == 105 {
			c = color.RGBA{R: 255, A: 255}
		}
		writeJPEG(t, filepath.Join(in, strconv.Itoa(10000+i)+".jpg"), 32, 18, c)
	}
	m, err := tile(in, out)
	if err != nil {
		t.Fatal(err)
	}
	if m.Count != 150 || m.Sheets != 2 || m.Width != 32 || m.Height != 18 || m.Cols != Cols || m.Rows != Rows {
		t.Fatalf("manifest %+v", m)
	}
	if w, h := size(t, filepath.Join(out, "0.jpg")); w != 320 || h != 180 {
		t.Fatalf("sheet 0 is %dx%d", w, h)
	}
	if w, h := size(t, filepath.Join(out, "1.jpg")); w != 320 || h != 90 {
		t.Fatalf("sheet 1 is %dx%d, want 320x90 (5 rows)", w, h)
	}
	f, _ := os.Open(filepath.Join(out, "1.jpg"))
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := img.At(5*32+16, 9).RGBA(); r < 0xc000 {
		t.Fatalf("frame 105 is not at sheet 1, column 5, row 0")
	}
	if r, _, _, _ := img.At(4*32+16, 9).RGBA(); r > 0x4000 {
		t.Fatalf("frame 104 is red")
	}
}

func TestGenerateFromVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	// 65 s, a keyframe every 2 s: frames at 0, 10, …, 60 → 7
	if b, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=65",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "20", video).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	src := fakeSrc{7: {ID: 7, Path: video, RuntimeSec: 65, Sig: "a"}}
	g := New(src, "ffmpeg", dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if g.Manifest(7) != nil {
		t.Fatal("manifest before generation")
	}
	if st := g.Status(); st.Queue != 1 {
		t.Fatalf("requested item not queued: %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, ok := g.next(ctx)
	if !ok || s.ID != 7 {
		t.Fatalf("next: %+v %v", s, ok)
	}
	m, err := g.Generate(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if m.Count < 6 || m.Count > 8 || m.Sheets != 1 || m.Width != Width || m.Height != 180 {
		t.Fatalf("manifest %+v", m)
	}
	if got := g.Manifest(7); got == nil || got.Count != m.Count {
		t.Fatalf("manifest after generation: %+v", got)
	}
	if g.SheetPath(7, 0) == "" || g.SheetPath(7, 1) != "" || g.SheetPath(7, -1) != "" {
		t.Fatal("sheet paths")
	}
	// turned off: no manifest and nothing queued, but the sheets are kept for later
	off := false
	g.Enabled = func() bool { return !off }
	off = true
	if g.Manifest(7) != nil || g.Status().Queue != 0 {
		t.Fatal("manifest or queueing while turned off")
	}
	off = false
	if g.Manifest(7) == nil {
		t.Fatal("sheets lost while turned off")
	}
	// the file changed: the old sheets do not count, and the item is queued again
	src[7] = store.TrickplaySource{ID: 7, Path: video, RuntimeSec: 65, Sig: "b"}
	if g.Manifest(7) != nil {
		t.Fatal("stale manifest returned")
	}
	// the item is gone from the library: prune removes its sheets
	delete(src, 7)
	g.refill(true)
	if _, err := os.Stat(g.itemDir(7)); !os.IsNotExist(err) {
		t.Fatalf("sheets of a removed item kept: %v", err)
	}
}

func TestFailedFileNotRetried(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.mkv")
	os.WriteFile(bad, []byte("not a video"), 0o644)
	src := fakeSrc{3: {ID: 3, Path: bad, Sig: "x"}}
	g := New(src, "ffmpeg", dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.Request(3)
	g.drain(context.Background())
	if st := g.Status(); st.LastError == "" || st.Done != 0 {
		t.Fatalf("status %+v", st)
	}
	g.Request(3)
	if _, ok := g.next(context.Background()); ok {
		t.Fatal("a failed file is retried before it changes")
	}
}
