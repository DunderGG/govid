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
