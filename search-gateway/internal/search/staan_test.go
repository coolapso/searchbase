package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coolapso/searchbase/search-gateway/internal/settings"
)

func TestStaanProviderSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/search/web" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request method, path or auth")
		}
		q := r.URL.Query()
		if q.Get("q") != "private query" || q.Get("market") != "en-us" || q.Get("offset") != "20" || len(q) != 3 {
			t.Errorf("unexpected parameters: %v", q)
		}
		_, _ = w.Write([]byte(`{"search_id":"ignored","web":{"results":[{"title":"Title","url":"https://example.com","snippet":"Snippet","hostname":"ignored"},{"title":"Second"}]}}`))
	}))
	defer server.Close()
	p := NewStaanProvider("secret")
	p.address = server.URL
	results, err := p.Search(context.Background(), Request{Query: "private query", Region: "us-en", Page: 3, Limit: 1, TimeLimit: "d", SafeSearch: "off", Engine: "google"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%v err=%v", results, err)
	}
	if results[0] != (Result{Title: "Title", URL: "https://example.com", Snippet: "Snippet"}) {
		t.Fatalf("unexpected result: %v", results[0])
	}
}

func TestStaanRequestMapping(t *testing.T) {
	for _, tt := range []struct {
		region, market string
		page           int
		offset         string
	}{
		{"", "en-us", 0, ""}, {"wt-wt", "en-us", -1, ""}, {"gb-en", "en-us", 1, ""},
		{"US-EN", "en-us", 2, "10"}, {"fr-fr", "fr-fr", 4, "30"}, {"de-de", "de-de", 999, "30"},
	} {
		req, err := NewStaanProvider("secret").newRequest(context.Background(), Request{Query: "é & test", Region: tt.region, Page: tt.page})
		if err != nil {
			t.Fatal(err)
		}
		q := req.URL.Query()
		if q.Get("market") != tt.market || q.Get("offset") != tt.offset || q.Get("q") != "é & test" {
			t.Fatalf("mapping %+v: %v", tt, q)
		}
	}
}

func TestStaanResponses(t *testing.T) {
	for _, tt := range []struct {
		name, body, wantErr  string
		status, limit, count int
	}{
		{"default", `{"web":{"results":[{},{}]}}`, "", 200, 0, 2},
		{"negative limit", `{"web":{"results":[{},{}]}}`, "", 200, -1, 2},
		{"empty", `{"web":{"results":[]}}`, "", 200, 0, 0},
		{"unauthorized", "secret private query", "staan search provider returned status 401", 401, 0, 0},
		{"rate limited", "secret private query", "staan search provider returned status 429", 429, 0, 0},
		{"invalid JSON", "secret private query", "staan search provider returned an invalid response", 200, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			p := NewStaanProvider("secret")
			p.address = server.URL
			results, err := p.Search(context.Background(), Request{Query: "private query", Limit: tt.limit})
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil || len(results) != tt.count || results == nil {
				t.Fatalf("results=%v err=%v", results, err)
			}
		})
	}
}

func TestStaanCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewStaanProvider("secret").Search(ctx, Request{Query: "private query"})
	if err == nil || err.Error() != "staan search provider request failed" || strings.Contains(err.Error(), "private query") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStaanConfiguredProvider(t *testing.T) {
	t.Setenv("SEARCHBASE_SEARCH_PROVIDER", "staan")
	t.Setenv("SEARCHBASE_STAAN_API_KEY", "secret")
	s, err := settings.NewSettings()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(s.SearchProvider())
	if err != nil {
		t.Fatal(err)
	}
	p, ok := provider.(*StaanProvider)
	if !ok || p.apiKey != "secret" {
		t.Fatal("Staan provider not configured")
	}
}
