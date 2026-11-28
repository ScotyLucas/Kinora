package media

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestGPUSession runs the real GPU self-test and a transcode through a GPU
// pipeline on whatever hardware this machine has (VideoToolbox on a Mac).
// Skipped where no pipeline passes (CI runners, the dev container).
func TestGPUSession(t *testing.T) {
	if _, err := exec.LookPath(FFmpeg); err != nil {
		t.Skip("no ffmpeg")
	}
	it := probeItem(t, testFile(t, "Movies/Space Drama (2015)/Space.Drama.2015.1080p.BluRay.x265.mkv"))
	enc := NewEncoders()
	enc.SelfTest(context.Background())
	var ok []string
	for _, g := range enc.GPUResults() {
		if g.OK {
			ok = append(ok, g.Pipeline+"/"+g.Codec)
		} else if g.Error == "" {
			t.Errorf("%s/%s failed without a reason", g.Pipeline, g.Codec)
		}
	}
	if len(ok) == 0 {
		t.Skipf("no GPU pipeline on this machine: %+v", enc.GPUResults())
	}
	t.Logf("GPU pipelines: %v", ok)
	m := NewManager(t.TempDir(), enc, quiet)
	defer m.StopAll()
	d := Decide(it, PlaybackRequest{Caps: chrome}, nil, 0, enc.ToneMap())
	if d.Mode != "transcode" {
		t.Fatalf("mode %s", d.Mode)
	}
	base := enc.Pick("auto")
	if _, g := enc.PlanGPU(base, codecName(it.VideoCodec), false, true); g == nil {
		t.Skipf("no GPU pipeline decodes %s for %s", it.VideoCodec, base.Name)
	}
	s, err := m.Start(context.Background(), HLSSpec{Item: it, UserID: 1, Decision: d, Encoder: base})
	if err != nil {
		t.Fatal(err)
	}
	if s.Decode == "" {
		t.Fatalf("session must decode on the GPU (encoder %s)", s.Encoder)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !s.Finished() && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	pl, _ := s.Playlist()
	if !strings.Contains(string(pl), "#EXT-X-ENDLIST") || s.err != nil {
		t.Fatalf("gpu session: err=%v stderr=%s playlist=%s", s.err, s.stderr.String(), pl)
	}
	t.Logf("session encoder=%s decode=%s", s.Encoder, s.Decode)
}
