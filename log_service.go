// log_service.go — Centralizes session log/error log routing, rotation policy,
// and buffer-limit management.
//
// Responsibilities:
//   - LogService: owns the session-log file handle, mutexes, buffer-limit
//     management, daily rotation policy (daily filename scheme), and
//     error-line routing.
//   - Package-level helpers (IsErrorLine, ParseBufferLimit, SessionLogPath,
//     ErrorLogPath) that callers can use without an instance.
package main

import (
	"cmp"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LogService owns the session log and error log routing, rotation policy, and
// UI buffer-limit management. It has no UI or Fyne dependency.
type LogService struct {
	file        *os.File
	mutex       sync.Mutex
	errorMutex  sync.Mutex
	bufferLimit int
	sessionDir  string   // anchored once at OpenSessionLog time; used by WriteToErrorLog
	preSession  []string // timestamped lines written before a session log file exists; flushed by OpenSessionLog
	recent      []string // the latest recentLogLines lines written, for Copy diagnostics; see Recent
}

// defaultLogBufferLimit is the number of log lines kept in the UI by default.
// It is the integer form of the defaultLogLimit preference string.
const defaultLogBufferLimit = 200

// NewLogService returns a LogService with the default buffer limit.
func NewLogService() *LogService {
	return &LogService{bufferLimit: defaultLogBufferLimit}
}

// ── Session log ──────────────────────────────────────────────────────────────

// SessionLogPath returns the path for today's session log file inside dir.
func SessionLogPath(dir string) string {
	return filepath.Join(dir, fmt.Sprintf("GoVid_log_%s.txt", time.Now().Format("2006-01-02")))
}

// ErrorLogPath returns the path for today's error log file inside dir.
func ErrorLogPath(dir string) string {
	return filepath.Join(dir, fmt.Sprintf("GoVid_errors_%s.txt", time.Now().Format("2006-01-02")))
}

// OpenSessionLog opens (or creates) the daily session log in dir, appending to
// any existing content, then flushes any lines buffered before this session
// started (e.g. startup dependency checks, GPU diagnostics) so they aren't
// lost just because logging wasn't enabled yet when they were printed.
// Returns the resolved path on success.
func (svc *LogService) OpenSessionLog(dir string) (string, error) {
	path := SessionLogPath(dir)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	svc.mutex.Lock()
	svc.file = file
	svc.sessionDir = dir
	for _, line := range svc.preSession {
		fmt.Fprintln(file, line)
	}
	svc.preSession = nil
	svc.mutex.Unlock()
	return path, nil
}

// CloseSessionLog writes a closing marker and closes the session log file.
// It is a no-op when no session log is currently open.
func (svc *LogService) CloseSessionLog() {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	if svc.file != nil {
		fmt.Fprintf(svc.file, "[%s] [SYSTEM] Log file closed.\n", time.Now().Format("15:04:05"))
		svc.file.Close()
		svc.file = nil
		svc.sessionDir = ""
	}
}

// WriteToFile appends a timestamped line to the open session log. If no
// session log is open yet, the formatted line is buffered instead (capped to
// BufferLimit) and flushed by the next OpenSessionLog call, so nothing
// printed before logging is enabled gets silently discarded.
func (svc *LogService) WriteToFile(line string) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	formatted := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
	svc.recent = append(svc.recent, formatted)
	if len(svc.recent) > recentLogLines {
		svc.recent = svc.recent[len(svc.recent)-recentLogLines:]
	}
	if svc.file != nil {
		fmt.Fprintln(svc.file, formatted)
		return
	}
	svc.preSession = append(svc.preSession, formatted)
	if len(svc.preSession) > svc.bufferLimit {
		svc.preSession = svc.preSession[len(svc.preSession)-svc.bufferLimit:]
	}
}

// WriteToErrorLog appends a timestamped line to the daily error log. It uses
// the session directory cached at OpenSessionLog time. If no session is active,
// it falls back to the directory containing the executable.
func (svc *LogService) WriteToErrorLog(line string) {
	svc.mutex.Lock()
	dir := svc.sessionDir
	svc.mutex.Unlock()

	if dir == "" {
		if exePath, err := os.Executable(); err == nil {
			dir = filepath.Dir(exePath)
		}
	}
	if dir == "" {
		dir = "."
	}

	svc.errorMutex.Lock()
	defer svc.errorMutex.Unlock()
	path := ErrorLogPath(dir)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "[%s] %s\n", time.Now().Format("15:04:05"), line)
}

// ── Buffer-limit management ──────────────────────────────────────────────────

// SetBufferLimit updates the cached UI line cap.
func (svc *LogService) SetBufferLimit(limit int) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	svc.bufferLimit = limit
}

// BufferLimit returns the current UI line cap.
func (svc *LogService) BufferLimit() int {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	return svc.bufferLimit
}

// ── Session configuration logging ────────────────────────────────────────────

// SessionConfig is a plain-value snapshot of the settings a download session
// starts with. It has no widget references, so it can be built once from the
// UI and passed anywhere (WriteSessionConfig, future structured logging, etc.).
type SessionConfig struct {
	URLs        []string
	RawURLField string
	SavePath    string
	BatchMode   bool
	Format      string
	Quality     string
	TrimStart   string
	TrimEnd     string
	MaxSpeed    string
	Cookies     string // where cookies come from, as cookieLabel names it; never a path
	JSRuntime   string // the JavaScript runtime yt-dlp uses, e.g. "deno 2.9.7 (bin/)"

	SaveLog            bool
	Notify             bool
	AutoRetry          bool
	PostProcessEnabled bool

	SavePrefs bool
	LogLimit  string
	ShowDebug bool

	EmbedMetadata  bool
	EmbedThumbnail bool
	EmbedChapters  bool
	Subtitles      string
	SubtitleLangs  string
	AutoSubtitles  bool
	ThemeMode      string

	PP PostProcessSettings
}

// WriteSessionConfig writes the session's starting configuration to the log
// via writeFn, one line per setting, so a support request can be diagnosed
// from the log file alone.
func (svc *LogService) WriteSessionConfig(cfg SessionConfig, writeFn func(string, color.Color)) {
	maxSpeed := cfg.MaxSpeed
	if maxSpeed == "" {
		maxSpeed = "(none)"
	}
	rawURLField := cfg.RawURLField
	if strings.TrimSpace(rawURLField) == "" {
		rawURLField = "(empty)"
	}

	writeFn("[SYSTEM] ===== Session Configuration =====", colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Save path: %s", cfg.SavePath), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Mode: batch=%t, url_count=%d", cfg.BatchMode, len(cfg.URLs)), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Format/quality: %s / %s", cfg.Format, cfg.Quality), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Trim: start=%q, end=%q", cfg.TrimStart, cfg.TrimEnd), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Max speed: %s", maxSpeed), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Cookies: %s", cmp.Or(cfg.Cookies, "none")), colSystem)
	if cfg.JSRuntime != "" {
		writeFn(fmt.Sprintf("[SYSTEM] JS runtime: %s", cfg.JSRuntime), colSystem)
	}
	writeFn(fmt.Sprintf("[SYSTEM] Runtime toggles: saveLog=%t, notify=%t, autoRetry=%t, postProcess=%t", cfg.SaveLog, cfg.Notify, cfg.AutoRetry, cfg.PostProcessEnabled), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Preferences: savePrefs=%t, logLimit=%s, showDebug=%t, theme=%s", cfg.SavePrefs, cfg.LogLimit, cfg.ShowDebug, cfg.ThemeMode), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Embed: metadata=%t, thumbnail=%t, chapters=%t", cfg.EmbedMetadata, cfg.EmbedThumbnail, cfg.EmbedChapters), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Subtitles: mode=%s, langs=%q, auto=%t", cfg.Subtitles, cfg.SubtitleLangs, cfg.AutoSubtitles), colSystem)

	writeFn(fmt.Sprintf("[SYSTEM] URL field (raw): %q", rawURLField), colSystem)
	for i, url := range cfg.URLs {
		writeFn(fmt.Sprintf("[SYSTEM] URL[%d]: %s", i+1, url), colSystem)
	}

	pp := cfg.PP
	writeFn(fmt.Sprintf("[SYSTEM] Post-process toggles: smoothMotion=%t, sharpen=%t, normalizeAudio=%t, vividMode=%t, denoise=%t, hdrToSdr=%t, deband=%t, autoCrop=%t, stabilize=%t, deinterlace=%t, nightMode=%t, upscaleVideo=%t", pp.SmoothMotion, pp.Sharpen, pp.NormalizeAudio, pp.VividMode, pp.Denoise, pp.HDRToSDR, pp.Deband, pp.AutoCrop, pp.Stabilize, pp.Deinterlace, pp.NightMode, pp.UpscaleVideo), colSystem)
	writeFn(fmt.Sprintf("[SYSTEM] Post-process values: smoothMotionMode=%s, smoothFPS=%.0f, sharpenAmount=%.1f, denoiseMode=%s, upscaleTarget=%s", pp.SmoothMotionMode, pp.SmoothMotionFPS, pp.SharpenAmount, pp.DenoiseMode, pp.UpscaleTarget), colSystem)
	writeFn("[SYSTEM] =================================", colSystem)
}

// ── Package-level helpers ────────────────────────────────────────────────────

// IsErrorLine returns true when the line contains "ERROR" or "FAILED" (case-insensitive).
// yt-dlp's own [debug] diagnostics (e.g. "error cp1252 (No ANSI)") are excluded
// up front so they aren't misclassified as real errors.
func IsErrorLine(line string) bool {
	if strings.Contains(line, "[debug]") {
		return false
	}
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAILED")
}

// IsDebugLine reports whether line is one of yt-dlp's --verbose [debug]
// diagnostics, which are kept out of the log view by default.
func IsDebugLine(line string) bool {
	return strings.HasPrefix(line, "[debug]")
}

// ParseBufferLimit converts a log-limit preference string (e.g. "200",
// "Unlimited") to an integer. Returns defaultLogBufferLimit for any
// unrecognised value.
func ParseBufferLimit(s string) int {
	if s == logLimitUnlimited {
		return math.MaxInt32
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultLogBufferLimit
	}
	return n
}

// recentLogLines is how many of the latest lines LogService keeps for
// Recent, whether or not a session log is open.
const recentLogLines = 200

// Recent returns up to the latest n lines written with WriteToFile, oldest
// first, with their timestamps.
func (svc *LogService) Recent(n int) []string {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	start := max(len(svc.recent)-n, 0)
	return slices.Clone(svc.recent[start:])
}
