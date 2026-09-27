// fixture is the only upstream visible to the benchmark network. It provides
// DDGS-shaped search data, a crawl-worker-shaped extractor, and local pages.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coolapso/searchbase/loadtest/internal/loadtest"
)

type searchResponse struct {
	Results []result `json:"results"`
}
type result struct {
	Title string `json:"title"`
	Href  string `json:"href"`
	Body  string `json:"body"`
}

var requests atomic.Int64

func main() {
	latency, _ := time.ParseDuration(env("FIXTURE_LATENCY", "0ms"))
	failure, _ := strconv.Atoi(env("FIXTURE_FAILURE_PERCENT", "0"))
	addr := ":" + env("FIXTURE_PORT", "8081")
	log.Printf("fixture listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, fixtureHandler(latency, failure)))
}

func fixtureHandler(latency time.Duration, failure int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/docker-metrics", func(w http.ResponseWriter, r *http.Request) {
		service := r.URL.Query().Get("service")
		if !allowedService(service) {
			http.Error(w, "unknown service", http.StatusBadRequest)
			return
		}
		sample, err := loadtest.DockerSample(service)
		if err != nil {
			http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		label := `{container_label_com_docker_compose_service="` + service + `"}`
		for _, metric := range []struct {
			name  string
			value float64
		}{
			{"container_cpu_usage_seconds_total", sample.CPUSeconds},
			{"container_spec_cpu_quota", sample.CPUQuotaCores * 100000},
			{"container_spec_cpu_period", 100000},
			{"container_cpu_cfs_periods_total", sample.Periods},
			{"container_cpu_cfs_throttled_periods_total", sample.ThrottledPeriods},
			{"container_memory_working_set_bytes", sample.WorkingSetBytes},
			{"container_spec_memory_limit_bytes", sample.LimitBytes},
			{"container_oom_events_total", sample.OOMEvents},
			{"container_restart_count", sample.Restarts},
			{"container_start_time_seconds", sample.StartedAtSeconds},
		} {
			fmt.Fprintf(w, "%s%s %g\n", metric.name, label, metric.value)
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/failure/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "controlled failure", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/failure/invalid-json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	})
	mux.HandleFunc("/failure/timeout", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/failure/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if h, ok := w.(http.Hijacker); ok {
			conn, _, err := h.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
	})
	mux.HandleFunc("/search/text", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		size := "small"
		if strings.HasSuffix(request.Query, "-medium") {
			size = "medium"
		}
		if strings.HasSuffix(request.Query, "-large") {
			size = "large"
		}
		bytes := 128
		if size == "medium" {
			bytes = 4096
		}
		if size == "large" {
			bytes = 65536
		}
		results := make([]result, 3)
		for i := range results {
			results[i] = result{Title: "Fixture result", Href: "http://fixture:8081/site/" + size, Body: strings.Repeat("deterministic fixture ", bytes/22+1)}
		}
		reply(w, latency, failure, searchResponse{Results: results})
	})
	mux.HandleFunc("/extract", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		reply(w, latency, failure, map[string]any{"markdown": "# Fixture content\n\nDeterministic local benchmark content.", "success": true, "error": ""})
	})
	mux.HandleFunc("/site/", func(w http.ResponseWriter, r *http.Request) {
		page := strings.TrimPrefix(r.URL.Path, "/site/")
		if !allowed(page) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		size := 128
		if strings.Contains(page, "medium") {
			size = 4096
		}
		if strings.Contains(page, "large") {
			size = 65536
		}
		body := strings.Repeat("Searchbase deterministic fixture content. ", size/38+1)
		if strings.HasSuffix(page, "-js") {
			fmt.Fprintf(w, "<!doctype html><title>Fixture JavaScript</title><main id=app>loading</main><script>document.getElementById('app').textContent=%q;</script>", body)
			return
		}
		fmt.Fprintf(w, "<!doctype html><title>Fixture static</title><main><h1>Fixture</h1><p>%s</p></main>", body)
	})
	return mux
}
func reply(w http.ResponseWriter, latency time.Duration, failure int, payload any) {
	if latency > 0 {
		time.Sleep(latency)
	}
	// A counter sequence makes the configured failure percentage repeatable.
	if failure > 0 && requests.Add(1)%100 < int64(failure) {
		http.Error(w, "fixture failure", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
func allowed(s string) bool {
	for _, v := range []string{"small", "medium", "large", "small-js", "medium-js", "large-js"} {
		if s == v {
			return true
		}
	}
	return false
}
func allowedService(name string) bool {
	for _, allowed := range []string{"search-gateway", "search-gateway-isolated", "crawl-worker", "fixture", "cadvisor", "node-exporter", "loadtest"} {
		if name == allowed {
			return true
		}
	}
	return false
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
