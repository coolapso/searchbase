package loadtest

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func workerTestClient(body string) *Client {
	client := NewClient(Targets{Worker: "http://worker:8000"})
	client.http = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
			"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)),
			Request: request}, nil
	})}
	return client
}

func TestWorkerFetchCountsJSONFailure(t *testing.T) {
	client := workerTestClient(`{"markdown":"","success":false,"error":"private upstream detail"}`)
	class, err := client.Do(context.Background(), Scenario{Kind: "worker-fetch", Target: "worker", Payload: "medium"})
	if err == nil || class != "extract_failure" {
		t.Fatalf("class = %q, error = %v", class, err)
	}
}

func TestWorkerFetchAcceptsJSONSuccess(t *testing.T) {
	client := workerTestClient(`{"markdown":"# Page","success":true,"error":""}`)
	class, err := client.Do(context.Background(), Scenario{Kind: "worker-fetch", Target: "worker", Payload: "medium"})
	if err != nil || class != "" {
		t.Fatalf("class = %q, error = %v", class, err)
	}
}
