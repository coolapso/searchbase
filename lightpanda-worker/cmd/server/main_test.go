package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeFetcher struct {
	mu      sync.Mutex
	urls    []string
	content string
	err     error
}

func (f *fakeFetcher) Fetch(_ context.Context, url string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, url)
	return f.content, f.err
}

func testServer(fetch fetcher) http.Handler {
	return (&server{fetcher: fetch, jobs: make(chan struct{}, 2), timeout: time.Second}).routes()
}

func TestExtractIgnoresJSRender(t *testing.T) {
	fetch := &fakeFetcher{content: "# Rendered page"}
	handler := testServer(fetch)
	for _, flag := range []bool{false, true} {
		body := `{"url":"https://example.org/page","js_render":false}`
		if flag {
			body = `{"url":"https://example.org/page","js_render":true}`
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d", response.Code)
		}
		var got extractResponse
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !got.Success || got.Markdown != "# Rendered page" || got.Error != "" {
			t.Fatalf("unexpected response: %+v", got)
		}
	}
	if len(fetch.urls) != 2 || fetch.urls[0] != fetch.urls[1] {
		t.Fatalf("expected both requests to use the browser, got %d calls", len(fetch.urls))
	}
}

func TestExtractRejectsInvalidTargets(t *testing.T) {
	fetch := &fakeFetcher{content: "# Page"}
	for _, body := range []string{
		`{"url":"file:///etc/passwd"}`,
		`{"url":"https://user:secret@example.org/"}`,
		`{"url":"not a URL"}`,
		`{"url":"https://example.org:99999/"}`,
		`{"url":"https://example.org:abc/"}`,
		`{"url":"https://example.org:/"}`,
		`{"url":"https://example..org/"}`,
		`{"url":"https://example.org/hello world"}`,
		`{"url":"https://example.org/\n--log-level=debug"}`,
		`{"url":"https://example.org\\@evil.test/"}`,
		`{"url":"https://[not-ipv6]/"}`,
		`{"url":"https://example.org/"}{"url":"https://example.org/"}`,
	} {
		response := httptest.NewRecorder()
		testServer(fetch).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %q", response.Code, body)
		}
	}
	if len(fetch.urls) != 0 {
		t.Fatal("invalid target reached browser")
	}
}

func TestSanitizeTargetAllowsLocalFixturesAndIPv6(t *testing.T) {
	for _, target := range []string{"http://fixture:8080/path", "http://[::1]:8000/path"} {
		got, err := sanitizeTarget(target)
		if err != nil || got != target {
			t.Fatalf("sanitizeTarget(%q) = %q, %v", target, got, err)
		}
	}
}

func TestExtractPassesSanitizedURL(t *testing.T) {
	fetch := &fakeFetcher{content: "# Page"}
	response := httptest.NewRecorder()
	testServer(fetch).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/extract",
		strings.NewReader(`{"url":"HTTPS://EXAMPLE.ORG/a?q=1#private-fragment"}`)))
	if response.Code != http.StatusOK || len(fetch.urls) != 1 ||
		fetch.urls[0] != "https://example.org/a?q=1" {
		t.Fatalf("unexpected sanitized request: status %d, URLs %q", response.Code, fetch.urls)
	}
}

func TestLightpandaFetcherValidatesBeforeExec(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-lightpanda")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'executed'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fetch := lightpandaFetcher{binary: binary}
	if markdown, err := fetch.Fetch(context.Background(), "https://example.org:99999/"); err == nil || markdown != "" {
		t.Fatalf("invalid URL reached process: markdown %q, error %v", markdown, err)
	}
}

func TestExtractFailureIsPrivate(t *testing.T) {
	fetch := &fakeFetcher{err: errors.New("failed at https://secret.example/path")}
	response := httptest.NewRecorder()
	testServer(fetch).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/extract",
		strings.NewReader(`{"url":"https://secret.example/path"}`)))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret.example") {
		t.Fatalf("unsafe failure response: %d %q", response.Code, response.Body.String())
	}
	var result extractResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Success || result.Markdown != "" || result.Error != "extraction failed" {
		t.Fatalf("unexpected failure response: %+v", result)
	}
}

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	testServer(&fakeFetcher{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestOutputLimit(t *testing.T) {
	buffer := &cappedBuffer{limit: 3}
	if _, err := buffer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("d")); err == nil {
		t.Fatal("expected output limit error")
	}
}

func TestLightpandaProcessUsesMarkdownWithClutter(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-lightpanda")
	script := "#!/bin/sh\nprintf '%s' \"$*\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	markdown, err := (lightpandaFetcher{binary: binary, waitMS: 500}).Fetch(context.Background(), "HTTPS://EXAMPLE.ORG/path#private-fragment")
	if err != nil || !strings.Contains(markdown, "--dump markdown") ||
		!strings.Contains(markdown, "--strip-mode clutter") ||
		!strings.Contains(markdown, "--fail-on-http-error") ||
		!strings.Contains(markdown, "https://example.org/path") ||
		strings.Contains(markdown, "private-fragment") {
		t.Fatalf("markdown = %q, error = %v", markdown, err)
	}
}
