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
	if result.Success || result.Markdown != "" || result.Error != "extraction_failed" {
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
	script := "#!/bin/sh\nprintf '{\"http_status\":200,\"error\":null,\"content\":\"%s\"}' \"$*\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	markdown, err := (lightpandaFetcher{binary: binary, waitMS: 500}).Fetch(context.Background(), "HTTPS://EXAMPLE.ORG/path#private-fragment")
	if err != nil || !strings.Contains(markdown, "--dump markdown") ||
		!strings.Contains(markdown, "--strip-mode clutter") ||
		!strings.Contains(markdown, "--fail-on-http-error") ||
		!strings.Contains(markdown, "--obey-robots") ||
		!strings.Contains(markdown, "--block-private-networks") ||
		!strings.Contains(markdown, "--user-agent "+defaultUserAgent) ||
		!strings.Contains(markdown, "https://example.org/path") ||
		strings.Contains(markdown, "private-fragment") {
		t.Fatalf("markdown = %q, error = %v", markdown, err)
	}
}

func TestUserAgentValidation(t *testing.T) {
	for _, value := range []string{"", "SearchbaseCloud (+https://searchbase.md)", "PersonalBot (+https://example.org/bot)"} {
		got, err := validateUserAgent(value)
		if err != nil || got == "" {
			t.Fatalf("valid identity rejected: %q", value)
		}
	}
	for _, value := range []string{" ", "Bot\r\nInjected: value", "Mozilla/5.0", "mozilla/5.0", strings.Repeat("x", 1025)} {
		if _, err := validateUserAgent(value); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}

func TestLightpandaPrivateNetworkOverrideAndIdentity(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-lightpanda")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '{\"http_status\":200,\"error\":null,\"content\":\"%s\"}' \"$*\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	identity := "PersonalBot (+https://example.org/bot)"
	output, err := (lightpandaFetcher{binary: binary, userAgent: identity, allowPrivateNetworks: true}).Fetch(context.Background(), "http://127.0.0.1/")
	if err != nil || strings.Contains(output, "--block-private-networks") || !strings.Contains(output, "--user-agent "+identity) || !strings.Contains(output, "--obey-robots") {
		t.Fatalf("unexpected args %q: %v", output, err)
	}
}

func TestBrowserResultCategories(t *testing.T) {
	for _, tc := range []struct{ body, category string }{
		{`{"http_status":404,"content":"secret error page"}`, "not_found"},
		{`{"http_status":403}`, "forbidden"}, {`{"http_status":429}`, "rate_limited"},
		{`{"http_status":503}`, "upstream_error"}, {`{"http_status":504}`, "timeout"},
		{`{"error":"OperationTimedout"}`, "timeout"}, {`{"error":"CouldntResolveHost"}`, "unreachable"},
		{`{"error":"CouldntConnect"}`, "unreachable"}, {`{"error":"RobotsBlocked"}`, "robots_denied"},
		{`{"error":"unknown https://private.example"}`, "extraction_failed"},
		{`{"http_status":200,"content":""}`, "extraction_failed"}, {`not JSON`, "extraction_failed"},
	} {
		content, err := decodeBrowserResult([]byte(tc.body), errors.New("process failed"))
		if err == nil || content != "" || failureCategory(err) != tc.category {
			t.Fatalf("%s: content %q, error %v", tc.category, content, err)
		}
	}
	content, err := decodeBrowserResult([]byte(`{"http_status":200,"error":null,"content":"# Article","url":"secret","headers":{"secret":"value"}}`), nil)
	if err != nil || content != "# Article" {
		t.Fatalf("success: %q %v", content, err)
	}
	if _, err := decodeBrowserResult([]byte(`{"http_status":200,"content":"partial"}`), errors.New("killed")); failureCategory(err) != "extraction_failed" {
		t.Fatal("accepted failed process")
	}
}

func TestBrowserDeadlineCategory(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-lightpanda")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	content, err := (lightpandaFetcher{binary: binary}).Fetch(ctx, "https://example.org")
	if content != "" || err == nil || failureCategory(err) != "timeout" {
		t.Fatalf("deadline: %q %v", content, err)
	}
}
