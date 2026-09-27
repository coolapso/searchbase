package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const maxRequestBytes = 32 << 10
const maxMarkdownBytes = 8 << 20

type extractRequest struct {
	URL      string `json:"url"`
	JSRender bool   `json:"js_render"` // Accepted for gateway compatibility; Lightpanda always executes JavaScript.
}

type extractResponse struct {
	Markdown string `json:"markdown"`
	Success  bool   `json:"success"`
	Error    string `json:"error"`
}

type fetcher interface {
	Fetch(context.Context, string) (string, error)
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("markdown output too large")
	}
	return b.Buffer.Write(p)
}

type lightpandaFetcher struct {
	binary string
	waitMS int
}

func (f lightpandaFetcher) Fetch(ctx context.Context, target string) (string, error) {
	target, err := sanitizeTarget(target)
	if err != nil {
		return "", err
	}
	// Lightpanda's fetch command runs its JS-capable browser and dumps its own Markdown.
	// Never include stderr in API responses: upstream errors can contain the target URL.
	command := exec.CommandContext(ctx, f.binary, "fetch", "--dump", "markdown",
		"--fail-on-http-error",
		"--wait-ms", strconv.Itoa(f.waitMS), "--log-level", "error", target)
	output := &cappedBuffer{limit: maxMarkdownBytes}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	markdown := strings.TrimSpace(output.String())
	if markdown == "" {
		return "", errors.New("empty markdown")
	}
	return markdown, nil
}

type server struct {
	fetcher   fetcher
	jobs      chan struct{}
	timeout   time.Duration
	telemetry *workerTelemetry
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /extract", s.extract)
	return mux
}

func sanitizeTarget(raw string) (string, error) {
	invalid := errors.New("invalid URL")
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' {
			return "", invalid
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Opaque != "" || u.User != nil || u.Host == "" {
		return "", invalid
	}
	host := u.Hostname()
	if host == "" {
		return "", invalid
	}
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return "", invalid
		}
	} else {
		// Accept DNS names and local fixture service names, but not malformed
		// authorities or Unicode hostnames that have not been IDNA-encoded.
		name := strings.TrimSuffix(host, ".")
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", invalid
			}
			for _, r := range label {
				if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' ||
					'0' <= r && r <= '9' || r == '-' || r == '_') {
					return "", invalid
				}
			}
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", invalid
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalid
		}
	}
	// Fragments never go to the HTTP server and may contain private page state.
	u.Fragment, u.RawFragment = "", ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func respond(w http.ResponseWriter, status int, result extractResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func (s *server) extract(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	status := http.StatusOK
	outcome := "success"
	ctx := r.Context()
	var span trace.Span
	if s.telemetry != nil {
		ctx = s.telemetry.propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))
		ctx, span = s.telemetry.tracer.Start(ctx, "POST /extract", trace.WithSpanKind(trace.SpanKindServer))
		s.telemetry.active.Add(ctx, 1)
		defer func() { s.telemetry.finish(ctx, span, outcome, time.Since(start)) }()
	}
	defer func() {
		slog.Info("request handled", "method", r.Method, "path", r.URL.Path,
			"status", status, "latency_ms", time.Since(start).Milliseconds())
	}()
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var request extractRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		outcome = "invalid_request"
		status = http.StatusBadRequest
		respond(w, status, extractResponse{Error: "invalid request"})
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		outcome = "invalid_request"
		status = http.StatusBadRequest
		respond(w, status, extractResponse{Error: "invalid request"})
		return
	}
	target, err := sanitizeTarget(request.URL)
	if err != nil {
		outcome = "invalid_request"
		status = http.StatusBadRequest
		respond(w, status, extractResponse{Error: "invalid request"})
		return
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	select {
	case s.jobs <- struct{}{}:
		defer func() { <-s.jobs }()
	case <-ctx.Done():
		outcome = "busy_or_timeout"
		respond(w, status, extractResponse{Error: "worker busy or timed out"})
		return
	}
	browserCtx := ctx
	var browserSpan trace.Span
	if s.telemetry != nil {
		browserCtx, browserSpan = s.telemetry.tracer.Start(ctx, "Lightpanda.fetch", trace.WithSpanKind(trace.SpanKindInternal))
	}
	browserStart := time.Now()
	markdown, err := s.fetcher.Fetch(browserCtx, target)
	if s.telemetry != nil {
		s.telemetry.browserDuration.Record(ctx, time.Since(browserStart).Seconds())
		if err != nil {
			browserSpan.SetStatus(codes.Error, "extraction failed")
		}
		browserSpan.End()
	}
	if err != nil {
		outcome = "extraction_error"
		// Preserve the old worker's success=false JSON contract, not its potentially
		// sensitive upstream error text. The gateway must not log fetched URLs.
		respond(w, status, extractResponse{Error: "extraction failed"})
		return
	}
	respond(w, status, extractResponse{Markdown: markdown, Success: true, Error: ""})
}

func envInt(name string, fallback, minimum, maximum int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < minimum || n > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return n, nil
}

func main() {
	logLevel := new(slog.LevelVar)
	logLevel.Set(slog.LevelError)
	switch strings.ToLower(os.Getenv("LIGHTPANDA_WORKER_LOG_LEVEL")) {
	case "", "error":
	case "warn":
		logLevel.Set(slog.LevelWarn)
	case "info":
		logLevel.Set(slog.LevelInfo)
	case "debug":
		logLevel.Set(slog.LevelDebug)
	default:
		slog.Error("invalid LIGHTPANDA_WORKER_LOG_LEVEL")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))
	port, err := envInt("LIGHTPANDA_WORKER_PORT", 8000, 1, 65535)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err != nil {
			os.Exit(1)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			os.Exit(1)
		}
		return
	}
	concurrency, err := envInt("LIGHTPANDA_WORKER_CONCURRENCY", 4, 1, 128)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	timeoutSeconds, err := envInt("LIGHTPANDA_WORKER_TIMEOUT_SECONDS", 30, 1, 300)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	waitMS, err := envInt("LIGHTPANDA_WORKER_WAIT_MS", 500, 0, 30000)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	var telemetry *workerTelemetry
	if enabled := os.Getenv("LIGHTPANDA_WORKER_OTEL_ENABLED"); enabled != "" {
		useOTel, err := strconv.ParseBool(enabled)
		if err != nil {
			slog.Error("LIGHTPANDA_WORKER_OTEL_ENABLED must be true or false")
			os.Exit(2)
		}
		if useOTel {
			telemetry, err = initWorkerTelemetry(context.Background(), os.Getenv("LIGHTPANDA_WORKER_OTEL_ENDPOINT"))
			if err != nil {
				slog.Error("failed to initialize telemetry", "error", err)
				os.Exit(2)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := telemetry.shutdown(ctx); err != nil {
					slog.Error("failed to flush telemetry", "error", err)
				}
			}()
		}
	}
	handler := (&server{fetcher: lightpandaFetcher{binary: "/bin/lightpanda", waitMS: waitMS},
		jobs: make(chan struct{}, concurrency), timeout: time.Duration(timeoutSeconds) * time.Second,
		telemetry: telemetry}).routes()
	httpServer := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: time.Duration(timeoutSeconds+5) * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	slog.Info("lightpanda worker starting", "port", port, "concurrency", concurrency)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
