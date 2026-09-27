// Package loadtest contains the deterministic, privacy-safe capacity runner.
package loadtest

import "time"

const ReportSchemaVersion = "searchbase-load-report/v1"

// Scenario uses fixture labels only; no operator URL or customer input belongs
// in a benchmark scenario or report.
type Scenario struct {
	Name            string      `json:"name" yaml:"name"`
	Kind            string      `json:"kind" yaml:"kind"`
	Target          string      `json:"target" yaml:"target"`
	StartRate       float64     `json:"start_rate" yaml:"start_rate"`
	Payload         string      `json:"payload" yaml:"payload"`
	JSRender        bool        `json:"js_render" yaml:"js_render"`
	Mode            string      `json:"mode" yaml:"mode"`
	IdleSessions    int         `json:"idle_sessions" yaml:"idle_sessions"`
	MaxSessions     int         `json:"max_sessions" yaml:"max_sessions"`
	MaxRate         float64     `json:"max_rate" yaml:"max_rate"`
	StrictP95MS     float64     `json:"strict_p95_ms" yaml:"strict_p95_ms"`
	WarmupSeconds   int         `json:"warmup_seconds" yaml:"warmup_seconds"`
	CooldownSeconds int         `json:"cooldown_seconds" yaml:"cooldown_seconds"`
	Operations      []Operation `json:"operations,omitempty" yaml:"operations"`
	PayloadClasses  []string    `json:"payload_classes,omitempty" yaml:"payload_classes"`
	NonSizing       bool        `json:"non_sizing,omitempty" yaml:"non_sizing"`
}

type Operation struct {
	Kind     string `json:"kind" yaml:"kind"`
	Target   string `json:"target" yaml:"target"`
	Payload  string `json:"payload,omitempty" yaml:"payload"`
	JSRender bool   `json:"js_render,omitempty" yaml:"js_render"`
	Weight   int    `json:"weight" yaml:"weight"`
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

type HistogramBucket struct {
	UpperBoundMS float64 `json:"upper_bound_ms"`
	Count        int64   `json:"count"`
}

type ComponentSample struct {
	CPUSeconds        float64 `json:"cpu_seconds,omitempty"`
	CPUQuotaCores     float64 `json:"cpu_quota_cores,omitempty"`
	CPUUtilization    float64 `json:"cpu_utilization,omitempty"`
	StartedAtSeconds  float64 `json:"started_at_seconds,omitempty"`
	WorkingSetBytes   float64 `json:"working_set_bytes,omitempty"`
	NetworkRXBytes    float64 `json:"network_rx_bytes,omitempty"`
	NetworkTXBytes    float64 `json:"network_tx_bytes,omitempty"`
	BlockReadBytes    float64 `json:"block_read_bytes,omitempty"`
	BlockWriteBytes   float64 `json:"block_write_bytes,omitempty"`
	ThrottledPeriods  float64 `json:"throttled_periods,omitempty"`
	ThrottledFraction float64 `json:"throttled_fraction,omitempty"`
	Periods           float64 `json:"cpu_periods,omitempty"`
	Restarts          float64 `json:"restarts,omitempty"`
	OOMEvents         float64 `json:"oom_events,omitempty"`
	LimitBytes        float64 `json:"memory_limit_bytes,omitempty"`
}

type Sample struct {
	At             time.Time                  `json:"at"`
	Stage          string                     `json:"stage"`
	TargetRate     float64                    `json:"target_rate"`
	AchievedRate   float64                    `json:"achieved_rate"`
	Active         int64                      `json:"active_requests"`
	Latency        Latency                    `json:"latency"`
	Errors         int64                      `json:"errors"`
	Missed         int64                      `json:"missed_requests"`
	Components     map[string]ComponentSample `json:"components,omitempty"`
	ObserverHealth string                     `json:"observer_health,omitempty"`
}

type Report struct {
	SchemaVersion    string                     `json:"schema_version"`
	RunID            string                     `json:"run_id"`
	Suite            string                     `json:"suite"`
	Scenario         string                     `json:"scenario"`
	Mode             string                     `json:"mode"`
	Payload          string                     `json:"payload,omitempty"`
	JSRender         bool                       `json:"js_render"`
	Operations       []Operation                `json:"operations,omitempty"`
	PayloadClasses   []string                   `json:"payload_classes,omitempty"`
	Profile          string                     `json:"profile"`
	Comparable       bool                       `json:"comparable"`
	InstanceLabel    string                     `json:"instance_label"`
	StartedAt        time.Time                  `json:"started_at"`
	FinishedAt       time.Time                  `json:"finished_at"`
	GitRevision      string                     `json:"git_revision,omitempty"`
	DetectedCPUs     int                        `json:"detected_cpus"`
	DetectedMemory   uint64                     `json:"detected_memory_bytes"`
	Phases           []Phase                    `json:"phases"`
	Throughput       float64                    `json:"throughput"`
	Latency          Latency                    `json:"latency"`
	LatencyHistogram []HistogramBucket          `json:"latency_histogram,omitempty"`
	Errors           map[string]int64           `json:"errors"`
	Samples          []Sample                   `json:"samples"`
	Saturation       string                     `json:"saturation_reason,omitempty"`
	LastStableRate   float64                    `json:"last_stable_rate"`
	SafeCapacity     float64                    `json:"safe_capacity"`
	ResourcePerOp    map[string]ComponentSample `json:"resource_per_successful_operation,omitempty"`
	InvalidReasons   []string                   `json:"invalid_reasons,omitempty"`
}
