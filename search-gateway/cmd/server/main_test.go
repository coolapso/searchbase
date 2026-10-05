package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/coolapso/searchbase/search-gateway/internal/middlewares"
	"github.com/coolapso/searchbase/search-gateway/internal/settings"
	"github.com/gin-gonic/gin"
)

func TestConfiguredLoggerFiltersRequests(t *testing.T) {
	for _, level := range []string{"", "error", "warn", "info", "debug", "invalid"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("SEARCHBASE_LOG_LEVEL", level)
			t.Setenv("SEARCHBASE_SEARCH_PROVIDER", "searchbase_ddg")
			t.Setenv("SEARCHBASE_TRACING_ENABLED", "false")
			t.Setenv("SEARCHBASE_MCP_HEARTBEAT_ENABLED", "false")
			s, err := settings.NewSettings()
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.CreateTemp(t.TempDir(), "logs")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			previous := os.Stdout
			os.Stdout = output
			logger, err := newLogger(s)
			os.Stdout = previous
			if level == "invalid" {
				if err == nil {
					t.Fatal("invalid level accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.Use(middlewares.GinSlogger(logger))
			router.GET("/success", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			router.GET("/failure", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
			for _, path := range []string{"/success", "/failure"} {
				router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			}
			if _, err := output.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(output)
			count := 0
			foundError := false
			for decoder.More() {
				var event struct {
					Level string `json:"level"`
					Path  string `json:"path"`
				}
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				count++
				if event.Level == "ERROR" && event.Path == "/failure" {
					foundError = true
				}
			}
			expected := 1
			if level == "info" || level == "debug" {
				expected = 2
			}
			if count != expected || !foundError {
				t.Fatalf("level %q: %d events, error logged=%v", level, count, foundError)
			}
		})
	}
}
