// release_service.go — Latest-release lookups on GitHub, for update checks.
//
// Responsibilities:
//   - ReleaseService: fetches the latest release of a GitHub repository,
//     caching the answer (with the time it was fetched) in the preferences
//     store so each repository is checked at most once per day.
//   - Release / ReleaseAsset: the fields of a GitHub release GoVid uses.
//   - compareVersions / isOlderVersion: version ordering for yt-dlp's date
//     versions (2025.09.26) and GoVid's own release tags.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// releaseCheckInterval is how long a cached latest-release answer is reused
// before GitHub is asked again.
const releaseCheckInterval = 24 * time.Hour

// releaseRequestTimeout bounds each request to the GitHub API.
const releaseRequestTimeout = 5 * time.Second

// githubAPIURL is the GitHub REST API root.
const githubAPIURL = "https://api.github.com"

// errReleaseUnknown is returned when GitHub does not say which release is
// the latest, typically because the unauthenticated rate limit (HTTP 403) is
// used up. It means "try again later", not that anything is broken.
var errReleaseUnknown = errors.New("latest release unknown")

// Release is the part of a GitHub release GoVid uses.
type Release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"` // the release's page on github.com
	Body    string         `json:"body"`     // release notes, in Markdown
	Assets  []ReleaseAsset `json:"assets"`
}

// ReleaseAsset is one file attached to a release.
type ReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

// releaseCache is the part of fyne.Preferences ReleaseService uses to keep
// its answers between runs.
type releaseCache interface {
	String(key string) string
	SetString(key, value string)
}

// cachedRelease is a cache entry. An empty Release.TagName records that the
// latest release was unknown when checked.
type cachedRelease struct {
	CheckedAt time.Time `json:"checkedAt"`
	Release   Release   `json:"release"`
}

// ReleaseService looks up the latest release of GitHub repositories. It has
// no UI dependency.
type ReleaseService struct {
	client    *http.Client
	baseURL   string // githubAPIURL; replaced in tests
	userAgent string // GitHub rejects requests without a User-Agent
	cache     releaseCache
	now       func() time.Time
}

// NewReleaseService returns a ReleaseService that caches its answers in cache
// (pass fyne.CurrentApp().Preferences()) and identifies itself as userAgent.
func NewReleaseService(cache releaseCache, userAgent string) *ReleaseService {
	return &ReleaseService{
		client:    &http.Client{Timeout: releaseRequestTimeout},
		baseURL:   githubAPIURL,
		userAgent: userAgent,
		cache:     cache,
		now:       time.Now,
	}
}

// Latest returns the latest release of owner/repo. A cached answer is used
// when it is younger than maxAge; pass 0 to always ask GitHub. It returns
// errReleaseUnknown when GitHub would not say (for example when rate
// limited), and records that in the cache too, so a rate-limited check is
// not repeated on every start.
func (svc *ReleaseService) Latest(ctx context.Context, owner, repo string, maxAge time.Duration) (Release, error) {
	key := releaseCacheKey(owner, repo)
	if cached, ok := svc.cached(key); ok && svc.now().Sub(cached.CheckedAt) < maxAge {
		if cached.Release.TagName == "" {
			return Release{}, errReleaseUnknown
		}
		return cached.Release, nil
	}

	release, err := svc.fetch(ctx, owner, repo)
	if err != nil && !errors.Is(err, errReleaseUnknown) {
		return Release{}, err
	}
	svc.store(key, cachedRelease{CheckedAt: svc.now(), Release: release})
	return release, err
}

// fetch asks GitHub for the latest release of owner/repo.
func (svc *ReleaseService) fetch(ctx context.Context, owner, repo string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", svc.baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("building release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", svc.userAgent)

	resp, err := svc.client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("checking %s/%s releases: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return Release{}, errReleaseUnknown
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("checking %s/%s releases: %s", owner, repo, resp.Status)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("reading %s/%s release: %w", owner, repo, err)
	}
	if release.TagName == "" {
		return Release{}, fmt.Errorf("%s/%s release has no tag", owner, repo)
	}
	return release, nil
}

// releaseCacheKey is the preferences key a repository's answer is cached under.
func releaseCacheKey(owner, repo string) string {
	return "latestRelease:" + owner + "/" + repo
}

// cached returns the cache entry stored under key, if there is a valid one.
func (svc *ReleaseService) cached(key string) (cachedRelease, bool) {
	raw := svc.cache.String(key)
	if raw == "" {
		return cachedRelease{}, false
	}
	var entry cachedRelease
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return cachedRelease{}, false
	}
	return entry, true
}

// store saves entry under key. A cache that cannot be written only means
// the next check asks GitHub again, so the error is dropped.
func (svc *ReleaseService) store(key string, entry cachedRelease) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	svc.cache.SetString(key, string(raw))
}

// ── Version ordering ─────────────────────────────────────────────────────────

// compareVersions orders two version strings and returns -1, 0, or 1. It
// handles yt-dlp's date versions ("2025.09.26", nightly "2025.09.26.232302")
// and tags such as "v1.2.0" or "2026.09.17". A leading "v" and a channel
// prefix ("stable@") are ignored, and dot- or dash-separated parts are
// compared numerically when both are numbers, as text otherwise. A version
// with extra parts sorts after its prefix ("2025.09.26.1" > "2025.09.26").
func compareVersions(a, b string) int {
	partsA, partsB := versionParts(a), versionParts(b)
	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		if c := compareVersionPart(partsA[i], partsB[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(partsA), len(partsB))
}

// isOlderVersion reports whether installed is older than latest.
func isOlderVersion(installed, latest string) bool {
	return compareVersions(installed, latest) < 0
}

// versionParts normalises a version string and splits it into its parts.
func versionParts(version string) []string {
	version = strings.TrimSpace(version)
	if _, after, found := strings.Cut(version, "@"); found {
		version = after
	}
	version = strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")
	return strings.FieldsFunc(version, func(r rune) bool { return r == '.' || r == '-' })
}

// compareVersionPart orders two version parts.
func compareVersionPart(a, b string) int {
	numA, errA := strconv.Atoi(a)
	numB, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return cmp.Compare(numA, numB)
	}
	return strings.Compare(a, b)
}
