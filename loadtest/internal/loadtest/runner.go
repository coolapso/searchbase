package loadtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"
)

type RunConfig struct {
	Profile        string
	Rate           float64
	StageDuration  time.Duration
	SampleInterval time.Duration
	Output         string
	InstanceLabel  string
	Comparable     bool
	Targets        Targets
	Metrics        map[string]string
	Revision       string
	Status         io.Writer
}

var metricsHTTPClient = &http.Client{Timeout: 3 * time.Second}

func (c RunConfig) normalize() RunConfig {
	if c.StageDuration == 0 {
		if c.Profile == "smoke" {
			c.StageDuration = 15 * time.Second
		} else if c.Profile == "soak" {
			c.StageDuration = 30 * time.Minute
		} else {
			c.StageDuration = 60 * time.Second
		}
	}
	if c.SampleInterval == 0 {
		c.SampleInterval = 5 * time.Second
	}
	if c.InstanceLabel == "" {
		c.InstanceLabel = "co-located"
	}
	if c.Status == nil {
		c.Status = os.Stdout
	}
	return c
}

func Run(ctx context.Context, s Scenario, cfg RunConfig) (Report, error) {
	cfg = cfg.normalize()
	report := Report{SchemaVersion: ReportSchemaVersion, RunID: fmt.Sprintf("%d", time.Now().UnixNano()), Suite: "searchbase-core", Scenario: s.Name, Profile: cfg.Profile, Comparable: cfg.Comparable && cfg.InstanceLabel != "co-located", InstanceLabel: cfg.InstanceLabel, StartedAt: time.Now().UTC(), GitRevision: cfg.Revision, DetectedCPUs: runtime.NumCPU(), Errors: map[string]int64{}}
	overallStats := NewStats()
	client := NewClient(cfg.Targets)
	baseline := 0.0
	rate := s.StartRate
	if cfg.Rate > 0 {
		rate = cfg.Rate
	}
	max := s.MaxRate
	if max == 0 {
		max = rate * 64
	}
	stages := 1
	if cfg.Profile == "discover" {
		// The scenario's maximum rate, rather than an arbitrary stage count,
		// defines the discovery boundary.
		stages = 64
	}
	if cfg.Profile == "soak" && cfg.Rate == 0 {
		rate *= .7
	}
	for i := 0; i < stages && rate <= max; i++ {
		stageStats := NewStats()
		phase, samples, reason := runStage(ctx, s, cfg, client, stageStats, overallStats, rate, baseline)
		report.Phases = append(report.Phases, phase)
		report.Samples = append(report.Samples, samples...)
		if baseline == 0 && phase.Achieved > 0 {
			baseline = lastP95(samples)
		}
		if reason != "" {
			report.Saturation = reason
			break
		}
		report.LastStableRate = phase.Achieved
		rate *= 2
	}
	lat, errs, missed, _, throughput, classes := overallStats.Snapshot()
	report.FinishedAt = time.Now().UTC()
	report.Latency = lat
	report.Throughput = throughput
	report.Errors = classes
	report.ResourcePerOp = resourcePerOperation(report.Samples, lat.Count-int64(errs))
	if exporterReset(report.Samples) {
		report.InvalidReasons = append(report.InvalidReasons, "exporter counter reset or target restart")
	}
	if errs > 0 {
		report.Errors["total"] = errs
	}
	if missed > 0 {
		report.InvalidReasons = append(report.InvalidReasons, "generator missed scheduled requests")
	}
	if report.LastStableRate == 0 && report.Saturation == "" {
		report.LastStableRate = throughput
	}
	report.SafeCapacity = report.LastStableRate * .7
	if err := WriteReport(cfg.Output, report); err != nil {
		return report, err
	}
	return report, nil
}
func runStage(ctx context.Context, s Scenario, cfg RunConfig, client *Client, stats, overallStats *Stats, rate, baseline float64) (Phase, []Sample, string) {
	started := time.Now().UTC()
	// Stop scheduling at the deadline but let an already-started request drain.
	// Cancelling the request context here would turn otherwise successful work
	// into a false transport failure at every stage boundary.
	scheduleCtx, cancelSchedule := context.WithTimeout(ctx, cfg.StageDuration)
	defer cancelSchedule()
	var wg sync.WaitGroup
	var idle []io.Closer
	var idleMu sync.Mutex
	if s.Mode == "idle" {
		for i := 0; i < s.IdleSessions; i++ {
			x, err := client.OpenIdle(scheduleCtx, cfg.Targets.Gateway)
			if err != nil {
				stats.End(0, "sse")
				overallStats.End(0, "sse")
			} else {
				idleMu.Lock()
				idle = append(idle, x)
				idleMu.Unlock()
			}
		}
	} else {
		interval := time.Duration(float64(time.Second) / rate)
		if interval <= 0 {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-scheduleCtx.Done():
					return
				case <-ticker.C:
					if scheduleCtx.Err() != nil {
						return
					}
					if s.Mode == "closed" {
						_, _, _, active, _, _ := stats.Snapshot()
						if active >= int64(max(1, int(rate))) {
							stats.Miss()
							overallStats.Miss()
							continue
						}
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						stats.Begin()
						overallStats.Begin()
						at := time.Now()
						class, err := client.Do(ctx, s)
						errorClass := func() string {
							if err != nil {
								return class
							}
							return ""
						}()
						stats.End(time.Since(at), errorClass)
						overallStats.End(time.Since(at), errorClass)
					}()
				}
			}
		}()
	}
	tick := time.NewTicker(cfg.SampleInterval)
	defer tick.Stop()
	var samples []Sample
	bad := 0
	reason := ""
	for {
		if scheduleCtx.Err() != nil {
			wg.Wait()
			idleMu.Lock()
			for _, x := range idle {
				_ = x.Close()
			}
			idleMu.Unlock()
			_, _, _, _, achieved, _ := stats.Snapshot()
			return Phase{Name: "stage", TargetRate: rate, StartedAt: started, EndedAt: time.Now().UTC(), Achieved: achieved, Stable: reason == "", Reason: reason}, samples, reason
		}
		select {
		case <-scheduleCtx.Done():
			wg.Wait()
			idleMu.Lock()
			for _, x := range idle {
				_ = x.Close()
			}
			idleMu.Unlock()
			_, _, _, _, achieved, _ := stats.Snapshot()
			return Phase{Name: "stage", TargetRate: rate, StartedAt: started, EndedAt: time.Now().UTC(), Achieved: achieved, Stable: reason == "", Reason: reason}, samples, reason
		case <-tick.C:
			sample := sampleStats(stats, cfg.Metrics)
			samples = append(samples, sample)
			fmt.Fprintf(cfg.Status, "%s rate=%.2f achieved=%.2f active=%d p95=%.1fms errors=%d\n", s.Name, rate, sample.AchievedRate, sample.Active, sample.Latency.P95MS, sample.Errors)
			r := Evaluate(s, sample, baseline)
			if r != "" {
				bad++
				if bad >= 2 {
					reason = r
					cancelSchedule()
				}
			} else {
				bad = 0
			}
		}
	}
}
func sampleStats(s *Stats, endpoints map[string]string) Sample {
	lat, errc, missed, active, rate, _ := s.Snapshot()
	sample := Sample{At: time.Now().UTC(), AchievedRate: rate, Active: active, Latency: lat, Errors: errc, Missed: missed, Components: map[string]ComponentSample{}}
	type metricResult struct {
		name string
		data ComponentSample
		err  error
	}
	results := make(chan metricResult, len(endpoints))
	for name, endpoint := range endpoints {
		name, endpoint := name, endpoint
		go func() {
			var c ComponentSample
			var err error
			if name == "host" {
				c, err = scrapeHost(endpoint)
			} else {
				c, err = scrapeComponent(endpoint, name)
				if err == nil && c.WorkingSetBytes == 0 {
					if fallback, fallbackErr := dockerMemory(name); fallbackErr == nil {
						c.WorkingSetBytes, c.LimitBytes = fallback.WorkingSetBytes, fallback.LimitBytes
					}
				}
			}
			results <- metricResult{name: name, data: c, err: err}
		}()
	}
	for range endpoints {
		result := <-results
		name, c, err := result.name, result.data, result.err
		if err == nil {
			sample.Components[name] = c
		} else {
			sample.ObserverHealth = "metrics unavailable: " + err.Error()
		}
	}
	return sample
}

// dockerMemory is a read-only fallback for Docker releases where cAdvisor
// cannot resolve container layers. It is only used when cAdvisor has no
// working-set reading, so memory sizing remains available on those hosts.
func dockerMemory(service string) (ComponentSample, error) {
	dialer := &net.Dialer{}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", "/var/run/docker.sock")
	}}}
	filters := `{"label":["com.docker.compose.project=searchbase-loadtest","com.docker.compose.service=` + service + `"]}`
	response, err := client.Get("http://docker/containers/json?filters=" + url.QueryEscape(filters))
	if err != nil {
		return ComponentSample{}, err
	}
	defer response.Body.Close()
	var containers []struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&containers); err != nil || len(containers) != 1 {
		if err != nil {
			return ComponentSample{}, err
		}
		return ComponentSample{}, fmt.Errorf("expected one %s container", service)
	}
	statsResponse, err := client.Get("http://docker/containers/" + containers[0].ID + "/stats?stream=false")
	if err != nil {
		return ComponentSample{}, err
	}
	defer statsResponse.Body.Close()
	var stats struct {
		MemoryStats struct {
			Usage float64            `json:"usage"`
			Limit float64            `json:"limit"`
			Stats map[string]float64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(statsResponse.Body).Decode(&stats); err != nil {
		return ComponentSample{}, err
	}
	workingSet := stats.MemoryStats.Usage - stats.MemoryStats.Stats["inactive_file"]
	if workingSet < 0 {
		workingSet = stats.MemoryStats.Usage
	}
	return ComponentSample{WorkingSetBytes: workingSet, LimitBytes: stats.MemoryStats.Limit}, nil
}
func scrapeHost(url string) (ComponentSample, error) {
	resp, err := metricsHTTPClient.Get(url)
	if err != nil {
		return ComponentSample{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return ComponentSample{}, err
	}
	m, err := ParsePrometheus(string(b))
	if err != nil {
		return ComponentSample{}, err
	}
	value := func(metric string) float64 {
		if values := m[metric]; len(values) > 0 {
			return values[0].Value
		}
		return 0
	}
	total, available := value("node_memory_MemTotal_bytes"), value("node_memory_MemAvailable_bytes")
	return ComponentSample{WorkingSetBytes: total - available, LimitBytes: total}, nil
}
func scrapeComponent(url, name string) (ComponentSample, error) {
	resp, err := metricsHTTPClient.Get(url)
	if err != nil {
		return ComponentSample{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ComponentSample{}, fmt.Errorf("metrics status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return ComponentSample{}, err
	}
	m, err := ParsePrometheus(string(b))
	if err != nil {
		return ComponentSample{}, err
	}
	find := func(metric string) float64 {
		for _, v := range m[metric] {
			if v.Labels["container_label_com_docker_compose_service"] == name || v.Labels["name"] == name {
				return v.Value
			}
		}
		return 0
	}
	return ComponentSample{CPUSeconds: find("container_cpu_usage_seconds_total"), WorkingSetBytes: find("container_memory_working_set_bytes"), NetworkRXBytes: find("container_network_receive_bytes_total"), NetworkTXBytes: find("container_network_transmit_bytes_total"), BlockReadBytes: find("container_fs_reads_bytes_total"), BlockWriteBytes: find("container_fs_writes_bytes_total"), ThrottledPeriods: find("container_cpu_cfs_throttled_periods_total"), Periods: find("container_cpu_cfs_periods_total"), OOMEvents: find("container_memory_failcnt"), LimitBytes: find("container_spec_memory_limit_bytes")}, nil
}
func resourcePerOperation(samples []Sample, successes int64) map[string]ComponentSample {
	if len(samples) < 2 || successes < 1 {
		return nil
	}
	first, last := samples[0], samples[len(samples)-1]
	out := map[string]ComponentSample{}
	for name, end := range last.Components {
		start, ok := first.Components[name]
		if !ok {
			continue
		}
		out[name] = ComponentSample{CPUSeconds: (end.CPUSeconds - start.CPUSeconds) / float64(successes), NetworkRXBytes: (end.NetworkRXBytes - start.NetworkRXBytes) / float64(successes), NetworkTXBytes: (end.NetworkTXBytes - start.NetworkTXBytes) / float64(successes), BlockReadBytes: (end.BlockReadBytes - start.BlockReadBytes) / float64(successes), BlockWriteBytes: (end.BlockWriteBytes - start.BlockWriteBytes) / float64(successes)}
	}
	return out
}
func exporterReset(samples []Sample) bool {
	previous := map[string]ComponentSample{}
	for _, sample := range samples {
		for name, current := range sample.Components {
			if old, ok := previous[name]; ok && (CounterReset(old.CPUSeconds, current.CPUSeconds) || CounterReset(old.NetworkRXBytes, current.NetworkRXBytes) || CounterReset(old.NetworkTXBytes, current.NetworkTXBytes)) {
				return true
			}
			previous[name] = current
		}
	}
	return false
}
func lastP95(s []Sample) float64 {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1].Latency.P95MS
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
