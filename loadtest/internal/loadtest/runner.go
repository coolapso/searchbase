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
	DockerMetrics  string
	Revision       string
	Status         io.Writer
	dashboard      *Dashboard
	phaseName      string
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
	cfg.dashboard = NewDashboard(cfg.Status)
	if err := ValidateScenario(s); err != nil {
		return Report{}, err
	}
	if s.Mode == "" {
		s.Mode = "open"
	}
	if err := validateTargets(cfg.Targets, cfg.Metrics, cfg.DockerMetrics); err != nil {
		return Report{}, err
	}
	if cfg.Profile != "smoke" && cfg.Profile != "discover" && cfg.Profile != "soak" {
		return Report{}, fmt.Errorf("unsupported profile %q", cfg.Profile)
	}
	report := Report{SchemaVersion: ReportSchemaVersion, RunID: fmt.Sprintf("%d", time.Now().UnixNano()), Suite: "searchbase-core", Scenario: s.Name, Mode: s.Mode, Payload: s.Payload, JSRender: s.JSRender, Operations: s.Operations, PayloadClasses: s.PayloadClasses, Profile: cfg.Profile, Comparable: cfg.Comparable && cfg.InstanceLabel != "co-located", InstanceLabel: cfg.InstanceLabel, StartedAt: time.Now().UTC(), GitRevision: cfg.Revision, DetectedCPUs: runtime.NumCPU(), Errors: map[string]int64{}}
	overallStats := NewStats()
	client := NewClient(cfg.Targets)
	baseline := 0.0
	rate := s.StartRate
	if s.Mode == "idle" {
		rate = float64(s.IdleSessions)
	}
	if cfg.Rate > 0 {
		rate = cfg.Rate
	}
	max := s.MaxRate
	if s.Mode == "idle" && s.MaxSessions > 0 {
		max = float64(s.MaxSessions)
	}
	if max == 0 {
		max = rate * 64
	}
	if cfg.Profile != "discover" {
		max = rate
	}
	if cfg.Profile == "soak" && cfg.Rate == 0 {
		return Report{}, fmt.Errorf("soak requires RATE from a prior discovery report's safe_capacity")
	}
	warmup := time.Duration(s.WarmupSeconds) * time.Second
	if cfg.Profile == "discover" && s.WarmupSeconds == 0 {
		warmup = 30 * time.Second
	}
	if warmup > 0 {
		warmCfg := cfg
		warmCfg.StageDuration = warmup
		warmCfg.phaseName = "warmup"
		warmStats := NewStats()
		phase, _, _ := runStage(ctx, s, warmCfg, client, warmStats, NewStats(), rate, 0)
		phase.Name = "warmup"
		report.Phases = append(report.Phases, phase)
	}
	stages := 1
	if cfg.Profile == "discover" {
		stages = 64
	}
	var firstUnstable float64
	var stableTarget float64
	runMeasured := func(stageName string, atRate float64, stageScenario Scenario) string {
		stageCfg := cfg
		stageCfg.phaseName = stageName
		stageStats := NewStats()
		if stageScenario.Mode == "idle" {
			stageScenario.IdleSessions = int(atRate)
		}
		phase, samples, reason := runStage(ctx, stageScenario, stageCfg, client, stageStats, overallStats, atRate, baseline)
		phase.Name = stageName
		report.Phases = append(report.Phases, phase)
		report.Samples = append(report.Samples, samples...)
		if expected := int(phase.EndedAt.Sub(phase.StartedAt) / cfg.SampleInterval); expected >= 3 && len(samples) < expected-1 {
			report.InvalidReasons = append(report.InvalidReasons, "missing periodic resource samples")
		}
		if baseline == 0 && phase.Achieved > 0 && reason == "" {
			baseline = lastP95(samples)
		}
		if reason != "" {
			report.Saturation = reason
		} else {
			report.LastStableRate = phase.Achieved
			stableTarget = atRate
		}
		return reason
	}
	if s.Mode == "sweep" {
		for _, size := range s.PayloadClasses {
			for _, op := range s.Operations {
				one := s
				one.Mode, one.Payload, one.Operations = "open", size, []Operation{op}
				name := "sweep-" + op.Kind + "-" + size
				if op.JSRender {
					name += "-js"
				}
				runMeasured(name, rate, one)
				if ctx.Err() != nil {
					break
				}
			}
			if ctx.Err() != nil {
				break
			}
		}
	} else {
		for i := 0; i < stages && rate <= max && ctx.Err() == nil; i++ {
			if reason := runMeasured("stage", rate, s); reason != "" {
				firstUnstable = rate
				break
			}
			rate *= 2
		}
	}
	if cfg.Profile == "discover" && firstUnstable > 0 && report.LastStableRate > 0 && s.Mode != "idle" {
		stable := stableTarget
		for i := 0; i < 5 && ctx.Err() == nil && firstUnstable-stable > .05*stable; i++ {
			mid := (stable + firstUnstable) / 2
			if runMeasured("refine", mid, s) == "" {
				stable = mid
			} else {
				firstUnstable = mid
			}
		}
	}
	cooldown := time.Duration(s.CooldownSeconds) * time.Second
	if cooldown == 0 && cfg.Profile != "smoke" {
		cooldown = 5 * time.Second
	}
	if cooldown > 0 && ctx.Err() == nil {
		started := time.Now().UTC()
		select {
		case <-time.After(cooldown):
		case <-ctx.Done():
		}
		report.Phases = append(report.Phases, Phase{Name: "cooldown", StartedAt: started, EndedAt: time.Now().UTC(), Stable: ctx.Err() == nil})
	}
	lat, errs, missed, _, _, classes := overallStats.Snapshot()
	measuredSeconds := 0.0
	for _, phase := range report.Phases {
		if phase.Name != "warmup" && phase.Name != "cooldown" {
			measuredSeconds += phase.EndedAt.Sub(phase.StartedAt).Seconds()
		}
	}
	throughput := 0.0
	if measuredSeconds > 0 {
		throughput = float64(overallStats.Successes()) / measuredSeconds
	}
	report.FinishedAt = time.Now().UTC()
	report.Latency = lat
	report.LatencyHistogram = overallStats.Histogram()
	report.Throughput = throughput
	report.Errors = classes
	report.ResourcePerOp = resourcePerOperation(report.Samples, lat.Count-int64(errs))
	if exporterReset(report.Samples) {
		report.InvalidReasons = append(report.InvalidReasons, "exporter counter reset or target restart")
	}
	if ctx.Err() != nil {
		report.InvalidReasons = append(report.InvalidReasons, "run interrupted")
	}
	if s.NonSizing {
		report.InvalidReasons = append(report.InvalidReasons, "non-sizing diagnostic scenario")
	}
	if len(report.Samples) == 0 {
		report.InvalidReasons = append(report.InvalidReasons, "no resource samples")
	}
	observerMissing := false
	targetFailed := false
	for _, sample := range report.Samples {
		if sample.ObserverHealth != "" {
			observerMissing = true
		}
		if host, ok := sample.Components["host"]; ok && host.LimitBytes > 0 {
			report.DetectedMemory = uint64(host.LimitBytes)
		}
		for name, component := range sample.Components {
			if (name == "search-gateway" || name == "search-gateway-isolated" || name == "crawl-worker") && (component.OOMEvents > 0 || component.Restarts > 0) {
				targetFailed = true
			}
		}
	}
	if observerMissing {
		report.InvalidReasons = append(report.InvalidReasons, "resource observer unavailable")
	}
	if targetFailed {
		report.InvalidReasons = append(report.InvalidReasons, "target OOM or restart")
	}
	if errs > 0 {
		report.Errors["total"] = errs
		if !s.NonSizing && lat.Count > 0 && float64(errs)/float64(lat.Count) > .01 {
			report.InvalidReasons = append(report.InvalidReasons, "unexpected failures above 1%")
		}
	}
	if missed > 0 {
		report.InvalidReasons = append(report.InvalidReasons, "generator missed scheduled requests")
	}
	report.SafeCapacity = report.LastStableRate * .7
	if len(report.InvalidReasons) > 0 {
		report.SafeCapacity = 0
	}
	if err := WriteReport(cfg.Output, report); err != nil {
		return report, err
	}
	return report, nil
}
func validateTargets(targets Targets, metrics map[string]string, dockerMetrics string) error {
	addresses := []string{targets.Gateway, targets.IsolatedGateway, targets.Worker, targets.Fixture, dockerMetrics}
	for _, address := range metrics {
		addresses = append(addresses, address)
	}
	for _, address := range addresses {
		if address == "" {
			continue
		}
		parsed, err := url.Parse(address)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return fmt.Errorf("load-test target must be an HTTP URL")
		}
		host := parsed.Hostname()
		if host == "search-gateway" || host == "search-gateway-isolated" || host == "crawl-worker" || host == "fixture" || host == "cadvisor" || host == "node-exporter" || host == "localhost" {
			continue
		}
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
			return fmt.Errorf("load-test target must be a fixture service or private IP")
		}
	}
	return nil
}
func runStage(ctx context.Context, s Scenario, cfg RunConfig, client *Client, stats, overallStats *Stats, rate, baseline float64) (Phase, []Sample, string) {
	started := time.Now().UTC()
	stats.Start()
	// Stop scheduling at the deadline but let an already-started request drain.
	// Cancelling the request context here would turn otherwise successful work
	// into a false transport failure at every stage boundary.
	scheduleCtx, cancelSchedule := context.WithTimeout(ctx, cfg.StageDuration)
	defer cancelSchedule()
	deadline, _ := scheduleCtx.Deadline()
	var wg sync.WaitGroup
	schedulerDone := make(chan struct{})
	var idle []io.Closer
	var idleMu sync.Mutex
	if s.Mode == "idle" {
		for i := 0; i < s.IdleSessions; i++ {
			if scheduleCtx.Err() != nil {
				break
			}
			stats.Begin()
			overallStats.Begin()
			at := time.Now()
			x, err := client.OpenIdle(ctx, cfg.Targets.Gateway)
			if err != nil {
				stats.End(time.Since(at), "sse")
				overallStats.End(time.Since(at), "sse")
			} else {
				stats.End(time.Since(at), "")
				overallStats.End(time.Since(at), "")
				idleMu.Lock()
				idle = append(idle, x)
				idleMu.Unlock()
			}
		}
		close(schedulerDone)
	} else {
		interval := time.Duration(float64(time.Second) / rate)
		if interval <= 0 {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		go func() {
			defer close(schedulerDone)
			concurrency := max(1, int(rate))
			if s.Mode == "open" || s.Mode == "sweep" {
				concurrency = max(64, concurrency)
			}
			if concurrency > 4096 {
				concurrency = 4096
			}
			slots := make(chan struct{}, concurrency)
			for {
				select {
				case <-scheduleCtx.Done():
					return
				case <-ticker.C:
					if scheduleCtx.Err() != nil {
						return
					}
					select {
					case slots <- struct{}{}:
					default:
						stats.Miss()
						overallStats.Miss()
						continue
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer func() { <-slots }()
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
	var previous *Sample
	bad := 0
	reason := ""
	for {
		if scheduleCtx.Err() != nil {
			windowEnded := time.Now().UTC()
			if windowEnded.After(deadline) {
				windowEnded = deadline
			}
			<-schedulerDone
			wg.Wait()
			idleMu.Lock()
			alive := aliveSessions(idle)
			for _, x := range idle {
				_ = x.Close()
			}
			idleMu.Unlock()
			finalSample := sampleStats(stats, cfg.Metrics, cfg.DockerMetrics)
			finalSample.Stage, finalSample.TargetRate = cfg.phaseName, rate
			if s.Mode == "idle" {
				finalSample.Active = int64(alive)
			}
			annotateCPU(previous, &finalSample)
			samples = append(samples, finalSample)
			if reason == "" && bad > 0 {
				reason = Evaluate(s, finalSample, baseline)
			}
			if s.Mode == "idle" && alive < s.IdleSessions {
				reason = "idle SSE sessions closed"
			}
			achieved := float64(stats.Successes()) / windowEnded.Sub(started).Seconds()
			if s.Mode == "idle" {
				achieved = float64(alive)
			}
			return Phase{Name: "stage", TargetRate: rate, StartedAt: started, EndedAt: windowEnded, Achieved: achieved, Stable: reason == "", Reason: reason}, samples, reason
		}
		select {
		case <-scheduleCtx.Done():
			windowEnded := time.Now().UTC()
			if windowEnded.After(deadline) {
				windowEnded = deadline
			}
			<-schedulerDone
			wg.Wait()
			idleMu.Lock()
			alive := aliveSessions(idle)
			for _, x := range idle {
				_ = x.Close()
			}
			idleMu.Unlock()
			finalSample := sampleStats(stats, cfg.Metrics, cfg.DockerMetrics)
			finalSample.Stage, finalSample.TargetRate = cfg.phaseName, rate
			if s.Mode == "idle" {
				finalSample.Active = int64(alive)
			}
			annotateCPU(previous, &finalSample)
			samples = append(samples, finalSample)
			if reason == "" && bad > 0 {
				reason = Evaluate(s, finalSample, baseline)
			}
			if s.Mode == "idle" && alive < s.IdleSessions {
				reason = "idle SSE sessions closed"
			}
			achieved := float64(stats.Successes()) / windowEnded.Sub(started).Seconds()
			if s.Mode == "idle" {
				achieved = float64(alive)
			}
			return Phase{Name: "stage", TargetRate: rate, StartedAt: started, EndedAt: windowEnded, Achieved: achieved, Stable: reason == "", Reason: reason}, samples, reason
		case <-tick.C:
			sample := sampleStats(stats, cfg.Metrics, cfg.DockerMetrics)
			sample.Stage, sample.TargetRate = cfg.phaseName, rate
			if s.Mode == "idle" {
				idleMu.Lock()
				sample.Active = int64(aliveSessions(idle))
				idleMu.Unlock()
			}
			annotateCPU(previous, &sample)
			samples = append(samples, sample)
			previous = &samples[len(samples)-1]
			cfg.dashboard.Update(cfg.phaseName, rate, sample)
			r := Evaluate(s, sample, baseline)
			if r == "" && s.Mode == "idle" && sample.Active < int64(s.IdleSessions) {
				r = "idle SSE sessions closed"
			}
			if r == "" && s.Mode != "idle" && time.Since(started) >= 2*cfg.SampleInterval && rate > 0 && sample.AchievedRate < rate*.9 {
				r = "achieved throughput below 90% of target"
			}
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
func aliveSessions(sessions []io.Closer) int {
	alive := 0
	for _, session := range sessions {
		if status, ok := session.(interface{ Alive() bool }); !ok || status.Alive() {
			alive++
		}
	}
	return alive
}
func annotateCPU(previous *Sample, current *Sample) {
	if previous == nil {
		return
	}
	seconds := current.At.Sub(previous.At).Seconds()
	if seconds <= 0 {
		return
	}
	for name, component := range current.Components {
		old, ok := previous.Components[name]
		if !ok || component.CPUQuotaCores <= 0 {
			continue
		}
		component.CPUUtilization = (component.CPUSeconds - old.CPUSeconds) / (seconds * component.CPUQuotaCores)
		periods := component.Periods - old.Periods
		if periods > 0 {
			component.ThrottledFraction = (component.ThrottledPeriods - old.ThrottledPeriods) / periods
		}
		current.Components[name] = component
	}
}
func sampleStats(s *Stats, endpoints map[string]string, dockerMetrics string) Sample {
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
				if err != nil || c.WorkingSetBytes == 0 || c.CPUSeconds == 0 || ((name == "search-gateway" || name == "search-gateway-isolated" || name == "crawl-worker") && c.CPUQuotaCores == 0) {
					var fallback ComponentSample
					var fallbackErr error
					if dockerMetrics != "" {
						fallback, fallbackErr = scrapeComponent(dockerMetrics+"?service="+url.QueryEscape(name), name)
					} else {
						fallback, fallbackErr = DockerSample(name)
					}
					if fallbackErr == nil {
						err = nil
						if c.WorkingSetBytes == 0 {
							c.WorkingSetBytes, c.LimitBytes = fallback.WorkingSetBytes, fallback.LimitBytes
						}
						if c.CPUSeconds == 0 {
							c.CPUSeconds, c.CPUQuotaCores, c.Periods, c.ThrottledPeriods = fallback.CPUSeconds, fallback.CPUQuotaCores, fallback.Periods, fallback.ThrottledPeriods
						}
						if c.CPUQuotaCores == 0 {
							c.CPUQuotaCores = fallback.CPUQuotaCores
						}
						c.Restarts, c.OOMEvents, c.StartedAtSeconds = fallback.Restarts, fallback.OOMEvents, fallback.StartedAtSeconds
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
			if (name == "host" && c.LimitBytes <= 0) || (name != "host" && (c.WorkingSetBytes <= 0 || c.CPUSeconds <= 0 || ((name == "search-gateway" || name == "search-gateway-isolated" || name == "crawl-worker") && c.CPUQuotaCores <= 0))) {
				sample.ObserverHealth = "container CPU or memory unavailable"
			}
			sample.Components[name] = c
		} else {
			sample.ObserverHealth = "metrics unavailable for " + name
		}
	}
	return sample
}

// dockerMemory is a read-only Docker API fallback for hosts where cAdvisor
// cannot resolve container layers. The socket is trusted-harness-only: a
// read-only mount does not restrict Docker API operations.
func DockerSample(service string) (ComponentSample, error) {
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
		CPUStats struct {
			CPUUsage struct {
				TotalUsage float64 `json:"total_usage"`
			} `json:"cpu_usage"`
			ThrottlingData struct {
				Periods          float64 `json:"periods"`
				ThrottledPeriods float64 `json:"throttled_periods"`
			} `json:"throttling_data"`
		} `json:"cpu_stats"`
	}
	if err := json.NewDecoder(statsResponse.Body).Decode(&stats); err != nil {
		return ComponentSample{}, err
	}
	workingSet := stats.MemoryStats.Usage - stats.MemoryStats.Stats["inactive_file"]
	if workingSet < 0 {
		workingSet = stats.MemoryStats.Usage
	}
	inspectResponse, err := client.Get("http://docker/containers/" + containers[0].ID + "/json")
	if err != nil {
		return ComponentSample{}, err
	}
	defer inspectResponse.Body.Close()
	var inspect struct {
		RestartCount float64 `json:"RestartCount"`
		State        struct {
			OOMKilled bool   `json:"OOMKilled"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		HostConfig struct {
			NanoCpus  float64 `json:"NanoCpus"`
			CpuQuota  float64 `json:"CpuQuota"`
			CpuPeriod float64 `json:"CpuPeriod"`
		} `json:"HostConfig"`
	}
	if err := json.NewDecoder(inspectResponse.Body).Decode(&inspect); err != nil {
		return ComponentSample{}, err
	}
	cores := inspect.HostConfig.NanoCpus / 1e9
	if cores == 0 && inspect.HostConfig.CpuPeriod > 0 {
		cores = inspect.HostConfig.CpuQuota / inspect.HostConfig.CpuPeriod
	}
	started, startedErr := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
	startedSeconds := float64(0)
	if startedErr == nil {
		startedSeconds = float64(started.Unix())
	}
	oom := float64(0)
	if inspect.State.OOMKilled {
		oom = 1
	}
	return ComponentSample{WorkingSetBytes: workingSet, LimitBytes: stats.MemoryStats.Limit, CPUSeconds: stats.CPUStats.CPUUsage.TotalUsage / 1e9, CPUQuotaCores: cores, Periods: stats.CPUStats.ThrottlingData.Periods, ThrottledPeriods: stats.CPUStats.ThrottlingData.ThrottledPeriods, Restarts: inspect.RestartCount, OOMEvents: oom, StartedAtSeconds: startedSeconds}, nil
}
func scrapeHost(url string) (ComponentSample, error) {
	resp, err := metricsHTTPClient.Get(url)
	if err != nil {
		return ComponentSample{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ComponentSample{}, fmt.Errorf("host metrics status %d", resp.StatusCode)
	}
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
	if total <= 0 || available < 0 || available > total {
		return ComponentSample{}, fmt.Errorf("host memory metrics unavailable")
	}
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
	quota, period := find("container_spec_cpu_quota"), find("container_spec_cpu_period")
	cores := float64(0)
	if quota > 0 && period > 0 {
		cores = quota / period
	}
	return ComponentSample{CPUSeconds: find("container_cpu_usage_seconds_total"), CPUQuotaCores: cores, StartedAtSeconds: find("container_start_time_seconds"), WorkingSetBytes: find("container_memory_working_set_bytes"), NetworkRXBytes: find("container_network_receive_bytes_total"), NetworkTXBytes: find("container_network_transmit_bytes_total"), BlockReadBytes: find("container_fs_reads_bytes_total"), BlockWriteBytes: find("container_fs_writes_bytes_total"), ThrottledPeriods: find("container_cpu_cfs_throttled_periods_total"), Periods: find("container_cpu_cfs_periods_total"), OOMEvents: find("container_oom_events_total"), Restarts: find("container_restart_count"), LimitBytes: find("container_spec_memory_limit_bytes")}, nil
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
			if old, ok := previous[name]; ok && (CounterReset(old.CPUSeconds, current.CPUSeconds) || CounterReset(old.NetworkRXBytes, current.NetworkRXBytes) || CounterReset(old.NetworkTXBytes, current.NetworkTXBytes) || (old.StartedAtSeconds > 0 && current.StartedAtSeconds > 0 && old.StartedAtSeconds != current.StartedAtSeconds)) {
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
