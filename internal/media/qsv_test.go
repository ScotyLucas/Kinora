package media

import (
	"strings"
	"testing"
)

func TestQSVInitCandidates(t *testing.T) {
	if c := qsvInitCandidates("linux"); len(c) != 1 || strings.Join(c[0], " ") != "-init_hw_device qsv=hw -filter_hw_device hw" {
		t.Fatalf("linux: %v", c)
	}
	win := qsvInitCandidates("windows")
	if len(win) < 2 {
		t.Fatalf("windows: want several candidates, got %v", win)
	}
	first := strings.Join(win[0], " ")
	if !strings.Contains(first, "d3d11va=dx11:,vendor_id=0x8086") || !strings.Contains(first, "qsv=qs@dx11") {
		t.Fatalf("windows: the Intel D3D11 device must come first, got %q", first)
	}
	for _, c := range win {
		if c[len(c)-2] != "-filter_hw_device" {
			t.Fatalf("every candidate must set the filter device: %v", c)
		}
	}
}

func TestQSVInitArgsFollowsProbe(t *testing.T) {
	defer qsvInit.Store(nil)
	chosen := []string{"-init_hw_device", "qsv=x", "-filter_hw_device", "x"}
	qsvInit.Store(&chosen)
	enc, _ := encoderByName("hevc_qsv")
	got := enc.InitArgs()
	if strings.Join(got, " ") != strings.Join(chosen, " ") {
		t.Fatalf("InitArgs = %v, want %v", got, chosen)
	}
	got[1] = "mutated"
	if chosen[1] != "qsv=x" {
		t.Fatal("InitArgs must return a copy")
	}
}

func TestProbeQSVFallsBackToFirst(t *testing.T) {
	defer qsvInit.Store(nil)
	old := FFmpeg
	FFmpeg = "kinora-no-such-ffmpeg"
	defer func() { FFmpeg = old }()
	cands := qsvInitCandidates("windows")
	r := probeQSV(t.Context(), cands)
	if r.OK || r.Name != "h264_qsv" || !strings.Contains(r.Error, "egyik sem működött") {
		t.Fatalf("probe result: %+v", r)
	}
	if QSVInit() != strings.Join(cands[0], " ") {
		t.Fatalf("after a failed probe the preferred setup stays, got %q", QSVInit())
	}
}
