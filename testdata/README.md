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
