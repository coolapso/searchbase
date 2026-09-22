package loadtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestMemorySummary(t *testing.T) {
	got := memorySummary([]Sample{{Components: map[string]ComponentSample{"crawl-worker": {WorkingSetBytes: 256 * 1024 * 1024, LimitBytes: 2 * 1024 * 1024 * 1024}}}})
	if !strings.Contains(got, "crawl-worker") || !strings.Contains(got, "256 MiB") {
		t.Fatalf("unexpected memory summary: %s", got)
	}
}
