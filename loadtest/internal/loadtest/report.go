package loadtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Evaluate(s Scenario, sample Sample, baselineP95 float64) string {
	if sample.Errors > 0 && sample.Latency.Count > 0 && float64(sample.Errors)/float64(sample.Latency.Count) > .01 {
		return "unexpected failures above 1%"
	}
	if baselineP95 > 0 && sample.Latency.P95MS > baselineP95*2 {
		return "p95 latency above 2x baseline"
	}
	if s.StrictP95MS > 0 && sample.Latency.P95MS > s.StrictP95MS {
		return "scenario p95 SLO exceeded"
	}
	if sample.Latency.Count > 0 && float64(sample.Missed)/float64(sample.Latency.Count+sample.Missed) > .01 {
		return "generator missed more than 1% of scheduled requests"
	}
	for name, c := range sample.Components {
		if name != "search-gateway" && name != "crawl-worker" {
			continue // observer and fixture resources are diagnostics, not sizing limits.
		}
		if c.LimitBytes > 0 && c.WorkingSetBytes/c.LimitBytes > .85 {
			return name + " memory above 85% of limit"
		}
		if c.Periods > 0 && c.ThrottledPeriods/c.Periods > .05 {
			return name + " CPU throttling above 5%"
		}
		if c.OOMEvents > 0 || c.Restarts > 0 {
			return name + " restarted or OOM-killed"
		}
	}
	return ""
}

func WriteReport(dir string, r Report) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "report.json"), append(b, '\n'), 0640); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "samples.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"at", "achieved_rate", "active_requests", "p50_ms", "p95_ms", "p99_ms", "errors", "missed_requests", "gateway_working_set_bytes", "worker_working_set_bytes", "host_memory_used_bytes"})
	for _, s := range r.Samples {
		gateway, worker, host := s.Components["search-gateway"], s.Components["crawl-worker"], s.Components["host"]
		_ = w.Write([]string{s.At.Format("2006-01-02T15:04:05Z07:00"), fmt.Sprintf("%.3f", s.AchievedRate), fmt.Sprint(s.Active), fmt.Sprintf("%.3f", s.Latency.P50MS), fmt.Sprintf("%.3f", s.Latency.P95MS), fmt.Sprintf("%.3f", s.Latency.P99MS), fmt.Sprint(s.Errors), fmt.Sprint(s.Missed), fmt.Sprintf("%.0f", gateway.WorkingSetBytes), fmt.Sprintf("%.0f", worker.WorkingSetBytes), fmt.Sprintf("%.0f", host.WorkingSetBytes)})
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	invalid := "valid"
	if len(r.InvalidReasons) > 0 {
		invalid = "invalid: " + strings.Join(r.InvalidReasons, "; ")
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(fmt.Sprintf("# Searchbase load-test summary\n\n- Scenario: `%s`\n- Profile: `%s`\n- Status: %s\n- Throughput: %.2f successful operations/s\n- p50/p95/p99: %.2f / %.2f / %.2f ms\n- Last stable rate: %.2f operations/s\n- Safe sustained capacity: %.2f operations/s\n- Saturation: %s\n\n## Peak memory\n\n%s\n\nReports deliberately omit queries, URLs, request bodies, and response bodies.\n", r.Scenario, r.Profile, invalid, r.Throughput, r.Latency.P50MS, r.Latency.P95MS, r.Latency.P99MS, r.LastStableRate, r.SafeCapacity, empty(r.Saturation, "not reached"), memorySummary(r.Samples))), 0640)
}
func empty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func memorySummary(samples []Sample) string {
	peaks := map[string]ComponentSample{}
	for _, sample := range samples {
		for name, current := range sample.Components {
			if current.WorkingSetBytes > peaks[name].WorkingSetBytes {
				peaks[name] = current
			}
		}
	}
	if len(peaks) == 0 {
		return "No component samples were collected."
	}
	var rows []string
	for _, name := range []string{"search-gateway", "crawl-worker", "fixture", "loadtest", "cadvisor", "node-exporter", "host"} {
		peak, ok := peaks[name]
		if !ok {
			continue
		}
		limit := "unlimited"
		if peak.LimitBytes > 0 {
			limit = fmt.Sprintf("%.0f MiB (%.1f%%)", peak.LimitBytes/(1024*1024), 100*peak.WorkingSetBytes/peak.LimitBytes)
		}
		rows = append(rows, fmt.Sprintf("- `%s`: %.0f MiB working set; limit %s", name, peak.WorkingSetBytes/(1024*1024), limit))
	}
	return strings.Join(rows, "\n")
}
