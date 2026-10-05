package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ScraperClient calls the internal crawl-worker service.
type ScraperClient struct {
	BaseURL    string
	HTTPClient *http.Client
	tracer     trace.Tracer
}

// ExtractRequest is the internal payload sent to crawl-worker /extract.
type ExtractRequest struct {
	URL      string `json:"url"`
	JSRender bool   `json:"js_render"`
}

// ExtractResponse is the internal response returned by crawl-worker /extract.
type ExtractResponse struct {
	Markdown string `json:"markdown"`
	Success  bool   `json:"success"`
	Error    string `json:"error"`
}

// NewScraperClient creates a worker client with OpenTelemetry HTTP transport.
func NewScraperClient(baseURL string) *ScraperClient {
	return &ScraperClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		tracer: otel.Tracer("search-gateway"),
	}
}

// Extract asks crawl-worker to fetch a URL and return optimized Markdown.
func (c *ScraperClient) Extract(ctx context.Context, url string, jsRender bool) (string, error) {
	ctx, span := c.tracer.Start(ctx, "ScraperClient.Extract")
	defer span.End()

	reqBody := ExtractRequest{
		URL:      url,
		JSRender: jsRender,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to marshal request")
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/extract", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create request")
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "scrape request failed")
		return "", fmt.Errorf("scraper request failed: %w", err)
	}
	//nolint:errcheck
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("scraper returned non-200 status: %d", resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, "non-200 response")
		return "", err
	}

	var extractResp ExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&extractResp); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode scraper response")
		return "", fmt.Errorf("failed to decode scraper response: %w", err)
	}

	if !extractResp.Success {
		err := safeWorkerError(extractResp.Error)
		span.RecordError(err)
		span.SetStatus(codes.Error, "scraper failed internally")
		return "", err
	}

	return extractResp.Markdown, nil
}
