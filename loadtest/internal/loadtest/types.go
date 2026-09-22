// Package loadtest contains the deterministic, privacy-safe capacity runner.
package loadtest

import "time"

const ReportSchemaVersion = "searchbase-load-report/v1"

// Scenario is deliberately small and data-driven. JSON is valid YAML, which
// lets the runner avoid a benchmark-only YAML dependency.
type Scenario struct {
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Target       string  `json:"target"`
	StartRate    float64 `json:"start_rate"`
	Payload      string  `json:"payload"`
	JSRender     bool    `json:"js_render"`
	Mode         string  `json:"mode"`
	IdleSessions int     `json:"idle_sessions"`
	MaxRate      float64 `json:"max_rate"`
	StrictP95MS  float64 `json:"strict_p95_ms"`
}

type Phase struct {
	Name       string    `json:"name"`
	TargetRate float64   `json:"target_rate"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	Achieved   float64   `json:"achieved_rate"`
	Stable     bool      `json:"stable"`
	Reason     string    `json:"reason,omitempty"`
}

type Latency struct {
	Count int64   `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
}

type ComponentSample struct {
	CPUSeconds       float64 `json:"cpu_seconds,omitempty"`
	WorkingSetBytes  float64 `json:"working_set_bytes,omitempty"`
	NetworkRXBytes   float64 `json:"network_rx_bytes,omitempty"`
	NetworkTXBytes   float64 `json:"network_tx_bytes,omitempty"`
	BlockReadBytes   float64 `json:"block_read_bytes,omitempty"`
	BlockWriteBytes  float64 `json:"block_write_bytes,omitempty"`
	ThrottledPeriods float64 `json:"throttled_periods,omitempty"`
	Periods          float64 `json:"cpu_periods,omitempty"`
	Restarts         float64 `json:"restarts,omitempty"`
	OOMEvents        float64 `json:"oom_events,omitempty"`
	LimitBytes       float64 `json:"memory_limit_bytes,omitempty"`
}

type Sample struct {
	At             time.Time                  `json:"at"`
	AchievedRate   float64                    `json:"achieved_rate"`
	Active         int64                      `json:"active_requests"`
	Latency        Latency                    `json:"latency"`
	Errors         int64                      `json:"errors"`
	Missed         int64                      `json:"missed_requests"`
	Components     map[string]ComponentSample `json:"components,omitempty"`
	ObserverHealth string                     `json:"observer_health,omitempty"`
}

type Report struct {
	SchemaVersion  string                     `json:"schema_version"`
	RunID          string                     `json:"run_id"`
	Suite          string                     `json:"suite"`
	Scenario       string                     `json:"scenario"`
	Profile        string                     `json:"profile"`
	Comparable     bool                       `json:"comparable"`
	InstanceLabel  string                     `json:"instance_label"`
	StartedAt      time.Time                  `json:"started_at"`
	FinishedAt     time.Time                  `json:"finished_at"`
	GitRevision    string                     `json:"git_revision,omitempty"`
	DetectedCPUs   int                        `json:"detected_cpus"`
	DetectedMemory uint64                     `json:"detected_memory_bytes"`
	Phases         []Phase                    `json:"phases"`
	Throughput     float64                    `json:"throughput"`
	Latency        Latency                    `json:"latency"`
	Errors         map[string]int64           `json:"errors"`
	Samples        []Sample                   `json:"samples"`
	Saturation     string                     `json:"saturation_reason,omitempty"`
	LastStableRate float64                    `json:"last_stable_rate"`
	SafeCapacity   float64                    `json:"safe_capacity"`
	ResourcePerOp  map[string]ComponentSample `json:"resource_per_successful_operation,omitempty"`
	InvalidReasons []string                   `json:"invalid_reasons,omitempty"`
}
