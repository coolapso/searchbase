package loadtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func LoadScenario(dir, name string) (Scenario, error) {
	data, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
	if err != nil {
		return Scenario{}, err
	}
	var scenario Scenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario %q: use JSON-compatible YAML: %w", name, err)
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
	if s.StartRate < 0 {
		return fmt.Errorf("scenario start_rate cannot be negative")
	}
	if s.Mode == "" {
		s.Mode = "open"
	}
	if s.Mode != "open" && s.Mode != "closed" && s.Mode != "idle" {
		return fmt.Errorf("unsupported scenario mode %q", s.Mode)
	}
	if s.Mode == "idle" && s.IdleSessions < 1 {
		return fmt.Errorf("idle scenario requires idle_sessions")
	}
	return nil
}
