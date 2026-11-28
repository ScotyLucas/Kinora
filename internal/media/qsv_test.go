package media

import (
	"strings"
	"testing"

	"github.com/ScotyLucas/kinora/internal/store"
)

func TestQSVInitCandidates(t *testing.T) {
	if c := qsvInitCandidates("linux"); len(c) != 2 || strings.Join(c[0], " ") != "-init_hw_device qsv=hw -filter_hw_device hw" || len(c[1]) != 0 {
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
	if len(win[len(win)-1]) != 0 {
		t.Fatalf("windows: system memory must be the last resort, got %v", win[len(win)-1])
	}
	for _, c := range win[:len(win)-1] {
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

func TestErrorSummarySkipsTrailers(t *testing.T) {
	out := "[h264_qsv @ 0000] Error initializing an internal MFX session: unsupported (-3)\r\n" +
		"[vost#0:0/h264_qsv @ 0000] [enc:h264_qsv @ 0000] Error while opening encoder - maybe incorrect parameters\r\n" +
		"[out#0/null @ 0000] Nothing was written into output file, because at least one of its streams received no packets.\r\n"
	got := errorSummary(out)
	if !strings.Contains(got, "MFX session: unsupported (-3)") || strings.Contains(got, "Nothing was written") {
		t.Fatalf("errorSummary = %q", got)
	}
	if l := lastLine(out); !strings.Contains(l, "Error while opening encoder") {
		t.Fatalf("lastLine = %q", l)
	}
	only := "[out#0/null @ 0000] Nothing was written into output file\n"
	if l := lastLine(only); !strings.Contains(l, "Nothing was written") {
		t.Fatalf("only trailers: lastLine = %q", l)
	}
}

func TestQSVSystemMemoryMode(t *testing.T) {
	defer qsvInit.Store(nil)
	enc, _ := encoderByName("h264_qsv")
	up := []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
	qsvInit.Store(&up)
	if f := enc.Filter(EncodeOpts{Width: 1280}); !strings.HasSuffix(f, "format=nv12,hwupload=extra_hw_frames=64") {
		t.Fatalf("upload mode filter = %q", f)
	}
	mem := []string{}
	qsvInit.Store(&mem)
	if a := enc.InitArgs(); len(a) != 0 {
		t.Fatalf("memory mode InitArgs = %v", a)
	}
	f := enc.Filter(EncodeOpts{Width: 1280})
	if strings.Contains(f, "hwupload") || !strings.HasSuffix(f, "format=nv12") {
		t.Fatalf("memory mode filter = %q", f)
	}
	if QSVInit() != "system memory (no hwupload)" {
		t.Fatalf("QSVInit = %q", QSVInit())
	}
}

func TestGPUFilterExpressions(t *testing.T) {
	g := &GPUDecode{Name: "x", scale: gpuPipelines()[1].scale} // qsv
	if f := g.Filter(EncodeOpts{Width: 1280}); f != "scale_qsv=w='min(1280,iw)':h='trunc(ow/a/2)*2':format=nv12" {
		t.Fatalf("qsv filter = %q", f)
	}
	if f := g.Filter(EncodeOpts{}); f != "scale_qsv=w=iw:h=ih:format=nv12" {
		t.Fatalf("qsv filter without scaling = %q", f)
	}
	for _, p := range gpuPipelines() {
		if p.pre("h264") == nil {
			t.Errorf("%s must decode h264", p.Name)
		}
	}
}

func TestPlanGPU(t *testing.T) {
	e := NewEncoders()
	pls := gpuPipelines()
	idx := func(name string) int {
		for i, p := range pls {
			if p.Name == name {
				return i
			}
		}
		t.Fatalf("no pipeline %s", name)
		return -1
	}
	e.gpu = gpuState{
		decode: map[string]map[string]int{"d3d12va": {"h264": idx("d3d12"), "hevc": idx("d3d12")}, "qsv": {"h264": idx("qsv-d3d11")}},
		encode: map[string]bool{"h264_qsv": true, "hevc_qsv": true, "h264_d3d12va": true, "hevc_d3d12va": true},
		broken: map[string]bool{},
	}
	qsv, _ := encoderByName("h264_qsv")
	hevcQSV, _ := encoderByName("hevc_qsv")
	if enc, g := e.PlanGPU(qsv, "h264", false, true); enc.Name != "h264_qsv" || g == nil || g.Name != "qsv-d3d11" {
		t.Fatalf("own family first: %s %+v", enc.Name, g)
	}
	if enc, g := e.PlanGPU(hevcQSV, "hevc", false, true); enc.Name != "hevc_d3d12va" || g == nil || g.Name != "d3d12" {
		t.Fatalf("switch to D3D12 for hevc sources: %s %+v", enc.Name, g)
	}
	if enc, g := e.PlanGPU(hevcQSV, "hevc", false, false); enc.Name != "hevc_qsv" || g != nil {
		t.Fatalf("an explicit encoder setting keeps its family: %s %+v", enc.Name, g)
	}
	if _, g := e.PlanGPU(qsv, "h264", true, true); g != nil {
		t.Fatal("tone mapping / burn-in needs CPU frames")
	}
	if enc, g := e.PlanGPU(qsv, "vp9", false, true); enc.Name != "h264_qsv" || g != nil {
		t.Fatalf("untested source codec keeps CPU decoding: %s %+v", enc.Name, g)
	}
	e.MarkGPUBroken("qsv-d3d11", "h264")
	if enc, g := e.PlanGPU(qsv, "h264", false, true); enc.Name != "h264_d3d12va" || g == nil {
		t.Fatalf("after a broken pipeline the next family: %s %+v", enc.Name, g)
	}
	sw, _ := encoderByName("libx264")
	if _, g := e.PlanGPU(sw, "h264", false, true); g != nil {
		t.Fatal("software encoder never gets a GPU pipeline")
	}
}

func TestBuildHLSArgsGPU(t *testing.T) {
	enc, _ := encoderByName("h264_qsv")
	g := &GPUDecode{Name: "qsv", Pre: []string{"-hwaccel", "qsv"}, scale: gpuPipelines()[1].scale}
	sp := HLSSpec{Item: &store.Item{Path: "in.mkv"}, Decision: Decision{Width: 1280, Bitrate: 4_000_000, AudioIdx: -1}, Encoder: enc, GPU: g}
	a := strings.Join(BuildHLSArgs(sp, "/tmp/x"), " ")
	if !strings.Contains(a, "-hwaccel qsv -i in.mkv") || !strings.Contains(a, "scale_qsv=") || strings.Contains(a, "hwupload") {
		t.Fatalf("gpu args: %s", a)
	}
	sp.Decision.ToneMap = true
	a = strings.Join(BuildHLSArgs(sp, "/tmp/x"), " ")
	if strings.Contains(a, "-hwaccel") || strings.Contains(a, "scale_qsv") {
		t.Fatalf("tone mapping must use CPU frames: %s", a)
	}
}
