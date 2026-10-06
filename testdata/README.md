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
| `ffmpeg_progress.log` | `postprocess_test.go` | FFmpeg stderr with carriage-return-separated progress updates, as split by `scanCRLF`. |
| `hdr_pq_sample.mkv` | Manual check only | A 2-second, 320×180 HEVC Main 10 clip tagged as HDR10 (BT.2020 + PQ), 16 KiB. Made from `testsrc2` with `ffmpeg -f lavfi -i testsrc2=size=320x180:rate=24:duration=2 -vf "zscale=tin=bt709:min=bt709:pin=bt709:t=smpte2084:m=bt2020nc:p=bt2020:npl=100,format=yuv420p10le" -c:v libx265 -crf 28 -x265-params colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc -color_primaries bt2020 -color_trc smpte2084 -colorspace bt2020nc hdr_pq_sample.mkv`. Post-process a copy with **HDR to SDR**: the log should say "HDR10 (PQ) source, tone mapping to BT.709" and the output should look like the original test pattern rather than washed out. |
