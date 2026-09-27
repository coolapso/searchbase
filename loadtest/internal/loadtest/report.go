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
		if name == "host" && c.LimitBytes > 0 && c.WorkingSetBytes/c.LimitBytes > .85 {
			return "host memory above 85% of limit"
		}
		if name != "search-gateway" && name != "search-gateway-isolated" && name != "crawl-worker" {
			continue // observer and fixture resources are diagnostics, not sizing limits.
		}
		if c.LimitBytes > 0 && c.WorkingSetBytes/c.LimitBytes > .85 {
			return name + " memory above 85% of limit"
		}
		if c.CPUQuotaCores > 0 && c.CPUUtilization > .85 {
			return name + " CPU above 85% of quota"
		}
		if c.ThrottledFraction > .05 {
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
	_ = w.Write([]string{"at", "stage", "target_rate", "achieved_rate", "active_requests", "p50_ms", "p95_ms", "p99_ms", "errors", "missed_requests", "gateway_working_set_bytes", "worker_working_set_bytes", "host_memory_used_bytes", "gateway_cpu_utilization", "worker_cpu_utilization", "observer_health"})
	for _, s := range r.Samples {
		gateway, worker, host := s.Components["search-gateway"], s.Components["crawl-worker"], s.Components["host"]
		_ = w.Write([]string{s.At.Format("2006-01-02T15:04:05Z07:00"), s.Stage, fmt.Sprintf("%.3f", s.TargetRate), fmt.Sprintf("%.3f", s.AchievedRate), fmt.Sprint(s.Active), fmt.Sprintf("%.3f", s.Latency.P50MS), fmt.Sprintf("%.3f", s.Latency.P95MS), fmt.Sprintf("%.3f", s.Latency.P99MS), fmt.Sprint(s.Errors), fmt.Sprint(s.Missed), fmt.Sprintf("%.0f", gateway.WorkingSetBytes), fmt.Sprintf("%.0f", worker.WorkingSetBytes), fmt.Sprintf("%.0f", host.WorkingSetBytes), fmt.Sprintf("%.4f", gateway.CPUUtilization), fmt.Sprintf("%.4f", worker.CPUUtilization), s.ObserverHealth})
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
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(fmt.Sprintf("# Searchbase load-test summary\n\n- Scenario: `%s` (%s; payload %s; JavaScript %t)\n- Operation mix: %s\n- Profile: `%s`\n- Instance: `%s` (comparable: %t)\n- Git revision: `%s`\n- Detected host: %d CPUs, %.1f GiB RAM\n- Status: %s\n- Throughput: %.2f successful operations/s\n- p50/p95/p99: %.2f / %.2f / %.2f ms\n- Last stable rate: %.2f %s\n- Safe sustained capacity: %.2f %s\n- First limiting signal: %s\n\n## Peak memory\n\n%s\n\nThe safe value is 70%% of the last stable stage, not a guarantee. Idle SSE rates are session counts rather than requests/s. Smoke and co-located runs are functional/diagnostic, not production sizing evidence. Reports omit queries, URLs, request bodies, and response bodies.\n", r.Scenario, r.Mode, empty(r.Payload, "mixed"), r.JSRender, operationSummary(r.Operations), r.Profile, r.InstanceLabel, r.Comparable, empty(r.GitRevision, "unknown"), r.DetectedCPUs, float64(r.DetectedMemory)/(1024*1024*1024), invalid, r.Throughput, r.Latency.P50MS, r.Latency.P95MS, r.Latency.P99MS, r.LastStableRate, capacityUnit(r.Mode), r.SafeCapacity, capacityUnit(r.Mode), empty(r.Saturation, "not reached"), memorySummary(r.Samples))), 0640)
}
func capacityUnit(mode string) string {
	if mode == "idle" {
		return "sessions"
	}
	return "operations/s"
}
func operationSummary(operations []Operation) string {
	if len(operations) == 0 {
		return "single operation"
	}
	parts := make([]string, 0, len(operations))
	for _, operation := range operations {
		parts = append(parts, fmt.Sprintf("%s:%d", operation.Kind, operation.Weight))
	}
	return strings.Join(parts, ", ")
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
	for _, name := range []string{"search-gateway", "search-gateway-isolated", "crawl-worker", "fixture", "loadtest", "cadvisor", "node-exporter", "host"} {
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
