// http_fetch.go — Downloading files over HTTP while hashing them.
//
// Responsibilities:
//   - httpFetcher: GET requests with GoVid's User-Agent, for small text
//     files (checksum lists, version files) and for large files, which are
//     written to disk and hashed with SHA-256 as they stream, with progress.
//     SelfUpdater and ToolInstaller share it.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

// httpFetcher downloads files with a fixed client and User-Agent.
type httpFetcher struct {
	client    *http.Client
	userAgent string
}

// get starts a GET request for url and returns the response, which the
// caller closes, after checking its status.
func (fetcher httpFetcher) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fetcher.userAgent)
	resp, err := fetcher.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	return resp, nil
}

// fetchText downloads a small text file.
func (fetcher httpFetcher) fetchText(ctx context.Context, url string) (string, error) {
	resp, err := fetcher.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(data), err
}

// fetchFile downloads url to path and returns the file's SHA-256 in lower
// case hex. onProgress, when set, gets the bytes written so far and the
// total (-1 when the server does not say).
func (fetcher httpFetcher) fetchFile(ctx context.Context, url, path string, onProgress func(done, total int64)) (string, error) {
	resp, err := fetcher.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	progress := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
	if _, err := io.Copy(io.MultiWriter(file, hash, progress), resp.Body); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// progressWriter counts the bytes written through it and reports them.
type progressWriter struct {
	done, total int64
	onProgress  func(done, total int64)
}

func (writer *progressWriter) Write(p []byte) (int, error) {
	writer.done += int64(len(p))
	if writer.onProgress != nil {
		writer.onProgress(writer.done, writer.total)
	}
	return len(p), nil
}
