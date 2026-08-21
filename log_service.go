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
	"fmt"
	"math"
	"os"
	"path/filepath"
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
}

// NewLogService returns a LogService with the default buffer limit (200 lines).
func NewLogService() *LogService {
	return &LogService{bufferLimit: 200}
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	svc.mutex.Lock()
	svc.file = f
	svc.sessionDir = dir
	for _, line := range svc.preSession {
		fmt.Fprintln(f, line)
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

// IsActive returns true if the session log file is currently open.
func (svc *LogService) IsActive() bool {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	return svc.file != nil
}

// WriteToFile appends a timestamped line to the open session log. If no
// session log is open yet, the formatted line is buffered instead (capped to
// BufferLimit) and flushed by the next OpenSessionLog call, so nothing
// printed before logging is enabled gets silently discarded.
func (svc *LogService) WriteToFile(line string) {
	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	formatted := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), line)
}

// ── Buffer-limit management ──────────────────────────────────────────────────

// SetBufferLimit updates the cached UI line cap.
func (svc *LogService) SetBufferLimit(limit int) {
	svc.bufferLimit = limit
}

// BufferLimit returns the current UI line cap.
func (svc *LogService) BufferLimit() int {
	return svc.bufferLimit
}

// ── Package-level helpers ────────────────────────────────────────────────────

// IsErrorLine returns true when the line contains "ERROR" or "FAILED" (case-insensitive).
func IsErrorLine(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAILED")
}

// ParseBufferLimit converts a log-limit preference string (e.g. "200",
// "Unlimited") to an integer. Returns 200 for any unrecognised value.
func ParseBufferLimit(s string) int {
	if s == "Unlimited" {
		return math.MaxInt32
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 200
	}
	return n
}
