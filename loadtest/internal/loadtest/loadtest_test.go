package loadtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScenarioValidation(t *testing.T) {
	if err := ValidateScenario(Scenario{Name: "x", Kind: "rest-search", Target: "gateway", StartRate: 1, Mode: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateScenario(Scenario{Name: "x", Kind: "x", Target: "x", Mode: "idle"}); err == nil {
		t.Fatal("accepted idle scenario without sessions")
	}
}
func TestAllScenariosLoad(t *testing.T) {
	dir := filepath.Join("..", "..", "scenarios")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if _, err := LoadScenario(dir, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := LoadScenario(dir, "../gateway-rest-search"); err == nil {
		t.Fatal("accepted scenario path traversal")
	}
}
func TestHistogramIsBoundedAndPercentilesIncrease(t *testing.T) {
	stats := NewStats()
	for i := 0; i < 100000; i++ {
		stats.Begin()
		stats.End(time.Duration(1+i%100)*time.Millisecond, "")
	}
	lat, _, _, active, _, _ := stats.Snapshot()
	if lat.Count != 100000 || active != 0 || lat.P50MS > lat.P95MS || lat.P95MS > lat.P99MS {
		t.Fatalf("bad latency aggregation: %+v", lat)
	}
	if len(stats.Histogram()) > 1000 {
		t.Fatalf("histogram grew unexpectedly: %d buckets", len(stats.Histogram()))
	}
}
func TestCPUUtilizationAndSaturation(t *testing.T) {
	at := time.Now()
	previous := Sample{At: at, Components: map[string]ComponentSample{"crawl-worker": {CPUSeconds: 10, CPUQuotaCores: 2}}}
	current := Sample{At: at.Add(time.Second), Components: map[string]ComponentSample{"crawl-worker": {CPUSeconds: 11.8, CPUQuotaCores: 2}}}
	annotateCPU(&previous, &current)
	if got := Evaluate(Scenario{}, current, 0); !strings.Contains(got, "CPU") {
		t.Fatalf("expected CPU saturation, got %q", got)
	}
}
func TestTargetsStayPrivate(t *testing.T) {
	if err := validateTargets(Targets{Gateway: "https://example.com"}, nil, ""); err == nil {
		t.Fatal("accepted public target")
	}
	if err := validateTargets(Targets{Gateway: "http://10.0.0.10:18080"}, nil, "http://10.0.0.10:18082/docker-metrics"); err != nil {
		t.Fatal(err)
	}
}
func TestPrometheusAndCounterReset(t *testing.T) {
	m, err := ParsePrometheus("container_memory_working_set_bytes{container_label_com_docker_compose_service=\"search-gateway\",container_label_note=\"contains spaces\"} 12 1710000000000\n")
	if err != nil || m["container_memory_working_set_bytes"][0].Value != 12 {
		t.Fatal(m, err)
	}
	if !CounterReset(5, 1) {
		t.Fatal("counter reset not detected")
	}
}
func TestSaturation(t *testing.T) {
	s := Scenario{}
	sample := Sample{Latency: Latency{Count: 100}, Errors: 2}
	if got := Evaluate(s, sample, 0); !strings.Contains(got, "failures") {
		t.Fatalf("%q", got)
	}
	sample = Sample{Latency: Latency{Count: 100, P95MS: 201}}
	if got := Evaluate(s, sample, 100); !strings.Contains(got, "p95") {
		t.Fatalf("%q", got)
	}
}
func TestPrivacySafeReport(t *testing.T) {
	r := Report{Scenario: "x", Profile: "smoke", Throughput: 1, Latency: Latency{}, SafeCapacity: .7, Errors: map[string]int64{}, StartedAt: time.Now(), FinishedAt: time.Now()}
	d := t.TempDir()
	if err := WriteReport(d, r); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(d + "/report.json"); err != nil || strings.Contains(string(b), "load-test-query") {
		t.Fatalf("report leaked request data: %s", b)
	}
}
func TestGoldenReportSchema(t *testing.T) {
	root := filepath.Join("..", "..", "schema")
	golden, err := os.ReadFile(filepath.Join(root, "golden-report-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(golden, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != ReportSchemaVersion || report.InstanceLabel != "co-located" {
		t.Fatalf("incompatible golden report: %+v", report)
	}
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	encoded, err := os.ReadFile(filepath.Join(root, "report-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(golden, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range schema.Required {
		if _, ok := fields[key]; !ok {
			t.Errorf("golden report lacks required field %q", key)
		}
	}
}

func TestWorkerJavaScriptRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			JSRender bool   `json:"js_render"`
			URL      string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.JSRender || !strings.HasSuffix(request.URL, "medium-js") {
			t.Fatalf("unexpected worker request: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"markdown":"fixture"}`))
	}))
	defer server.Close()
	client := NewClient(Targets{Worker: server.URL})
	if _, err := client.Do(context.Background(), Scenario{Kind: "worker-fetch", Target: "worker", Payload: "medium", JSRender: true}); err != nil {
		t.Fatal(err)
	}
}
func TestWeightedMixedDispatch(t *testing.T) {
	search, fetch := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			search++
		} else if r.URL.Path == "/api/v1/fetch" {
			fetch++
		} else {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client := NewClient(Targets{Gateway: server.URL})
	s := Scenario{Kind: "mixed", Target: "gateway", Operations: []Operation{{Kind: "rest-search", Target: "gateway", Weight: 3}, {Kind: "rest-fetch", Target: "gateway", Weight: 1}}}
	for i := 0; i < 8; i++ {
		if _, err := client.Do(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	if search != 6 || fetch != 2 {
		t.Fatalf("unexpected mix: search=%d fetch=%d", search, fetch)
	}
	search, fetch = 0, 0
	client = NewClient(Targets{Gateway: server.URL})
	s.Operations = []Operation{{Kind: "rest-search", Target: "gateway", Weight: 40}, {Kind: "rest-fetch", Target: "gateway", Weight: 60}}
	for i := 0; i < 20; i++ {
		if _, err := client.Do(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	if search == 0 || fetch == 0 {
		t.Fatalf("short smoke did not exercise both operations: search=%d fetch=%d", search, fetch)
	}
}
func TestMCPHTTPSessionLifecycle(t *testing.T) {
	steps := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		steps = append(steps, message.Method)
		if message.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "fixture-session")
		} else if r.Header.Get("Mcp-Session-Id") != "fixture-session" {
			t.Error("missing MCP session")
		}
		if message.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	client := NewClient(Targets{Gateway: server.URL})
	if _, err := client.Do(context.Background(), Scenario{Kind: "mcp-http-search", Target: "gateway"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "initialize,notifications/initialized,tools/call" {
		t.Fatalf("unexpected MCP lifecycle: %v", steps)
	}
}
func TestControlledFailureClasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/failure/status":
			http.Error(w, "failure", http.StatusServiceUnavailable)
		case "/failure/invalid-json":
			_, _ = w.Write([]byte("{"))
		case "/failure/timeout":
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
		case "/failure/disconnect":
			if h, ok := w.(http.Hijacker); ok {
				conn, _, err := h.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
		}
	}))
	defer server.Close()
	client := NewClient(Targets{Fixture: server.URL})
	for kind, want := range map[string]string{"fixture-status": "http_503", "fixture-invalid-json": "invalid_json", "fixture-timeout": "timeout", "fixture-disconnect": "transport"} {
		class, err := client.Do(context.Background(), Scenario{Kind: kind, Target: "fixture"})
		if err == nil || class != want {
			t.Errorf("%s: class=%q err=%v; want %q", kind, class, err, want)
		}
	}
}

func TestMemorySummary(t *testing.T) {
	got := memorySummary([]Sample{{Components: map[string]ComponentSample{"crawl-worker": {WorkingSetBytes: 256 * 1024 * 1024, LimitBytes: 2 * 1024 * 1024 * 1024}}}})
	if !strings.Contains(got, "crawl-worker") || !strings.Contains(got, "256 MiB") {
		t.Fatalf("unexpected memory summary: %s", got)
	}
}
