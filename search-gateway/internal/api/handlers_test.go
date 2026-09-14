package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coolapso/searchbase/search-gateway/internal/scraper"
	"github.com/coolapso/searchbase/search-gateway/internal/search"
	"github.com/gin-gonic/gin"
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
			Title:   "Test Result",
			URL:     "http://example.com",
			Snippet: "Test snippet",
		},
	}, nil
}

func TestHandleSearchProviderError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	scraperClient := scraper.NewScraperClient("http://example.com")
	apiServer := NewAPIServer(&MockSearchProvider{Err: errors.New("searxng search provider request failed")}, scraperClient, slog.Default())

	router := gin.New()
	apiServer.RegisterRoutes(router.Group("/api/v1"))

	bodyBytes, _ := json.Marshal(search.Request{Query: "secret private query"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("Expected status code 502, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error != "searxng search provider request failed" {
		t.Fatalf("Expected provider error message, got %s", resp.Error)
	}

	if bytes.Contains(w.Body.Bytes(), []byte("secret private query")) {
		t.Fatalf("Response leaked query: %s", w.Body.String())
	}
}

func TestHandleSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock scraper server
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
	apiServer := NewAPIServer(&MockSearchProvider{}, scraperClient, logger)

	router := gin.New()
	apiServer.RegisterRoutes(router.Group("/api/v1"))

	// Perform the test request
	reqBody := search.Request{
		Query: "test query",
		Limit: 1,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status code 200, got %d", w.Code)
	}

	var resp search.Results
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resp))
	}
}

func TestHandleFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock scraper server
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
	apiServer := NewAPIServer(&MockSearchProvider{}, scraperClient, logger)

	router := gin.New()
	apiServer.RegisterRoutes(router.Group("/api/v1"))

	// Perform the test request
	reqBody := FetchRequest{
		URL: "http://example.com",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/fetch", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status code 200, got %d", w.Code)
	}

	var resp FetchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Markdown != "# Fetched Markdown" {
		t.Errorf("Expected fetched markdown, got %s", resp.Markdown)
	}
}

func TestHandleFetchFailureDoesNotLogURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockScraperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockScraperServer.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	apiServer := NewAPIServer(&MockSearchProvider{}, scraper.NewScraperClient(mockScraperServer.URL), logger)

	router := gin.New()
	apiServer.RegisterRoutes(router.Group("/api/v1"))

	const targetURL = "https://private.example/secret-path"
	bodyBytes, _ := json.Marshal(FetchRequest{URL: targetURL})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/fetch", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status code 500, got %d", w.Code)
	}

	if bytes.Contains(logs.Bytes(), []byte(targetURL)) {
		t.Fatalf("Fetch failure log leaked URL: %s", logs.String())
	}
}
