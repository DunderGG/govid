package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// memoryCache is an in-memory releaseCache.
type memoryCache map[string]string

func (cache memoryCache) String(key string) string    { return cache[key] }
func (cache memoryCache) SetString(key, value string) { cache[key] = value }

// fakeGitHub serves /repos/<owner>/<repo>/releases/latest with a fixed
// status and body for each repository, and counts the requests it receives.
type fakeGitHub struct {
	server    *httptest.Server
	requests  atomic.Int32
	userAgent atomic.Value
}

// fakeGitHubRelease is the answer fakeGitHub gives for one repository.
type fakeGitHubRelease struct {
	status int
	body   string
}

// newFakeGitHub answers for yt-dlp/yt-dlp only.
func newFakeGitHub(t *testing.T, status int, body string) *fakeGitHub {
	t.Helper()
	return newFakeGitHubRepos(t, map[string]fakeGitHubRelease{"yt-dlp/yt-dlp": {status, body}})
}

// newFakeGitHubRepos answers for each "owner/repo" in repos.
func newFakeGitHubRepos(t *testing.T, repos map[string]fakeGitHubRelease) *fakeGitHub {
	t.Helper()
	gh := &fakeGitHub{}
	gh.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gh.requests.Add(1)
		gh.userAgent.Store(r.Header.Get("User-Agent"))
		for repo, answer := range repos {
			if r.URL.Path == "/repos/"+repo+"/releases/latest" {
				w.WriteHeader(answer.status)
				w.Write([]byte(answer.body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(gh.server.Close)
	return gh
}

// newTestReleaseService returns a ReleaseService talking to gh, with a clock
// the test controls.
func newTestReleaseService(gh *fakeGitHub, now *time.Time) *ReleaseService {
	svc := NewReleaseService(memoryCache{}, "GoVid/test")
	svc.baseURL = gh.server.URL
	svc.now = func() time.Time { return *now }
	return svc
}

const ytDlpReleaseJSON = `{
	"tag_name": "2026.10.01",
	"html_url": "https://github.com/yt-dlp/yt-dlp/releases/tag/2026.10.01",
	"body": "### Changelog\n- Fixes",
	"assets": [{"name": "yt-dlp.exe", "browser_download_url": "https://example.com/yt-dlp.exe"}]
}`

func TestReleaseServiceLatest(t *testing.T) {
	gh := newFakeGitHub(t, http.StatusOK, ytDlpReleaseJSON)
	now := time.Unix(1_800_000_000, 0)
	svc := newTestReleaseService(gh, &now)

	release, err := svc.Latest(context.Background(), "yt-dlp", "yt-dlp", releaseCheckInterval)

	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if release.TagName != "2026.10.01" || release.HTMLURL != "https://github.com/yt-dlp/yt-dlp/releases/tag/2026.10.01" || release.Body != "### Changelog\n- Fixes" {
		t.Errorf("Latest() = %+v", release)
	}
	if len(release.Assets) != 1 || release.Assets[0].Name != "yt-dlp.exe" || release.Assets[0].DownloadURL != "https://example.com/yt-dlp.exe" {
		t.Errorf("Assets = %+v", release.Assets)
	}
	if ua := gh.userAgent.Load(); ua != "GoVid/test" {
		t.Errorf("User-Agent = %v, want GoVid/test", ua)
	}
}

func TestReleaseServiceChecksAtMostOncePerInterval(t *testing.T) {
	gh := newFakeGitHub(t, http.StatusOK, ytDlpReleaseJSON)
	now := time.Unix(1_800_000_000, 0)
	svc := newTestReleaseService(gh, &now)
	ctx := context.Background()

	for range 3 {
		if _, err := svc.Latest(ctx, "yt-dlp", "yt-dlp", releaseCheckInterval); err != nil {
			t.Fatalf("Latest() error = %v", err)
		}
	}
	if n := gh.requests.Load(); n != 1 {
		t.Errorf("requests within a day = %d, want 1 (cached)", n)
	}

	now = now.Add(releaseCheckInterval + time.Minute)
	if _, err := svc.Latest(ctx, "yt-dlp", "yt-dlp", releaseCheckInterval); err != nil {
		t.Fatal(err)
	}
	if n := gh.requests.Load(); n != 2 {
		t.Errorf("requests after the interval = %d, want 2", n)
	}

	if _, err := svc.Latest(ctx, "yt-dlp", "yt-dlp", 0); err != nil {
		t.Fatal(err)
	}
	if n := gh.requests.Load(); n != 3 {
		t.Errorf("requests with maxAge 0 = %d, want 3 (always asks)", n)
	}
}

func TestReleaseServiceRateLimitIsUnknown(t *testing.T) {
	gh := newFakeGitHub(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
	now := time.Unix(1_800_000_000, 0)
	svc := newTestReleaseService(gh, &now)

	for range 2 {
		_, err := svc.Latest(context.Background(), "yt-dlp", "yt-dlp", releaseCheckInterval)
		if !errors.Is(err, errReleaseUnknown) {
			t.Fatalf("Latest() error = %v, want errReleaseUnknown", err)
		}
	}
	if n := gh.requests.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (a rate-limited answer is cached too)", n)
	}
}

func TestReleaseServiceServerErrorIsNotCached(t *testing.T) {
	gh := newFakeGitHub(t, http.StatusInternalServerError, "")
	now := time.Unix(1_800_000_000, 0)
	svc := newTestReleaseService(gh, &now)

	for range 2 {
		_, err := svc.Latest(context.Background(), "yt-dlp", "yt-dlp", releaseCheckInterval)
		if err == nil || errors.Is(err, errReleaseUnknown) {
			t.Fatalf("Latest() error = %v, want a server error", err)
		}
	}
	if n := gh.requests.Load(); n != 2 {
		t.Errorf("requests = %d, want 2 (failures are retried)", n)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2025.09.26", "2025.09.26", 0},
		{"2025.09.26", "2025.10.01", -1},
		{"2026.03.17", "2025.12.31", 1},
		{"2025.9.3", "2025.09.03", 0},
		{"2025.09.26", "2025.09.26.232302", -1}, // a nightly of the same day is newer
		{"stable@2025.09.26", "2025.09.26", 0},
		{"  2025.09.26\n", "2025.09.26", 0},
		{"v1.2.0", "1.10.0", -1},
		{"1.1.0", "v1.1.0", 0},
		{"2026.09.17", "2026.10.06", -1},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
