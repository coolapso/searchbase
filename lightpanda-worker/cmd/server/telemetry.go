package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const telemetryServiceName = "lightpanda-worker"

var durationBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}

type workerTelemetry struct {
	tracer          trace.Tracer
	propagator      propagation.TextMapPropagator
	requests        metric.Int64Counter
	active          metric.Int64UpDownCounter
	requestDuration metric.Float64Histogram
	browserDuration metric.Float64Histogram
	traces          *sdktrace.TracerProvider
	metrics         *sdkmetric.MeterProvider
}

func makeWorkerTelemetry(tp trace.TracerProvider, mp metric.MeterProvider) (*workerTelemetry, error) {
	meter := mp.Meter(telemetryServiceName)
	requests, err := meter.Int64Counter("lightpanda_worker_requests", metric.WithDescription("Completed extraction requests"))
	if err != nil {
		return nil, err
	}
	active, err := meter.Int64UpDownCounter("lightpanda_worker_active_requests", metric.WithDescription("Extraction requests in progress"))
	if err != nil {
		return nil, err
	}
	requestDuration, err := meter.Float64Histogram("lightpanda_worker_request_duration",
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, err
	}
	browserDuration, err := meter.Float64Histogram("lightpanda_worker_browser_duration",
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, err
	}
	return &workerTelemetry{
		tracer:          tp.Tracer(telemetryServiceName),
		propagator:      propagation.TraceContext{},
		requests:        requests,
		active:          active,
		requestDuration: requestDuration,
		browserDuration: browserDuration,
	}, nil
}

func initWorkerTelemetry(ctx context.Context, endpoint string) (*workerTelemetry, error) {
	return initWorkerTelemetryWithClient(ctx, endpoint, nil)
}

func initWorkerTelemetryWithClient(ctx context.Context, endpoint string, client *http.Client) (*workerTelemetry, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("LIGHTPANDA_WORKER_OTEL_ENDPOINT must be an HTTP(S) OTLP base URL")
	}
	base := strings.TrimRight(u.String(), "/")
	traceOptions := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(base + "/v1/traces")}
	metricOptions := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(base + "/v1/metrics")}
	if client != nil {
		traceOptions = append(traceOptions, otlptracehttp.WithHTTPClient(client))
		metricOptions = append(metricOptions, otlpmetrichttp.WithHTTPClient(client))
	}
	traceExporter, err := otlptracehttp.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("initialize trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOptions...)
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, fmt.Errorf("initialize metric exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes("", attribute.String("service.name", telemetryServiceName)))
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		_ = metricExporter.Shutdown(ctx)
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	)
	t, err := makeWorkerTelemetry(tp, mp)
	if err != nil {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil, err
	}
	t.traces, t.metrics = tp, mp
	return t, nil
}

func (t *workerTelemetry) shutdown(ctx context.Context) error {
	return errors.Join(t.metrics.Shutdown(ctx), t.traces.Shutdown(ctx))
}

func (t *workerTelemetry) finish(ctx context.Context, span trace.Span, outcome string, elapsed time.Duration) {
	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	t.requests.Add(ctx, 1, attrs)
	t.requestDuration.Record(ctx, elapsed.Seconds(), attrs)
	t.active.Add(ctx, -1)
	if outcome != "success" {
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
}
