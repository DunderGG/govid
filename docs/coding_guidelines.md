# Go Coding Guidelines

Guidelines are grouped by concern. Each rule is short by design — it states *what* to do and *why* it matters. Code examples are included where the rule is easiest to misapply.

## Index
- [1. Coding](#1-coding)
   - [1.1 One operation per line](#11-one-operation-per-line)
   - [1.2 Naming](#12-naming)
   - [1.3 Error handling](#13-error-handling)
   - [1.4 Functions do one thing](#14-functions-do-one-thing)
   - [1.5 Return early, not deeply nested](#15-return-early-not-deeply-nested)
   - [1.6 Use defer for cleanup](#16-use-defer-for-cleanup)
   - [1.7 Comments explain why, not what](#17-comments-explain-why-not-what)
   - [1.8 Let gofmt own formatting](#18-let-gofmt-own-formatting)
   - [1.9 Import grouping](#19-import-grouping)
- [2. Concurrency](#2-concurrency)
   - [2.1 Share memory by communicating (when practical)](#21-share-memory-by-communicating-when-practical)
   - [2.2 Keep long work off the UI thread](#22-keep-long-work-off-the-ui-thread)
   - [2.3 Fyne widgets must be mutated on the UI thread](#23-fyne-widgets-must-be-mutated-on-the-ui-thread)
   - [2.4 Use context.Context for cancellation](#24-use-contextcontext-for-cancellation)
   - [2.5 Protect shared state with the right tool](#25-protect-shared-state-with-the-right-tool)
- [3. Architectural](#3-architectural)
   - [3.1 Separation of concerns — one responsibility per file](#31-separation-of-concerns--one-responsibility-per-file)
   - [3.2 Use callback structs to cross layer boundaries](#32-use-callback-structs-to-cross-layer-boundaries)
   - [3.3 Prefer plain value structs as data transfer objects](#33-prefer-plain-value-structs-as-data-transfer-objects)
   - [3.4 Accept interfaces; return concrete types by default](#34-accept-interfaces-return-concrete-types-by-default)
- [4. Testing](#4-testing)
   - [4.1 Prefer table-driven tests for pure logic](#41-prefer-table-driven-tests-for-pure-logic)
   - [4.2 Keep tests deterministic](#42-keep-tests-deterministic)
   - [4.3 Test behaviour, not implementation details](#43-test-behaviour-not-implementation-details)
- [5. Sources](#5-sources)

---

## 1. Coding
> Line-level and function-level rules that apply to every file.

Use judgment when rules conflict. Prefer readability, correctness, and maintainability over mechanical rule-following.

### 1.1 One operation per line
Break compound expressions into separate lines so each step is visible and debuggable individually.

```go
// ✗ hard to read, hard to set a breakpoint on
totalFrames := computeOutputFrameCount(ctx, engine.FFprobePath, inputPath, probeFrameCount(ctx, engine.FFprobePath, inputPath), activeVF)

// ✓ each step is named and readable
inputFrames := probeFrameCount(ctx, engine.FFprobePath, inputPath)
totalFrames := computeOutputFrameCount(ctx, engine.FFprobePath, inputPath, inputFrames, activeVF)
```

---

### 1.2 Naming
Go favours short, precise names. The rule of thumb is: the larger the scope, the more descriptive the name.

- **Short-lived variables** in a small scope use short names: `i`, `n`, `err`, `ok`, `ctx`, `p`, `f`.
- **Package-level variables and exported fields** use full descriptive names: `logBufferLimit`, `FFmpegPath`.
- **Acronyms** keep all their letters the same case. An exported identifier uses the full uppercase form (`URL`, `HTTPClient`); an unexported identifier uses the full lowercase form (`urlStr`, `httpClient`). The mixed form is never correct: never `Url`, `Http`, `Id`.
- **Getter methods** have no `Get` prefix. A method that returns a name is `Name()`, not `GetName()`.
- **Boolean variables** read as a true/false assertion: `isRunning`, `hadTransientErr`, `ok` — never `runningFlag` or `bRunning`.

```go
// ✗ mixed-case acronyms and verbose getter naming
type DownloadConfig struct {
    Url string
}

func (c DownloadConfig) GetUrl() string { return c.Url }

// ✓ consistent acronym casing and idiomatic method names
type DownloadConfig struct {
    SourceURL string
}

func (c DownloadConfig) URL() string { return c.SourceURL }

// ✓ boolean names read like assertions
isRunning := true
hadTransientErr := false
```

---

### 1.3 Error handling
Go has no exceptions. Errors are plain values returned as the last return argument. Always check them.

```go
// ✗ silently ignores a failure
data, _ := os.ReadFile("govid.json")

// ✓ handle or propagate
data, err := os.ReadFile("govid.json")
if err != nil {
    return nil, fmt.Errorf("loading config: %w", err)
}
```

- **Return early on error.** Avoid deeply nested `if-else` trees; the happy path should stay at the left margin.
- **Add context when wrapping.** Use `fmt.Errorf("doing X: %w", err)` so callers see a useful message chain.
- **Don't both log and return an error.** Pick one. Logging hides the error from callers; returning lets the caller decide.
- **Keep error strings simple.** Error messages should start lowercase and avoid trailing punctuation unless they include proper nouns or structured data.
- **Prefer `errors.Is`/`errors.As` for branching.** Compare error semantics, not string text.

---

### 1.4 Functions do one thing
A function that validates input, builds a command, starts a process, parses its output, and renames files is four functions, not one. If you cannot describe a function in a single sentence without the word "and", split it.

Use roughly 60 lines as a smell threshold, not a hard limit. Small, cohesive functions are preferred, but a slightly longer function is acceptable when splitting would hurt readability.

```go
// ✗ one function owns multiple responsibilities
func processVideo(ctx context.Context, in, out string) error {
    if err := validateInput(in, out); err != nil {
        return err
    }
    cmd := buildFFmpegCommand(in, out)
    raw, err := runCommand(ctx, cmd)
    if err != nil {
        return err
    }
    if err := parseAndLogProgress(raw); err != nil {
        return err
    }
    return renameOutput(out)
}

// ✓ orchestration delegates each concern to a helper
func processVideo(ctx context.Context, in, out string) error {
    if err := validateInput(in, out); err != nil {
        return err
    }
    if err := transcode(ctx, in, out); err != nil {
        return err
    }
    return finalizeOutput(out)
}
```

---

### 1.5 Return early, not deeply nested
Validate inputs and error conditions at the top of a function. The main logic should be as flat as possible.

```go
// ✗ logic buried inside nested if blocks
func process(path string) error {
    if path != "" {
        if info, err := os.Stat(path); err == nil {
            if info.IsDir() {
                // ... actual work here
            }
        }
    }
    return nil
}

// ✓ guard clauses exit early, main logic stays flat
func process(path string) error {
    if path == "" {
        return errors.New("path is required")
    }
    info, err := os.Stat(path)
    if err != nil {
        return fmt.Errorf("stat %q: %w", path, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("%q is not a directory", path)
    }
    // ... actual work here
    return nil
}
```

---

### 1.6 Use `defer` for cleanup
Call `defer` immediately after acquiring a resource so the release is always paired with the acquire, even if the function returns early due to an error.

```go
f, err := os.Open(path)
if err != nil {
    return err
}
defer f.Close() // ← paired right here, never forgotten

// ... use f
```

---

### 1.7 Comments explain *why*, not *what*
The code already says *what* it does. Comments should explain *why* a choice was made, clarify a non-obvious invariant, or describe a side-effect the reader might not expect.

```go
// ✗ restates the code
// Set i to 0
i := 0

// ✓ explains the reason
// Start from 1 — index 0 is the sentinel header row and must not be processed.
for i := 1; i < len(rows); i++ {
```

**Doc comments** on exported types and functions are the exception: they *do* describe what the thing is, because `go doc` and IDE hover text surface them. Every exported symbol must have one, starting with the symbol's name.

```go
// PPEngine holds the resolved paths to the FFmpeg tools it drives.
// Construct one with NewPPEngine and call ApplyFilters to post-process files.
type PPEngine struct { … }
```

---

### 1.8 Let `gofmt` own formatting
Do not hand-format code. Run `gofmt` (or rely on `go fmt`/editor-on-save) and accept its output.

```go
// ✗ manually aligned formatting that drifts over time
if    err!=nil{ return err }

// ✓ gofmt output is the team standard
if err != nil {
    return err
}
```

---

### 1.9 Import grouping
Organise imports into two blocks separated by a blank line: standard library first, then external packages. `goimports` (or `gopls`) does this automatically on save.

```go
import (
    "context"
    "fmt"
    "os"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/widget"
)
```

---

## 2. Concurrency
> Rules for code that touches goroutines, channels, or shared state.

### 2.1 Share memory by communicating (when practical)
Prefer passing work and ownership through channels instead of sharing mutable state across goroutines. Use mutexes/atomics when they are the simpler and clearer tool.

```go
// ✓ ownership transfer through a channel
jobs := make(chan Job)
go func() {
    for job := range jobs {
        process(job)
    }
}()
jobs <- nextJob
```

---

### 2.2 Keep long work off the UI thread
Any operation that can block (file I/O, network calls, subprocesses, heavy parsing) should run in a background goroutine. Keep the UI thread focused on rendering and event handling.

```go
go func() {
    // long-running work
    result, err := runJob(ctx)

    fyne.Do(func() {
        if err != nil {
            app.ui.status.SetText("Failed")
            return
        }
        app.ui.status.SetText(result)
    })
}()
```

---

### 2.3 Fyne widgets must be mutated on the UI thread
Fyne panics when a widget is read or written from a goroutine other than the one that created it. Wrap every widget mutation in `fyne.Do` when calling from a background goroutine.

```go
// ✗ data race — called from a goroutine
app.ui.status.SetText("Done")

// ✓ deferred to the UI event loop
fyne.Do(func() {
    app.ui.status.SetText("Done")
})
```

This rule applies to `SetText`, `SetValue`, `SetChecked`, `Refresh`, `Add`, `Objects = …`, and any other method that modifies widget state.

---

### 2.4 Use `context.Context` for cancellation
Pass a `context.Context` as the first argument to any function that starts I/O, runs a subprocess, or may take a long time. Check `ctx.Err()` in loops. Never use a global boolean flag as a cancellation signal.

```go
// ✗ global flag — not composable, racy
var shouldStop bool

// ✓ context — composable, safe, standard
func runJob(ctx context.Context, job PostProcessJob, cb PPCallbacks) {
    cmd := exec.CommandContext(ctx, engine.FFmpegPath, job.ffmpegArgs...)
    …
}
```

---

### 2.5 Protect shared state with the right tool
- Use `sync.Mutex` or `sync.RWMutex` for structs accessed from multiple goroutines.
- Use `sync/atomic` types (`atomic.Int32`, `atomic.Bool`) for single scalar values that only need atomic read/write — they are faster and simpler than a mutex for that case.
- Use channels when goroutines need to hand off work or signal completion.

In GoVid: `ppFailed` uses `atomic.Int32` (simple counter), `isRunning` uses `atomic.Bool` (single flag), and `LogManager` uses a `sync.Mutex` (protects multi-field struct writes).

```go
// ✗ unsynchronised shared state
type Stats struct {
    completed int
}

// ✓ mutex for multi-field or compound updates
type Stats struct {
    mu        sync.Mutex
    completed int
}

func (s *Stats) Inc() {
    s.mu.Lock()
    s.completed++
    s.mu.Unlock()
}

// ✓ atomic for a single scalar flag
var isRunning atomic.Bool
isRunning.Store(true)
```

---

## 3. Architectural
> Design and structure rules for how components interact.

### 3.1 Separation of concerns — one responsibility per file
Each file should have a single clear responsibility, stated in its package-level doc comment. If a file is doing two unrelated things, one of them belongs elsewhere.

| File | Responsibility |
|---|---|
| `download_engine.go` | yt-dlp arg building and process execution |
| `pp_engine.go` | FFmpeg post-processing worker pool |
| `preference_service.go` | Preference key constants, defaults, Load/Save/Reset |
| `helpers.go` | Thread-safe UI updates and preference translation |

---

### 3.2 Use callback structs to cross layer boundaries
When a lower-level component (engine, service) needs to report events to a higher layer (UI), pass a callback struct rather than a direct reference to `DownloaderApp` or any Fyne type. This keeps the engine free of UI imports and makes it independently testable.

```go
// ✓ PPEngine knows nothing about Fyne
type PPCallbacks struct {
    OnLog     func(line string, col color.Color)
    OnStatus  func(msg string)
    OnFailure func()
}
```

The caller (e.g. `DownloaderApp`) fills the struct with closures that call its own UI helpers. The engine never imports `fyne.io`.

---

### 3.3 Prefer plain value structs as data transfer objects
When passing a bundle of settings between layers, use a plain struct with no methods or widget references. This makes the data easy to inspect, copy, and test.

`AppPreferences` is the canonical example: it is populated from widget state by `savePreferences()`, passed to `PreferenceService.Save()`, and read back by `applyPreferencesToWidgets()` — all without any Fyne import inside the service itself.

---

### 3.4 Accept interfaces; return concrete types by default
Functions that accept an interface are easier to test (you can pass a mock). Returning a concrete type gives callers full access to the API without assertions.

Return an interface when abstraction is intentional: for example, when the concrete type is an internal detail and callers should depend only on behaviour.

```go
// ✓ accepts the fyne.Preferences interface — easy to stub in tests
func NewPreferenceService(store fyne.Preferences) *PreferenceService { … }

// ✓ return an interface when concrete implementation is intentionally hidden
func NewHash() hash.Hash32 { … }
```

---

## 4. Testing
> Rules for making behaviour safe to change.

### 4.1 Prefer table-driven tests for pure logic
When a function has multiple input/output cases, use table-driven tests so each case is explicit and easy to extend.

```go
func TestNormalizeExtension(t *testing.T) {
    cases := []struct {
        in   string
        want string
    }{
        {in: "mp4", want: ".mp4"},
        {in: ".mkv", want: ".mkv"},
        {in: "", want: ""},
    }

    for _, tc := range cases {
        got := normalizeExtension(tc.in)
        if got != tc.want {
            t.Fatalf("normalizeExtension(%q) = %q, want %q", tc.in, got, tc.want)
        }
    }
}
```

### 4.2 Keep tests deterministic
Avoid time-dependent and environment-dependent assertions where possible. Inject clocks, temp directories, and command runners at boundaries so tests remain stable.

```go
// ✗ non-deterministic: depends on wall clock
if time.Now().After(deadline) { ... }

// ✓ deterministic: pass a clock function in tests
func isExpired(now func() time.Time, deadline time.Time) bool {
    return now().After(deadline)
}
```

### 4.3 Test behaviour, not implementation details
Assert observable outcomes (returned values, state changes, emitted callbacks), not internal helper call order.

```go
// ✗ brittle: fails when internals are refactored
// mockRunner.AssertCalled(t, "buildArgs", ...)

// ✓ robust: asserts externally visible behaviour
err := processVideo(ctx, in, out)
if err != nil {
    t.Fatalf("processVideo returned error: %v", err)
}
if _, statErr := os.Stat(out); statErr != nil {
    t.Fatalf("expected output file to exist: %v", statErr)
}
```

---

## 5. Sources
- [Effective Go](https://go.dev/doc/effective_go) — the canonical Go style reference
- [Google Go Style Guide](https://google.github.io/styleguide/go/) — more detailed rules; the sections on *Decisions* and *Best Practices* are the most useful
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) — concise list of common review findings

