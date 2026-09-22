// fixture is the only upstream visible to the benchmark network. It provides
// DDGS-shaped search data, a crawl-worker-shaped extractor, and local pages.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/search/text", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		reply(w, latency, failure, searchResponse{Results: []result{{"Fixture small result", "http://fixture:8081/site/small", "deterministic fixture"}, {"Fixture medium result", "http://fixture:8081/site/medium", "deterministic fixture"}, {"Fixture large result", "http://fixture:8081/site/large", "deterministic fixture"}}})
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
	addr := ":" + env("FIXTURE_PORT", "8081")
	log.Printf("fixture listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
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
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
