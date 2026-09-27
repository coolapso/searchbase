package loadtest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadScenario(dir, name string) (Scenario, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return Scenario{}, fmt.Errorf("scenario must be a file name under the scenario directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
	if err != nil {
		return Scenario{}, err
	}
	var scenario Scenario
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&scenario); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario %q: %w", name, err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Scenario{}, fmt.Errorf("scenario %q must contain one YAML document", name)
	}
	if scenario.Name != name {
		return Scenario{}, fmt.Errorf("scenario name must match its file name")
	}
	if err := ValidateScenario(scenario); err != nil {
		return Scenario{}, err
	}
	return scenario, nil
}

func ValidateScenario(s Scenario) error {
	if s.Name == "" || s.Kind == "" || s.Target == "" {
		return fmt.Errorf("scenario requires name, kind, and target")
	}
	if !validKind(s.Kind) || !validTargetLabel(s.Target) {
		return fmt.Errorf("scenario has unsupported kind or target")
	}
	if s.StartRate < 0 || (s.StartRate == 0 && s.Mode != "idle") || s.MaxRate < 0 ||
		(s.MaxRate > 0 && s.MaxRate < s.StartRate) || s.StrictP95MS < 0 {
		return fmt.Errorf("scenario rates and p95 limit must be non-negative and ordered")
	}
	if s.WarmupSeconds < 0 || s.CooldownSeconds < 0 {
		return fmt.Errorf("scenario warmup and cooldown cannot be negative")
	}
	if s.Payload != "" && !validPayload(s.Payload) {
		return fmt.Errorf("unsupported payload class")
	}
	if s.Mode == "" {
		s.Mode = "open"
	}
	if s.Mode != "open" && s.Mode != "closed" && s.Mode != "idle" && s.Mode != "sweep" {
		return fmt.Errorf("unsupported scenario mode %q", s.Mode)
	}
	if s.Mode == "idle" && s.IdleSessions < 1 {
		return fmt.Errorf("idle scenario requires idle_sessions")
	}
	if s.Mode == "idle" && s.Kind != "mcp-sse-search" {
		return fmt.Errorf("idle mode requires MCP SSE")
	}
	if s.Kind == "mixed" && len(s.Operations) == 0 {
		return fmt.Errorf("mixed scenario requires operations")
	}
	if s.Kind == "sweep" && s.Mode != "sweep" {
		return fmt.Errorf("sweep kind requires sweep mode")
	}
	if s.Target == "fixture" && !s.NonSizing {
		return fmt.Errorf("fixture failure scenarios must be non-sizing")
	}
	if s.MaxSessions > 0 && s.MaxSessions < s.IdleSessions {
		return fmt.Errorf("max_sessions must be at least idle_sessions")
	}
	if s.Mode == "sweep" && (len(s.PayloadClasses) == 0 || len(s.Operations) == 0) {
		return fmt.Errorf("payload sweep requires payload_classes and operations")
	}
	for _, size := range s.PayloadClasses {
		if !validPayload(size) {
			return fmt.Errorf("unsupported payload class in sweep")
		}
	}
	weight := 0
	for _, operation := range s.Operations {
		if !validKind(operation.Kind) || !validTargetLabel(operation.Target) ||
			(operation.Payload != "" && !validPayload(operation.Payload)) || operation.Weight < 1 {
			return fmt.Errorf("invalid weighted operation")
		}
		weight += operation.Weight
	}
	if weight > 1000 {
		return fmt.Errorf("operation weights exceed 1000")
	}
	return nil
}

func validKind(kind string) bool {
	switch kind {
	case "rest-search", "rest-fetch", "worker-fetch", "mcp-http-search", "mcp-http-fetch",
		"mcp-sse-search", "mcp-sse-fetch", "mixed", "sweep", "fixture-invalid-json",
		"fixture-status", "fixture-timeout", "fixture-disconnect":
		return true
	}
	return false
}

func validTargetLabel(target string) bool {
	switch target {
	case "gateway", "isolated-gateway", "worker", "fixture":
		return true
	}
	return false
}

func validPayload(size string) bool {
	return size == "small" || size == "medium" || size == "large"
}
