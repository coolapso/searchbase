package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coolapso/searchbase/search-gateway/internal/scraper"
	"github.com/coolapso/searchbase/search-gateway/internal/search"
	"github.com/mark3labs/mcp-go/mcp"
)

// MockSearchProvider implements search.SearchProvider for testing
type MockSearchProvider struct {
	Err error
}

func (m *MockSearchProvider) Search(ctx context.Context, req search.Request) (search.Results, error) {
	if m.Err != nil {
		return nil, m.Err
	}

	return search.Results{
		{
			Title:   "Test Title",
			URL:     "http://example.com/mock",
			Snippet: "Test snippet",
		},
	}, nil
}

func TestHandleWebSearchProviderError(t *testing.T) {
	scraperClient := scraper.NewScraperClient("http://example.com")
	srv := NewServer(&MockSearchProvider{Err: errors.New("ddgs search provider request failed")}, scraperClient, slog.Default())

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "web_search",
			Arguments: map[string]any{
				"query": "secret private query",
			},
		},
	}

	res, err := srv.HandleWebSearch(context.Background(), req)
	if err == nil {
		t.Fatalf("Expected returned provider error, got nil")
	}

	if !res.IsError {
		t.Fatalf("Expected tool error")
	}

	textContent := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(textContent, "Search failed: ddgs search provider request failed") {
		t.Fatalf("Expected provider error in response, got %s", textContent)
	}

	if strings.Contains(textContent, "secret private query") {
		t.Fatalf("Response leaked query: %s", textContent)
	}
}

func TestHandleWebSearch(t *testing.T) {
	// Mock scraper
	mockScraperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/extract" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(scraper.ExtractResponse{
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockScraperServer.Close()

	scraperClient := scraper.NewScraperClient(mockScraperServer.URL)
	logger := slog.Default()
	srv := NewServer(&MockSearchProvider{}, scraperClient, logger)

	// Call HandleWebSearch directly
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "web_search",
			Arguments: map[string]any{
				"query": "test query",
				"limit": 1,
			},
		},
	}

	res, err := srv.HandleWebSearch(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWebSearch returned unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("Expected success, got tool error")
	}

	if len(res.Content) == 0 {
		t.Fatalf("Expected content, got none")
	}

	textContent := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(textContent, "Test Title") {
		t.Errorf("Expected title in response, got %s", textContent)
	}
}

func TestHandleFetchURL(t *testing.T) {
	// Mock scraper
	mockScraperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/extract" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(scraper.ExtractResponse{
				Markdown: "# Fetched Markdown",
				Success:  true,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockScraperServer.Close()

	scraperClient := scraper.NewScraperClient(mockScraperServer.URL)
	logger := slog.Default()
	srv := NewServer(&MockSearchProvider{}, scraperClient, logger)

	// Call HandleFetchURL directly
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "fetch_url",
			Arguments: map[string]any{
				"url": "http://example.com",
			},
		},
	}

	res, err := srv.HandleFetchURL(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleFetchURL returned unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("Expected success, got tool error")
	}

	if len(res.Content) == 0 {
		t.Fatalf("Expected content, got none")
	}

	textContent := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(textContent, "# Fetched Markdown") {
		t.Errorf("Expected markdown in response, got %s", textContent)
	}
}

func TestFetchToolFailureCategories(t *testing.T) {
	for _, category := range []string{"not_found", "robots_denied", "timeout", "failed https://private.example"} {
		worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(scraper.ExtractResponse{Error: category})
		}))
		server := NewServer(&MockSearchProvider{}, scraper.NewScraperClient(worker.URL), slog.Default())
		request := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "fetch_url", Arguments: map[string]any{"url": "https://private.example"}}}
		result, err := server.HandleFetchURL(context.Background(), request)
		worker.Close()
		if err != nil || !result.IsError {
			t.Fatalf("expected tool error, got %v %v", result, err)
		}
		text := result.Content[0].(mcp.TextContent).Text
		expected := category
		if strings.Contains(category, "private.example") {
			expected = "extraction_failed"
		}
		if text != "Failed to fetch URL: "+expected {
			t.Fatalf("unsafe or unexpected tool error %q", text)
		}
	}
}
