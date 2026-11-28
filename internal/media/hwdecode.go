package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// A GPU pipeline keeps the frames on the graphics card: hardware decode, GPU
// scaling and hardware encode, so the CPU only demuxes and muxes. Which ones
// work depends on the card, the driver and the ffmpeg build (an Arc B580 on
// Windows, for one, encodes fine but can't hwupload into D3D11 frames), so the
// self-test tries each one on real sample files and only those that pass are
// used. A session that still fails on one is restarted with CPU decoding.

// gpuPipeline is one way to decode and scale on a GPU for an encoder family.
type gpuPipeline struct {
	Name   string // for logs and the admin page
	Family string // encoder family it feeds (qsv, d3d12va, nvenc, vaapi)
	GOOS   string // "" any OS, else only this one
	Filter string // the scaler; the pipeline is skipped when ffmpeg lacks it
	// pre are the args before -i for a source codec (ffprobe name); nil: can't decode it
	pre func(src string) []string
	// scale is the GPU filter chain producing nv12 at w×h (ffmpeg expressions)
	scale func(w, h string) string
}

// GPUDecode is the chosen pipeline for one session.
type GPUDecode struct {
	Name  string
	Pre   []string
	scale func(w, h string) string
}

// Filter is the GPU filter chain for the encode options (no CPU filters are allowed here).
func (g *GPUDecode) Filter(o EncodeOpts) string {
	w, h := "iw", "ih"
	if o.Width > 0 {
		// even height from the aspect ratio: scale_qsv has no -2
		w, h = fmt.Sprintf("'min(%d,iw)'", o.Width), "'trunc(ow/a/2)*2'"
	}
	return g.scale(w, h)
}

// gpuDecodeCodecs are the source codecs the self-test makes samples of. Other
// codecs (VP9, MPEG-2, VC-1…) keep CPU decoding.
var gpuDecodeCodecs = []string{"h264", "hevc", "av1"}

var qsvDecoders = map[string]string{"h264": "h264_qsv", "hevc": "hevc_qsv", "av1": "av1_qsv", "vp9": "vp9_qsv", "mpeg2video": "mpeg2_qsv", "vc1": "vc1_qsv"}

func anyCodec(args ...string) func(string) []string {
	return func(string) []string { return append([]string(nil), args...) }
}

// gpuPipelines in preference order within a family.
func gpuPipelines() []gpuPipeline {
	return []gpuPipeline{
		{
			// D3D11 decode on the Intel adapter, mapped (not uploaded) into QSV
			Name: "qsv-d3d11", Family: "qsv", GOOS: "windows", Filter: "scale_qsv",
			pre: anyCodec("-init_hw_device", "d3d11va=dx11:,vendor_id=0x8086", "-init_hw_device", "qsv=qs@dx11",
				"-filter_hw_device", "qs", "-hwaccel", "d3d11va", "-hwaccel_device", "dx11", "-hwaccel_output_format", "d3d11"),
			scale: func(w, h string) string {
				return fmt.Sprintf("hwmap=derive_device=qsv,scale_qsv=w=%s:h=%s:format=nv12", w, h)
			},
		},
		{
			Name: "qsv", Family: "qsv", Filter: "scale_qsv",
			pre: func(src string) []string {
				dec, ok := qsvDecoders[src]
				if !ok {
					return nil
				}
				return []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw",
					"-hwaccel", "qsv", "-hwaccel_output_format", "qsv", "-c:v", dec}
			},
			scale: func(w, h string) string { return fmt.Sprintf("scale_qsv=w=%s:h=%s:format=nv12", w, h) },
		},
		{
			Name: "d3d12", Family: "d3d12va", GOOS: "windows", Filter: "scale_d3d12",
			pre: anyCodec("-init_hw_device", "d3d12va=dx12", "-filter_hw_device", "dx12",
				"-hwaccel", "d3d12va", "-hwaccel_device", "dx12", "-hwaccel_output_format", "d3d12"),
			scale: func(w, h string) string { return fmt.Sprintf("scale_d3d12=w=%s:h=%s:format=nv12", w, h) },
		},
		{
			Name: "cuda", Family: "nvenc", Filter: "scale_cuda",
			pre:   anyCodec("-hwaccel", "cuda", "-hwaccel_output_format", "cuda"),
			scale: func(w, h string) string { return fmt.Sprintf("scale_cuda=w=%s:h=%s:format=nv12", w, h) },
		},
		{
			Name: "videotoolbox", Family: "videotoolbox", GOOS: "darwin", Filter: "scale_vt",
			pre:   anyCodec("-hwaccel", "videotoolbox", "-hwaccel_output_format", "videotoolbox_vld"),
			scale: func(w, h string) string { return fmt.Sprintf("scale_vt=w=%s:h=%s", w, h) },
		},
		{
			Name: "vaapi", Family: "vaapi", GOOS: "linux", Filter: "scale_vaapi",
			pre: func(string) []string {
				return []string{"-init_hw_device", "vaapi=va:" + VAAPIDevice, "-filter_hw_device", "va",
					"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi"}
			},
			scale: func(w, h string) string { return fmt.Sprintf("scale_vaapi=w=%s:h=%s:format=nv12", w, h) },
		},
	}
}

// gpuState is what the GPU self-test found. Guarded by Encoders.mu.
type gpuState struct {
	// decode[family][source codec] = index into gpuPipelines()
	decode map[string]map[string]int
	// encode[encoder name]: the encoder works fed by its family's GPU pipeline
	encode map[string]bool
	// broken[pipeline+"/"+codec]: failed in a real session, not tried again
	broken map[string]bool
}

// GPUPipelineResult is one self-test line for the admin page and the log.
type GPUPipelineResult struct {
	Pipeline string `json:"pipeline"`
	Codec    string `json:"codec"` // source codec, or "encode:<encoder>"
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// gpuSelfTest makes short sample files and tries every pipeline whose encoder
// family ffmpeg has. listed/filters are ffmpeg's -encoders/-filters output.
func (e *Encoders) gpuSelfTest(ctx context.Context, listed, filters string, results []EncoderResult) []GPUPipelineResult {
	st := gpuState{decode: map[string]map[string]int{}, encode: map[string]bool{}, broken: map[string]bool{}}
	var out []GPUPipelineResult
	defer func() {
		e.mu.Lock()
		e.gpu = st
		e.gpuResults = out
		e.mu.Unlock()
	}()
	dir, err := os.MkdirTemp("", "kinora-gputest-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	samples := makeSamples(ctx, dir, listed, results)
	h264, ok := samples["h264"]
	if !ok {
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	byFamily := map[string][]int{}
	pls := gpuPipelines()
	for i, p := range pls {
		if (p.GOOS != "" && p.GOOS != runtime.GOOS) || !strings.Contains(filters, " "+p.Filter+" ") ||
			!strings.Contains(listed, " h264_"+p.Family+" ") {
			continue
		}
		if p.Family == "vaapi" {
			if _, err := os.Stat(VAAPIDevice); err != nil {
				continue
			}
		}
		byFamily[p.Family] = append(byFamily[p.Family], i)
	}
	// families in parallel; within a family the pipelines in order, first good one per codec wins
	for fam, idx := range byFamily {
		wg.Add(1)
		go func(fam string, idx []int) {
			defer wg.Done()
			for _, i := range idx {
				p := pls[i]
				// the family's encoders fed by this pipeline (from an H.264 sample)
				var encOK []string
				for _, codec := range []string{"h264", "hevc", "av1"} {
					enc, found := encoderByName(codec + "_" + fam)
					if !found || !strings.Contains(listed, " "+enc.Name+" ") {
						continue
					}
					r := runGPUTest(ctx, p, "h264", h264, enc)
					mu.Lock()
					out = append(out, GPUPipelineResult{Pipeline: p.Name, Codec: "encode:" + enc.Name, OK: r == "", Error: r})
					mu.Unlock()
					if r == "" {
						encOK = append(encOK, enc.Name)
					} else if codec == "h264" {
						break // without H.264 the pipeline is useless
					}
				}
				if len(encOK) == 0 || encOK[0] != "h264_"+fam {
					continue
				}
				h264enc, _ := encoderByName("h264_" + fam)
				dec := map[string]bool{"h264": true}
				for _, src := range gpuDecodeCodecs[1:] {
					path, ok := samples[src]
					if !ok || p.pre(src) == nil {
						continue
					}
					r := runGPUTest(ctx, p, src, path, h264enc)
					mu.Lock()
					out = append(out, GPUPipelineResult{Pipeline: p.Name, Codec: src, OK: r == "", Error: r})
					mu.Unlock()
					dec[src] = r == ""
				}
				mu.Lock()
				for _, n := range encOK {
					st.encode[n] = true
				}
				if st.decode[fam] == nil {
					st.decode[fam] = map[string]int{}
				}
				for src, ok := range dec {
					if _, taken := st.decode[fam][src]; ok && !taken {
						st.decode[fam][src] = i
					}
				}
				done := len(st.decode[fam]) == len(gpuDecodeCodecs)
				mu.Unlock()
				if done {
					return // every sample codec covered; later pipelines not needed
				}
			}
		}(fam, idx)
	}
	wg.Wait()
	return out
}

// makeSamples writes 1 s test clips per source codec, with a software encoder
// when ffmpeg has one, else a hardware encoder that passed the self-test.
func makeSamples(ctx context.Context, dir, listed string, results []EncoderResult) map[string]string {
	soft := map[string][]string{
		"h264": {"-c:v", "libx264", "-preset", "ultrafast"},
		"hevc": {"-c:v", "libx265", "-preset", "ultrafast"},
		"av1":  {"-c:v", "libsvtav1", "-preset", "12"},
	}
	out := map[string]string{}
	for _, codec := range gpuDecodeCodecs {
		path := filepath.Join(dir, "sample_"+codec+".mp4")
		var tries [][]string
		if a := soft[codec]; strings.Contains(listed, " "+a[1]+" ") {
			tries = append(tries, append([]string{"-vf", "format=yuv420p"}, a...))
		}
		if codec == "av1" && strings.Contains(listed, " libaom-av1 ") {
			tries = append(tries, []string{"-vf", "format=yuv420p", "-c:v", "libaom-av1", "-cpu-used", "8"})
		}
		for _, r := range results {
			if r.OK && r.Hardware && r.Codec == codec {
				o := EncodeOpts{Bitrate: 2_000_000}
				tries = append(tries, append(append(r.InitArgs(), "-vf", r.Filter(o)), r.Args(o)...))
			}
		}
		for _, t := range tries {
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
			args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}
			// init args (if any) must come before -i: split them off the front
			pre, rest := splitInit(t)
			args = append(args, pre...)
			args = append(args, "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24", "-t", "1")
			args = append(args, rest...)
			args = append(args, "-g", "12", path)
			err := exec.CommandContext(c, FFmpeg, args...).Run()
			cancel()
			if err == nil {
				out[codec] = path
				break
			}
		}
	}
	return out
}

// splitInit separates the leading -init_hw_device/-filter_hw_device pairs.
func splitInit(a []string) (pre, rest []string) {
	i := 0
	for i+1 < len(a) && (a[i] == "-init_hw_device" || a[i] == "-filter_hw_device") {
		i += 2
	}
	return a[:i], a[i:]
}

// runGPUTest decodes the sample on the GPU, scales it there and encodes it; "" = OK.
func runGPUTest(ctx context.Context, p gpuPipeline, src, path string, enc Encoder) string {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	g := &GPUDecode{Name: p.Name, Pre: p.pre(src), scale: p.scale}
	opts := EncodeOpts{Bitrate: 2_000_000, Width: 640, GOP: true}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, g.Pre...)
	args = append(args, "-i", path, "-vf", g.Filter(opts))
	args = append(args, enc.Args(opts)...)
	args = append(args, "-f", "null", "-")
	out, err := exec.CommandContext(ctx, FFmpeg, args...).CombinedOutput()
	if err == nil {
		return ""
	}
	if ctx.Err() != nil {
		return "időtúllépés (20 mp)"
	}
	return firstNonEmpty(errorSummary(string(out)), err.Error())
}

// GPUResults returns the GPU pipeline self-test lines.
func (e *Encoders) GPUResults() []GPUPipelineResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]GPUPipelineResult(nil), e.gpuResults...)
}

// PlanGPU picks how a session encodes: when no CPU filter is needed (tone
// mapping, subtitle burn-in) and a GPU pipeline decodes the source codec for
// enc's family, that pipeline. With switchFamily (the encoder setting is auto)
// it may move to another family's encoder of the same codec that has a full
// GPU pipeline. Otherwise enc with CPU decoding (nil).
func (e *Encoders) PlanGPU(enc Encoder, srcCodec string, cpuFilters, switchFamily bool) (Encoder, *GPUDecode) {
	if cpuFilters || !enc.Hardware {
		return enc, nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	pls := gpuPipelines()
	try := func(c Encoder) *GPUDecode {
		if !e.gpu.encode[c.Name] {
			return nil
		}
		i, ok := e.gpu.decode[c.Family()][srcCodec]
		if !ok || e.gpu.broken[pls[i].Name+"/"+srcCodec] {
			return nil
		}
		pre := pls[i].pre(srcCodec)
		if pre == nil {
			return nil
		}
		return &GPUDecode{Name: pls[i].Name, Pre: pre, scale: pls[i].scale}
	}
	if g := try(enc); g != nil {
		return enc, g
	}
	if switchFamily {
		for _, c := range AllEncoders {
			if c.Hardware && c.Codec == enc.Codec && c.Name != enc.Name {
				if g := try(c); g != nil {
					return c, g
				}
			}
		}
	}
	return enc, nil
}

// MarkGPUBroken stops using a pipeline for a source codec after a real session failed with it.
func (e *Encoders) MarkGPUBroken(pipeline, srcCodec string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.gpu.broken == nil {
		e.gpu.broken = map[string]bool{}
	}
	e.gpu.broken[pipeline+"/"+srcCodec] = true
}
