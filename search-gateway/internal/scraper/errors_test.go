package scraper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerFailureAllowlist(t *testing.T) {
	for _, tc := range []struct {
		input, category string
		status          int
	}{
		{"not_found", "not_found", 404}, {"forbidden", "forbidden", 403},
		{"robots_denied", "robots_denied", 403}, {"timeout", "timeout", 504},
		{"rate_limited", "rate_limited", 429}, {"unreachable", "unreachable", 502},
		{"upstream_error", "upstream_error", 502}, {"failed at https://secret.example", "extraction_failed", 500},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ExtractResponse{Error: tc.input})
		}))
		content, err := NewScraperClient(server.URL).Extract(context.Background(), "https://secret.example", false)
		server.Close()
		category, status := FetchFailure(err)
		if err == nil || content != "" || category != tc.category || status != tc.status || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %q %v %s %d", tc.input, content, err, category, status)
		}
	}
}
