// gpu_capability.go — Runtime GPU acceleration capability detection.
//
// Responsibilities:
//   - GPUBackend identifiers matching docs/gpu-acceleration.md §3.
//   - GPUCapabilityService: probes the bundled ffmpeg binary once per app run
//     and caches, per backend, whether the target H.264 encoder is compiled
//     into ffmpeg and whether it actually initializes on this machine.
//
// Scope: final-encode detection only (docs/gpu-acceleration.md §7). No
// command-builder integration, user-facing settings, or fallback logic yet —
// those are separate, later roadmap items.
package main

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPUBackend is a stable configuration identifier for a GPU acceleration
// pipeline, as defined in docs/gpu-acceleration.md §3.
type GPUBackend string

const (
	BackendAuto         GPUBackend = "auto"
	BackendOff          GPUBackend = "off"
	BackendNVIDIA       GPUBackend = "nvidia"
	BackendIntel        GPUBackend = "intel"
	BackendAMD          GPUBackend = "amd"
	BackendVAAPI        GPUBackend = "vaapi"
	BackendVideoToolbox GPUBackend = "videotoolbox"
)

// BackendCapability records what was found for a single backend.
type BackendCapability struct {
	Backend    GPUBackend
	Applicable bool // true when this backend targets the current runtime.GOOS
	Compiled   bool // true when ffmpeg -encoders lists the target encoder
	Available  bool // true when the runtime probe successfully encoded a frame
	Encoder    string
	Reason     string // human-readable reason when Available is false
}

// backendDef pairs a backend with the OSes it targets and its H.264 encoder,
// per the initial platform scope in docs/gpu-acceleration.md §3.
type backendDef struct {
	Backend GPUBackend
	OSes    []string
	Encoder string
}

var backendDefs = []backendDef{
	{BackendNVIDIA, []string{"windows", "linux"}, "h264_nvenc"},
	{BackendIntel, []string{"windows", "linux"}, "h264_qsv"},
	{BackendAMD, []string{"windows"}, "h264_amf"},
	{BackendVAAPI, []string{"linux"}, "h264_vaapi"},
	{BackendVideoToolbox, []string{"darwin"}, "h264_videotoolbox"},
}

// GPUCapabilityService detects and caches GPU backend capability for the
// resolved ffmpeg binary. Detection runs once per app run; call Detect to
// populate the cache and Capability to read a single backend's result.
type GPUCapabilityService struct {
	ffmpegPath string

	mu       sync.Mutex
	cache    map[GPUBackend]BackendCapability
	detected bool
}

// NewGPUCapabilityService returns a GPUCapabilityService for the given
// resolved ffmpeg binary path.
func NewGPUCapabilityService(ffmpegPath string) *GPUCapabilityService {
	return &GPUCapabilityService{ffmpegPath: ffmpegPath}
}

// Detect runs capability detection on first call and caches the result for
// the lifetime of the service; subsequent calls return the cached copy.
func (svc *GPUCapabilityService) Detect(ctx context.Context) map[GPUBackend]BackendCapability {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	if svc.detected {
		return svc.cloneCacheLocked()
	}

	encodersOut := svc.runFFmpeg(ctx, "-hide_banner", "-encoders")

	cache := make(map[GPUBackend]BackendCapability, len(backendDefs))
	for _, def := range backendDefs {
		bc := BackendCapability{Backend: def.Backend, Encoder: def.Encoder}

		bc.Applicable = osIn(def.OSes, runtime.GOOS)
		if !bc.Applicable {
			bc.Reason = "not applicable on " + runtime.GOOS
			cache[def.Backend] = bc
			continue
		}

		bc.Compiled = isEncoderCompiled(encodersOut, def.Encoder)
		if !bc.Compiled {
			bc.Reason = "ffmpeg build does not include " + def.Encoder
			cache[def.Backend] = bc
			continue
		}

		bc.Available, bc.Reason = probeEncoder(ctx, svc.ffmpegPath, def.Encoder)
		cache[def.Backend] = bc
	}

	svc.cache = cache
	svc.detected = true
	return svc.cloneCacheLocked()
}

// Capability returns the cached result for a single backend. ok is false if
// Detect has not completed yet.
func (svc *GPUCapabilityService) Capability(backend GPUBackend) (BackendCapability, bool) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	if !svc.detected {
		return BackendCapability{}, false
	}
	bc, ok := svc.cache[backend]
	return bc, ok
}

// cloneCacheLocked returns a copy of the cache so callers cannot mutate the
// service's internal state. Must be called with svc.mu held.
func (svc *GPUCapabilityService) cloneCacheLocked() map[GPUBackend]BackendCapability {
	result := make(map[GPUBackend]BackendCapability, len(svc.cache))
	for backend, bc := range svc.cache {
		result[backend] = bc
	}
	return result
}

// runFFmpeg runs the ffmpeg binary with the given args and returns its
// combined output, or "" if it could not be started or run successfully.
func (svc *GPUCapabilityService) runFFmpeg(ctx context.Context, args ...string) string {
	cmd := exec.CommandContext(ctx, svc.ffmpegPath, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// osIn reports whether goos appears in oses.
func osIn(oses []string, goos string) bool {
	for _, os := range oses {
		if os == goos {
			return true
		}
	}
	return false
}

// isEncoderCompiled reports whether encoderName appears as an exact encoder
// name in the output of `ffmpeg -encoders`. Each encoder line has the form
// " V....D <name>            <description>"; matching the exact name field
// avoids false positives from substring matches (docs/gpu-acceleration.md §4).
func isEncoderCompiled(encodersOutput, encoderName string) bool {
	for _, line := range strings.Split(encodersOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == encoderName {
			return true
		}
	}
	return false
}

// probeEncoder attempts to encode a single synthetic frame with encoderName.
// It reports whether the encoder initialized successfully and, if not, a
// short human-readable reason drawn from ffmpeg's error output.
func probeEncoder(ctx context.Context, ffmpegPath, encoderName string) (bool, string) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:d=1",
		"-frames:v", "1", "-c:v", encoderName,
		"-f", "null", "-",
	)
	hideWindow(cmd)

	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, ""
	}

	reason := strings.TrimSpace(string(out))
	if reason == "" {
		reason = err.Error()
	}
	if idx := strings.LastIndex(reason, "\n"); idx != -1 {
		reason = reason[idx+1:]
	}
	return false, reason
}

// ── Encoder command builder ─────────────────────────────────────────────────
// Quality values below target visual parity with the current CPU baseline
// (libx264 -crf 18 -preset slower); they are approximate and expected to be
// refined by the benchmarking roadmap item, not final tuned values.
const (
	qualityNVENCConstQP     = 19
	qualityQSVGlobalQuality = 20
	qualityAMFQPI           = 20
	qualityAMFQPP           = 22
	qualityVAAPIQP          = 20
	qualityVideoToolboxQV   = 65
)

// backendPriority is the fixed selection order used to resolve "auto".
var backendPriority = []GPUBackend{
	BackendNVIDIA, BackendIntel, BackendAMD, BackendVAAPI, BackendVideoToolbox,
}

// EncoderPlan is the resolved -c:v argument set for a single post-processing
// job, along with a human-readable label for logs and job summaries.
type EncoderPlan struct {
	Args    []string
	Label   string
	UsedGPU bool
	Backend GPUBackend
}

// PlanEncoder resolves which encoder to use for a post-processing job and
// always returns a runnable plan: GPU args when the requested backend (or,
// for "auto", the highest-priority available backend) is usable, otherwise
// the existing CPU encoder for containerExt. WebM always stays on the CPU
// VP9 encoder regardless of the requested backend (docs/gpu-acceleration.md
// §7 — no NVENC/AMF VP9 encoder in the bundled build).
func PlanEncoder(requested GPUBackend, capabilities map[GPUBackend]BackendCapability, containerExt string) EncoderPlan {
	if strings.ToLower(containerExt) == ".webm" {
		return cpuEncoderPlan(containerExt)
	}

	resolved := requested
	if resolved == BackendAuto {
		var ok bool
		resolved, ok = firstAvailableBackend(capabilities)
		if !ok {
			return cpuEncoderPlan(containerExt)
		}
	}

	if resolved == "" || resolved == BackendOff {
		return cpuEncoderPlan(containerExt)
	}

	if cap, ok := capabilities[resolved]; ok && cap.Available {
		return gpuEncoderPlan(resolved)
	}
	return cpuEncoderPlan(containerExt)
}

// firstAvailableBackend returns the highest-priority backend that is
// Available in capabilities, for resolving BackendAuto.
func firstAvailableBackend(capabilities map[GPUBackend]BackendCapability) (GPUBackend, bool) {
	for _, backend := range backendPriority {
		if cap, ok := capabilities[backend]; ok && cap.Available {
			return backend, true
		}
	}
	return "", false
}

// cpuEncoderPlan returns the existing CPU encoder args for containerExt:
// libvpx-vp9 for WebM, libx264 otherwise.
func cpuEncoderPlan(containerExt string) EncoderPlan {
	if strings.ToLower(containerExt) == ".webm" {
		return EncoderPlan{
			Args:  []string{"-c:v", "libvpx-vp9", "-crf", "31", "-b:v", "0", "-deadline", "good", "-cpu-used", "2"},
			Label: "Re-encode (libvpx-vp9, CRF 31)",
		}
	}
	return EncoderPlan{
		Args:  []string{"-c:v", "libx264", "-crf", "18", "-preset", "slower"},
		Label: "Re-encode (libx264, CRF 18, slower)",
	}
}

// gpuEncoderPlan returns the -c:v args and label for the given backend's
// H.264 encoder using its constant-quality mode.
func gpuEncoderPlan(backend GPUBackend) EncoderPlan {
	plan := EncoderPlan{UsedGPU: true, Backend: backend}
	switch backend {
	case BackendNVIDIA:
		plan.Args = []string{"-c:v", "h264_nvenc", "-rc", "constqp", "-qp", strconv.Itoa(qualityNVENCConstQP)}
		plan.Label = "Re-encode (NVIDIA NVENC, CQP " + strconv.Itoa(qualityNVENCConstQP) + ")"
	case BackendIntel:
		plan.Args = []string{"-c:v", "h264_qsv", "-global_quality", strconv.Itoa(qualityQSVGlobalQuality)}
		plan.Label = "Re-encode (Intel QSV, Q" + strconv.Itoa(qualityQSVGlobalQuality) + ")"
	case BackendAMD:
		plan.Args = []string{"-c:v", "h264_amf", "-qp_i", strconv.Itoa(qualityAMFQPI), "-qp_p", strconv.Itoa(qualityAMFQPP)}
		plan.Label = "Re-encode (AMD AMF, QP " + strconv.Itoa(qualityAMFQPI) + "/" + strconv.Itoa(qualityAMFQPP) + ")"
	case BackendVAAPI:
		plan.Args = []string{"-c:v", "h264_vaapi", "-qp", strconv.Itoa(qualityVAAPIQP)}
		plan.Label = "Re-encode (VAAPI, QP " + strconv.Itoa(qualityVAAPIQP) + ")"
	case BackendVideoToolbox:
		plan.Args = []string{"-c:v", "h264_videotoolbox", "-q:v", strconv.Itoa(qualityVideoToolboxQV)}
		plan.Label = "Re-encode (VideoToolbox, Q" + strconv.Itoa(qualityVideoToolboxQV) + ")"
	default:
		return cpuEncoderPlan("")
	}
	return plan
}

// ── UI label mapping ─────────────────────────────────────────────────────────

// backendLabels maps each GPUBackend to its display label in the UI.
var backendLabels = map[GPUBackend]string{
	BackendAuto:         "Auto (Recommended)",
	BackendOff:          "Off",
	BackendNVIDIA:       "NVIDIA",
	BackendIntel:        "Intel",
	BackendAMD:          "AMD",
	BackendVAAPI:        "VAAPI",
	BackendVideoToolbox: "VideoToolbox",
}

// GPUBackendOptions returns the backend labels applicable to the current
// runtime.GOOS, in priority order, for use in a UI selector.
func GPUBackendOptions() []string {
	options := []string{backendLabels[BackendAuto], backendLabels[BackendOff]}
	for _, def := range backendDefs {
		if osIn(def.OSes, runtime.GOOS) {
			options = append(options, backendLabels[def.Backend])
		}
	}
	return options
}

// GPUBackendFromLabel resolves a UI label back to its GPUBackend identifier,
// falling back to BackendAuto for an unrecognized or empty label.
func GPUBackendFromLabel(label string) GPUBackend {
	for backend, l := range backendLabels {
		if l == label {
			return backend
		}
	}
	return BackendAuto
}
