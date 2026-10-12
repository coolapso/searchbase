package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StaanProvider connects the gateway to the Staan Search API.
type StaanProvider struct {
	client  *http.Client
	address string
	tracer  trace.Tracer
	apiKey  string
}

type staanResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

type staanResponse struct {
	Web staanSearchResponse `json:"web"`
}

type staanSearchResponse struct {
	Results []staanResult `json:"results"`
}

// NewStaanProvider creates a Staan provider with the supplied API key.
func NewStaanProvider(apiKey string) *StaanProvider {
	return &StaanProvider{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		address: "https://api.staan.ai",
		tracer:  otel.Tracer("search-gateway"),
		apiKey:  apiKey,
	}
}

// Search sends the query to Staan and converts results into Searchbase's
// compact Result shape.
func (p *StaanProvider) Search(ctx context.Context, req Request) (Results, error) {
	ctx, span := p.tracer.Start(ctx, "StaanProvider.Search")
	defer span.End()

	span.SetAttributes(
		attribute.String("search.provider", "staan"),
	)

	req.Normalize()

	sr, err := p.newRequest(ctx, req)
	if err != nil {
		errMsg := fmt.Errorf("failed to create staan search request")
		span.RecordError(errMsg)
		span.SetStatus(codes.Error, "failed to create request")
		return nil, errMsg
	}

	resp, err := p.client.Do(sr)
	if err != nil {
		errMsg := fmt.Errorf("staan search provider request failed")
		span.RecordError(errMsg)
		span.SetStatus(codes.Error, "http request failed")
		return nil, errMsg
	}
	//nolint:errcheck
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Errorf("staan search provider returned status %d", resp.StatusCode)
		span.RecordError(errMsg)
		span.SetStatus(codes.Error, "non-200 response")
		return nil, errMsg
	}

	var parsedResp staanResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsedResp); err != nil {
		errMsg := fmt.Errorf("staan search provider returned an invalid response")
		span.RecordError(errMsg)
		span.SetStatus(codes.Error, "failed to decode response")
		return nil, errMsg
	}

	results := make(Results, 0, len(parsedResp.Web.Results))
	for _, r := range parsedResp.Web.Results {
		results = append(results, Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
		})
	}

	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}
	span.SetAttributes(attribute.Int("search.results_count", len(results)))
	return results, nil
}

func (p *StaanProvider) newRequest(ctx context.Context, req Request) (*http.Request, error) {
	searchURL, err := url.Parse(strings.TrimRight(p.address, "/") + "/v2/search/web")
	if err != nil {
		return nil, err
	}
	q := searchURL.Query()
	q.Set("q", req.Query)
	// Omit unsupported regions so Staan uses its default fr-fr market.
	switch strings.ToLower(req.Region) {
	case "us-en":
		q.Set("market", "en-us")
	case "fr-fr":
		q.Set("market", "fr-fr")
	case "de-de":
		q.Set("market", "de-de")
	}
	if req.Page > 1 {
		q.Set("offset", strconv.Itoa((min(req.Page, 4)-1)*10))
	}
	searchURL.RawQuery = q.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Accept", "application/json")
	return request, nil
}
