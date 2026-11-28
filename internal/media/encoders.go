package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Encoder is a video encoder (H.264, HEVC or AV1) with the filter chain it needs.
type Encoder struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Hardware bool   `json:"hardware"`
	Codec    string `json:"codec"` // h264 | hevc | av1 (client caps names)
}

// Family is the hardware backend shared by an encoder's codec variants
// (h264_nvenc, hevc_nvenc, av1_nvenc → nvenc).
func (e Encoder) Family() string {
	if i := strings.IndexByte(e.Name, '_'); i >= 0 {
		return e.Name[i+1:]
	}
	return e.Name
}

// EncoderResult is the outcome of a real 1 s test encode.
type EncoderResult struct {
	Encoder
	OK    bool   `json:"ok"`
	Ms    int64  `json:"ms"`
	Error string `json:"error,omitempty"`
	// Skipped: not tried at all (ffmpeg lacks it, or it can't exist on this OS).
	Skipped bool `json:"skipped,omitempty"`
}

// VAAPIDevice is the render node used by h264_vaapi (KINORA_VAAPI_DEVICE).
var VAAPIDevice = "/dev/dri/renderD128"

// AllEncoders in preference order (hardware first). The H.264 ones are the
// baseline every client plays; HEVC/AV1 variants of the same hardware are used
// on top when the client reports the codec. libx264 must stay last.
var AllEncoders = []Encoder{
	{"h264_nvenc", "NVIDIA NVENC", true, "h264"},
	{"h264_qsv", "Intel Quick Sync (QSV)", true, "h264"},
	{"h264_vaapi", "VA-API (Intel/AMD)", true, "h264"},
	{"h264_videotoolbox", "Apple VideoToolbox", true, "h264"},
	{"h264_v4l2m2m", "V4L2 M2M (Raspberry Pi / ARM)", true, "h264"},
	{"hevc_nvenc", "NVIDIA NVENC (HEVC)", true, "hevc"},
	{"hevc_qsv", "Intel Quick Sync (HEVC)", true, "hevc"},
	{"hevc_vaapi", "VA-API (HEVC)", true, "hevc"},
	{"hevc_videotoolbox", "Apple VideoToolbox (HEVC)", true, "hevc"},
	{"av1_nvenc", "NVIDIA NVENC (AV1)", true, "av1"},
	{"av1_qsv", "Intel Quick Sync (AV1)", true, "av1"},
	{"av1_vaapi", "VA-API (AV1)", true, "av1"},
	{"libx264", "Szoftveres (libx264, veryfast)", false, "h264"},
}

// codecPreference is the order efficient codecs are tried in. HEVC comes before
// AV1: browsers may report AV1 with software-only decoding (dav1d), which can
// stutter on weak devices, while HEVC support practically means hardware decode.
var codecPreference = []string{"hevc", "av1"}

// EncodeOpts parameterizes a video encode.
type EncodeOpts struct {
	Bitrate int    // bps
	Width   int    // 0 = keep
	ToneMap bool   // HDR → SDR
	Overlay string // "[0:v][0:s:N]overlay" input label chain for image subtitle burn-in; "" = none
	GOP     bool   // constant 4 s GOP for HLS
}

// qsvInitCandidates are the ways to open an Intel Quick Sync device, best first.
//
// On Windows a bare "qsv=hw" lets the runtime pick the adapter and the D3D
// flavour: on a machine with more than one adapter (an Intel iGPU next to an
// NVIDIA/AMD card, or the Microsoft Basic Render Driver) it can open the wrong
// one, and DXVA2/D3D9 does not work at all in a Windows service (session 0).
// So on Windows we first open a D3D11 device on the Intel adapter
// (vendor 0x8086) and derive the QSV device from it, the same way other media
// servers do; the self-test keeps the first variant that actually encodes.
//
// The last candidate is empty: no device and no hwupload, the encoder gets
// nv12 frames in system memory and opens its own session. Some driver/ffmpeg
// pairs (an Arc B580 on Windows, for one) encode fine but cannot create the
// D3D11 frame pool hwupload needs ("Error initializing a child frames context").
func qsvInitCandidates(goos string) [][]string {
	plain := []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
	memory := []string{}
	if goos != "windows" {
		return [][]string{plain, memory}
	}
	return [][]string{
		{"-init_hw_device", "d3d11va=dx11:,vendor_id=0x8086", "-init_hw_device", "qsv=qs@dx11", "-filter_hw_device", "qs"},
		{"-init_hw_device", "qsv=hw,child_device_type=d3d11va", "-filter_hw_device", "hw"},
		plain,
		{"-init_hw_device", "qsv=hw,child_device_type=dxva2", "-filter_hw_device", "hw"},
		memory,
	}
}

// qsvInit is the QSV device setup chosen by SelfTest (nil: the first candidate).
var qsvInit atomic.Pointer[[]string]

func qsvInitArgs() []string {
	if p := qsvInit.Load(); p != nil {
		return *p
	}
	return qsvInitCandidates(runtime.GOOS)[0]
}

// InitArgs are global args placed before -i (hardware device setup).
func (e Encoder) InitArgs() []string {
	switch e.Family() {
	case "qsv":
		return append([]string(nil), qsvInitArgs()...)
	case "vaapi":
		return []string{"-init_hw_device", "vaapi=va:" + VAAPIDevice, "-filter_hw_device", "va"}
	}
	return nil
}

// Filter builds the -vf / -filter_complex chain (CPU filters first, then upload).
func (e Encoder) Filter(o EncodeOpts) string {
	return e.filter(o, len(qsvInitArgs()) > 0)
}

// filter: qsvUpload says whether a QSV device is set up to hwupload into; without
// one the QSV encoder takes nv12 frames from system memory.
func (e Encoder) filter(o EncodeOpts, qsvUpload bool) string {
	var f []string
	if o.ToneMap {
		f = append(f, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
			"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv")
	}
	if o.Width > 0 {
		f = append(f, fmt.Sprintf("scale=w='min(%d,iw)':h=-2", o.Width))
	}
	switch e.Family() {
	case "qsv":
		f = append(f, "format=nv12")
		if qsvUpload {
			f = append(f, "hwupload=extra_hw_frames=64")
		}
	case "vaapi":
		f = append(f, "format=nv12", "hwupload")
	default:
		f = append(f, "format=yuv420p")
	}
	return strings.Join(f, ",")
}

// Args are the codec arguments (after the filter).
//
// Rate control is quality based with the bitrate as a ceiling: the target
// bitrate is the client/profile limit, so it is used as -maxrate and the
// encoder spends less on easy scenes instead of padding every segment to it.
func (e Encoder) Args(o EncodeOpts) []string {
	br := o.Bitrate
	if br <= 0 {
		br = 4_000_000
	}
	ceiling := []string{"-maxrate", fmt.Sprint(br), "-bufsize", fmt.Sprint(br * 2)}
	// VBR for encoders without a capped constant-quality mode: average at 3/4 of the ceiling.
	vbr := append([]string{"-b:v", fmt.Sprint(br / 4 * 3)}, ceiling...)
	var a []string
	switch e.Name {
	case "libx264":
		a = []string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-level", "4.1", "-crf", "21"}
		a = append(a, ceiling...)
		if o.GOP {
			a = append(a, "-sc_threshold", "0")
		}
	case "h264_nvenc":
		a = []string{"-c:v", "h264_nvenc", "-preset", "p5", "-tune", "hq", "-profile:v", "high", "-rc", "vbr", "-cq", "23", "-b:v", "0"}
		a = append(a, ceiling...)
	case "hevc_nvenc":
		a = []string{"-c:v", "hevc_nvenc", "-preset", "p5", "-tune", "hq", "-profile:v", "main", "-rc", "vbr", "-cq", "25", "-b:v", "0"}
		a = append(a, ceiling...)
	case "av1_nvenc":
		a = append([]string{"-c:v", "av1_nvenc", "-preset", "p5", "-tune", "hq", "-rc", "vbr"}, vbr...)
	case "h264_qsv":
		a = append([]string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "high"}, vbr...)
	case "hevc_qsv":
		a = append([]string{"-c:v", "hevc_qsv", "-preset", "veryfast", "-profile:v", "main"}, vbr...)
	case "av1_qsv":
		a = append([]string{"-c:v", "av1_qsv", "-preset", "veryfast"}, vbr...)
	case "h264_vaapi":
		a = append([]string{"-c:v", "h264_vaapi", "-profile:v", "high", "-rc_mode", "VBR"}, vbr...)
	case "hevc_vaapi":
		a = append([]string{"-c:v", "hevc_vaapi", "-profile:v", "main", "-rc_mode", "VBR"}, vbr...)
	case "av1_vaapi":
		a = append([]string{"-c:v", "av1_vaapi", "-rc_mode", "VBR"}, vbr...)
	case "h264_videotoolbox":
		a = append([]string{"-c:v", "h264_videotoolbox", "-profile:v", "high", "-realtime", "1"}, vbr...)
	case "hevc_videotoolbox":
		a = append([]string{"-c:v", "hevc_videotoolbox", "-profile:v", "main", "-realtime", "1"}, vbr...)
	case "h264_v4l2m2m":
		a = []string{"-c:v", "h264_v4l2m2m", "-b:v", fmt.Sprint(br)}
		a = append(a, ceiling...)
	}
	if e.Codec == "hevc" {
		a = append(a, "-tag:v", "hvc1") // Apple players only accept the hvc1 sample entry
	}
	if o.GOP {
		a = append(a, "-g", "48", "-keyint_min", "48", "-force_key_frames", "expr:gte(t,n_forced*4)")
	}
	return a
}

// Encoders holds self-test results and picks the encoder to use.
type Encoders struct {
	mu       sync.RWMutex
	results  []EncoderResult
	testedAt time.Time
	toneMap  bool
}

// NewEncoders creates an empty registry (call SelfTest).
func NewEncoders() *Encoders { return &Encoders{} }

// Results returns the last self-test results.
func (e *Encoders) Results() ([]EncoderResult, time.Time) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]EncoderResult(nil), e.results...), e.testedAt
}

// ToneMap reports whether ffmpeg has zscale+tonemap.
func (e *Encoders) ToneMap() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.toneMap
}

// SelfTest encodes 1 s of testsrc2 with every encoder in parallel. Encoders that
// ffmpeg doesn't list are reported as unavailable without running.
func (e *Encoders) SelfTest(ctx context.Context) []EncoderResult {
	listed := ffmpegList(ctx, "-encoders")
	filters := ffmpegList(ctx, "-filters")
	results := make([]EncoderResult, len(AllEncoders))
	var qsvProbe *EncoderResult
	if strings.Contains(listed, " h264_qsv ") {
		r := probeQSV(ctx, qsvInitCandidates(runtime.GOOS))
		qsvProbe = &r
	}
	var wg sync.WaitGroup
	for i, enc := range AllEncoders {
		results[i] = EncoderResult{Encoder: enc}
		if !strings.Contains(listed, " "+enc.Name+" ") {
			results[i].Error = "az ffmpeg nem tartalmazza ezt az encodert"
			results[i].Skipped = true
			continue
		}
		if enc.Family() == "vaapi" && runtime.GOOS != "linux" {
			results[i].Error = "a VAAPI csak Linuxon érhető el"
			results[i].Skipped = true
			continue
		}
		if enc.Name == "h264_qsv" && qsvProbe != nil {
			results[i] = *qsvProbe
			continue
		}
		if enc.Family() == "vaapi" {
			if _, err := os.Stat(VAAPIDevice); err != nil {
				results[i].Error = VAAPIDevice + " nem elérhető (Dockerben: devices: /dev/dri)"
				continue
			}
		}
		wg.Add(1)
		go func(i int, enc Encoder) {
			defer wg.Done()
			results[i] = testEncoder(ctx, enc)
		}(i, enc)
	}
	wg.Wait()
	e.mu.Lock()
	e.results = results
	e.testedAt = time.Now()
	e.toneMap = strings.Contains(filters, " zscale ") && strings.Contains(filters, " tonemap ")
	e.mu.Unlock()
	return results
}

func ffmpegList(ctx context.Context, what string) string {
	out, _ := exec.CommandContext(ctx, FFmpeg, "-hide_banner", what).Output()
	return string(out)
}

// probeQSV tries the QSV device setups in order with h264_qsv and keeps the
// first that encodes, so hevc_qsv/av1_qsv and real transcodes use it too. When
// none works, the first (preferred) variant stays and its error is reported.
func probeQSV(ctx context.Context, candidates [][]string) EncoderResult {
	enc, _ := encoderByName("h264_qsv")
	var first EncoderResult
	for i, dev := range candidates {
		r := testEncoderWith(ctx, enc, dev)
		if r.OK {
			qsvInit.Store(&candidates[i])
			return r
		}
		if i == 0 {
			first = r
		}
		if ctx.Err() != nil {
			break
		}
	}
	qsvInit.Store(&candidates[0])
	if len(candidates) > 1 {
		first.Error = fmt.Sprintf("%s (%d QSV-eszközbeállítás közül egyik sem működött)", first.Error, len(candidates))
	}
	return first
}

// QSVInit is the QSV device setup in use (for logs).
func QSVInit() string {
	if a := qsvInitArgs(); len(a) > 0 {
		return strings.Join(a, " ")
	}
	return "system memory (no hwupload)"
}

func testEncoder(ctx context.Context, enc Encoder) EncoderResult {
	return testEncoderWith(ctx, enc, enc.InitArgs())
}

func testEncoderWith(ctx context.Context, enc Encoder, dev []string) EncoderResult {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	opts := EncodeOpts{Bitrate: 2_000_000, Width: 1280, GOP: true}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, dev...)
	args = append(args, "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24", "-t", "1", "-vf", enc.filter(opts, len(dev) > 0))
	args = append(args, enc.Args(opts)...)
	args = append(args, "-f", "null", "-")
	t := time.Now()
	out, err := exec.CommandContext(ctx, FFmpeg, args...).CombinedOutput()
	r := EncoderResult{Encoder: enc, Ms: time.Since(t).Milliseconds()}
	if err != nil {
		msg := errorSummary(string(out))
		if ctx.Err() != nil {
			msg = "időtúllépés (20 mp)"
		}
		r.Error = firstNonEmpty(msg, err.Error())
		return r
	}
	r.OK = true
	return r
}

// Pick returns the H.264 encoder to use: configured one if it works, otherwise
// the first working hardware encoder, otherwise libx264. A configured HEVC/AV1
// encoder selects its hardware family's H.264 variant here.
func (e *Encoders) Pick(configured string) Encoder {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pickH264(configured)
}

func (e *Encoders) okNames() map[string]bool {
	ok := map[string]bool{}
	for _, r := range e.results {
		if r.OK {
			ok[r.Name] = true
		}
	}
	return ok
}

func encoderByName(n string) (Encoder, bool) {
	for _, enc := range AllEncoders {
		if enc.Name == n {
			return enc, true
		}
	}
	return Encoder{}, false
}

func (e *Encoders) pickH264(configured string) Encoder {
	ok := e.okNames()
	if configured != "" && configured != "auto" {
		if c, found := encoderByName(configured); found {
			if c.Codec != "h264" {
				c, found = encoderByName("h264_" + c.Family())
			}
			if found && ok[c.Name] {
				return c
			}
		}
	}
	for _, enc := range AllEncoders {
		if enc.Hardware && enc.Codec == "h264" && ok[enc.Name] {
			return enc
		}
	}
	enc, _ := encoderByName("libx264")
	return enc
}

// PickFor returns the most efficient encoder for a client: the HEVC or AV1
// variant of the chosen H.264 encoder's hardware when it passed the self-test
// and the client reported the codec, otherwise the H.264 encoder itself.
// With codecMode "h264" (setting video_codec) only H.264 is produced.
func (e *Encoders) PickFor(configured, codecMode string, caps Caps) Encoder {
	e.mu.RLock()
	defer e.mu.RUnlock()
	base := e.pickH264(configured)
	if codecMode == "h264" || !base.Hardware {
		return base
	}
	ok := e.okNames()
	for _, codec := range codecPreference {
		if !has(caps.Video, codec) {
			continue
		}
		if enc, found := encoderByName(codec + "_" + base.Family()); found && ok[enc.Name] {
			return enc
		}
	}
	return base
}
