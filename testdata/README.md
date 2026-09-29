# Test Fixtures

This directory holds deterministic files consumed by automated tests. Go treats a
directory named `testdata` as test-only data: it is ignored when building packages.

Keep fixtures small, sanitized, and stable. Suitable examples include:

- representative yt-dlp and FFmpeg logs for parser tests;
- preference and configuration JSON files, including invalid or legacy variants;
- media-metadata responses used by download and post-processing tests.

Tests should read fixtures using paths relative to their package (for example,
`testdata/example.log`). Do not place credentials, downloaded user media, or large
binary files here.

## Current fixtures

| File | Used by | Contents |
| --- | --- | --- |
| `ytdlp_stdout.log` | `logscanner_test.go` | yt-dlp stdout for a two-format (WebM video + M4A audio) download with progress lines. |
| `ytdlp_stderr_merge.log` | `logscanner_test.go` | yt-dlp stderr with debug, warning, `[Merger]`, and remux lines. |
| `ytdlp_stderr_transient.log` | `logscanner_test.go` | yt-dlp stderr ending in a retryable HTTP 429 error. |
| `ytdlp_stderr_fatal.log` | `logscanner_test.go` | yt-dlp stderr ending in a permanent "Unsupported URL" error. |
| `history_valid.json` | `history_service_test.go` | A two-entry `download_history.json` in the current schema. |
| `history_corrupted.json` | `history_service_test.go` | A truncated history file that fails to parse. |
