package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNewSettings_Defaults(t *testing.T) {
	s, err := NewSettings()
	if err != nil {
		t.Fatalf("Failed to create settings: %v", err)
	}

	if s.Port() != "808int" && s.Port() != "8080" {
		t.Errorf("Expected default port 8080 or 8088, got %s", s.Port())
	}

	if s.Port() != "8080" {
		t.Errorf("Expected default port 8080, got %s", s.Port())
	}

	if s.CrawlWorkerAddress() != "http://localhost:8000" {
		t.Errorf("Expected default crawl worker address http://localhost:8000, got %s", s.CrawlWorkerAddress())
	}

	if s.GinMode() != gin.ReleaseMode {
		t.Errorf("Expected default gin mode %s, got %s", gin.ReleaseMode, s.GinMode())
	}

	if s.LogLevel() != "error" {
		t.Errorf("Expected default log level error, got %s", s.LogLevel())
	}

	if s.SearchProvider().Name() != "searchbase_ddg" {
		t.Errorf("Expected default search provider searchbase_ddg, got %s", s.SearchProvider().Name())
	}

	if s.Mcp().HeartbeatEnabled() {
		t.Error("Expected mcp heartbeat to be disabled by default")
	}

	if s.Mcp().HeartbeatInterval() != 60*time.Second {
		t.Errorf("Expected default mcp heartbeat interval 60s, got %s", s.Mcp().HeartbeatInterval())
	}
}

func TestNewSettings_EnvVars(t *testing.T) {
	t.Setenv("SEARCHBASE_PORT", "9090")
	t.Setenv("SEARCHBASE_CRAWL_WORKER_ADDRESS", "http://remote:8001")
	t.Setenv("SEARCHBASE_LOG_LEVEL", "debug")
	t.Setenv("SEARCHBASE_ENVIRONMENT", "dev")
	t.Setenv("SEARCHBASE_SEARCH_PROVIDER", "ddgs")
	t.Setenv("SEARCHBASE_DDGS_PROVIDER_ADDRESS", "http://ddgs-service:8002")
	t.Setenv("SEARCHBASE_MCP_HEARTBEAT_ENABLED", "true")
	t.Setenv("SEARCHBASE_MCP_HEARTBEAT_INTERVAL", "45")

	s, err := NewSettings()
	if err != nil {
		t.Fatalf("Failed to create settings: %v", err)
	}

	if s.Port() != "9090" {
		t.Errorf("Expected port 9090, got %s", s.Port())
	}

	if s.CrawlWorkerAddress() != "http://remote:8001" {
		t.Errorf("Expected crawl worker address http://remote:8001, got %s", s.CrawlWorkerAddress())
	}

	if s.LogLevel() != "debug" {
		t.Errorf("Expected log level debug, got %s", s.LogLevel())
	}

	if s.GinMode() != gin.DebugMode {
		t.Errorf("Expected gin mode %s, got %s", gin.DebugMode, s.GinMode())
	}

	if s.SearchProvider().Name() != "ddgs" {
		t.Errorf("Expected search provider ddgs, got %s", s.SearchProvider().Name())
	}

	if s.SearchProvider().Address() != "http://ddgs-service:8002" {
		t.Errorf("Expected ddgs address http://ddgs-service:8002, got %s", s.SearchProvider().Address())
	}

	if !s.Mcp().HeartbeatEnabled() {
		t.Error("Expected mcp heartbeat to be enabled")
	}

	if s.Mcp().HeartbeatInterval() != 45*time.Second {
		t.Errorf("Expected mcp heartbeat interval 45s, got %s", s.Mcp().HeartbeatInterval())
	}
}

func TestNewSettings_SearchProviderValidationErrors(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		expectedError string
	}{
		{
			name:          "ddgs missing address",
			provider:      "ddgs",
			expectedError: "provider address not set: searchbase provider ddgs requires SEARCHBASE_DDGS_PROVIDER_ADDRESS",
		},
		{
			name:          "searxng missing address",
			provider:      "searxng",
			expectedError: "provider address not set: searchbase provider searxng requires SEARCHBASE_SEARXNG_PROVIDER_ADDRESS",
		},
		{
			name:          "brave missing token",
			provider:      "brave",
			expectedError: "provider API Token not set: searchbase provider brave requires SEARCHBASE_BRAVE_API_TOKEN",
		},
		{
			name:          "mojeek missing token",
			provider:      "mojeek",
			expectedError: "provider API Token not set: searchbase provider mojeek requires SEARCHBASE_MOJEEK_API_KEY",
		},
		{
			name:          "unsupported provider",
			provider:      "unknown",
			expectedError: "unsupported search provider: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SEARCHBASE_SEARCH_PROVIDER", tt.provider)

			_, err := NewSettings()
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if err.Error() != tt.expectedError {
				t.Fatalf("Expected error %q, got %q", tt.expectedError, err.Error())
			}

			if strings.Contains(err.Error(), "SEARCHBASE_") && strings.HasSuffix(err.Error(), "SEARCHBASE_") {
				t.Fatalf("Error has incomplete environment variable name: %v", err)
			}
		})
	}
}

func TestNewSettings_McpHeartbeatValidation(t *testing.T) {
	tests := []struct {
		name          string
		enabled       string
		interval      string
		expectedError string
	}{
		{
			name:          "interval below minimum while enabled",
			enabled:       "true",
			interval:      "1",
			expectedError: "mcp heartbeat interval too low: SEARCHBASE_MCP_HEARTBEAT_INTERVAL must be >= 15s, got 1s",
		},
		{
			name:          "interval just below minimum while enabled",
			enabled:       "true",
			interval:      "14",
			expectedError: "mcp heartbeat interval too low: SEARCHBASE_MCP_HEARTBEAT_INTERVAL must be >= 15s, got 14s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SEARCHBASE_MCP_HEARTBEAT_ENABLED", tt.enabled)
			t.Setenv("SEARCHBASE_MCP_HEARTBEAT_INTERVAL", tt.interval)

			_, err := NewSettings()
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if err.Error() != tt.expectedError {
				t.Fatalf("Expected error %q, got %q", tt.expectedError, err.Error())
			}
		})
	}
}

func TestNewSettings_McpHeartbeatAcceptedIntervals(t *testing.T) {
	tests := []struct {
		name             string
		enabled          string
		interval         string
		expectedEnabled  bool
		expectedInterval time.Duration
	}{
		{
			name:             "interval at the minimum while enabled",
			enabled:          "true",
			interval:         "15",
			expectedEnabled:  true,
			expectedInterval: 15 * time.Second,
		},
		{
			name:             "enabled without an interval falls back to the default",
			enabled:          "true",
			interval:         "",
			expectedEnabled:  true,
			expectedInterval: 60 * time.Second,
		},
		{
			name:             "interval below minimum is ignored while disabled",
			enabled:          "false",
			interval:         "1",
			expectedEnabled:  false,
			expectedInterval: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SEARCHBASE_MCP_HEARTBEAT_ENABLED", tt.enabled)
			if tt.interval != "" {
				t.Setenv("SEARCHBASE_MCP_HEARTBEAT_INTERVAL", tt.interval)
			}

			s, err := NewSettings()
			if err != nil {
				t.Fatalf("Failed to create settings: %v", err)
			}

			if s.Mcp().HeartbeatEnabled() != tt.expectedEnabled {
				t.Errorf("Expected mcp heartbeat enabled %t, got %t", tt.expectedEnabled, s.Mcp().HeartbeatEnabled())
			}

			if s.Mcp().HeartbeatInterval() != tt.expectedInterval {
				t.Errorf("Expected mcp heartbeat interval %s, got %s", tt.expectedInterval, s.Mcp().HeartbeatInterval())
			}
		})
	}
}
