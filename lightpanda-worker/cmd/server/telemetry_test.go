package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWorkerTelemetryPropagatesTraceAndRecordsOutcomes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()
	telemetry, err := makeWorkerTelemetry(tp, mp)
	if err != nil {
		t.Fatal(err)
	}
	s := (&server{fetcher: &fakeFetcher{content: "# Page"}, jobs: make(chan struct{}, 1),
		timeout: time.Second, telemetry: telemetry}).routes()
	valid := httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader(`{"url":"https://secret.example/page"}`))
	valid.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, valid)
	if response.Code != http.StatusOK {
		t.Fatalf("valid request status = %d", response.Code)
	}
	invalid := httptest.NewRecorder()
	s.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader(`{"url":"file:///etc/passwd"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d", invalid.Code)
	}

	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("expected server, browser, and rejected-request spans, got %d", len(spans))
	}
	var serverSpan, browserSpan sdktrace.ReadOnlySpan
	for _, span := range spans {
		if strings.Contains(span.Name(), "secret.example") {
			t.Fatal("URL leaked into span name")
		}
		for _, attr := range span.Attributes() {
			if strings.Contains(attr.Value.Emit(), "secret.example") {
				t.Fatal("URL leaked into span attribute")
			}
		}
		if span.Name() == "POST /extract" && span.SpanContext().TraceID().String() == "11111111111111111111111111111111" {
			serverSpan = span
		}
		if span.Name() == "Lightpanda.fetch" {
			browserSpan = span
		}
	}
	if serverSpan == nil || browserSpan == nil ||
		browserSpan.Parent().SpanID() != serverSpan.SpanContext().SpanID() ||
		serverSpan.Parent().SpanID().String() != "2222222222222222" {
		t.Fatal("gateway trace context was not linked to the browser span")
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	var requests, durations, browserDurations int
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "lightpanda_worker_requests":
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					for _, point := range sum.DataPoints {
						requests += int(point.Value)
					}
				}
			case "lightpanda_worker_request_duration":
				if histogram, ok := m.Data.(metricdata.Histogram[float64]); ok {
					for _, point := range histogram.DataPoints {
						durations += int(point.Count)
					}
				}
			case "lightpanda_worker_browser_duration":
				if histogram, ok := m.Data.(metricdata.Histogram[float64]); ok {
					for _, point := range histogram.DataPoints {
						browserDurations += int(point.Count)
					}
				}
			}
		}
	}
	if requests != 2 || durations != 2 || browserDurations != 1 {
		t.Fatalf("unexpected metric counts: requests %d, durations %d, browser durations %d", requests, durations, browserDurations)
	}
}

func TestWorkerTelemetryExportsBothSignals(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	telemetry, err := initWorkerTelemetryWithClient(context.Background(), "http://collector.invalid:4318", client)
	if err != nil {
		t.Fatal(err)
	}
	handler := (&server{fetcher: &fakeFetcher{content: "# Page"}, jobs: make(chan struct{}, 1),
		timeout: time.Second, telemetry: telemetry}).routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader(`{"url":"https://example.org"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := telemetry.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if paths["/v1/traces"] == 0 || paths["/v1/metrics"] == 0 {
		t.Fatalf("missing OTLP export: %v", paths)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestWorkerTelemetryRejectsInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:4318", "http://user:password@localhost:4318", "http://localhost:4318?token=secret"} {
		if _, err := initWorkerTelemetry(context.Background(), endpoint); err == nil {
			t.Fatalf("accepted invalid OTLP endpoint")
		}
	}
}
