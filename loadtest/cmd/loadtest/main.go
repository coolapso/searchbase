package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coolapso/searchbase/loadtest/internal/loadtest"
)

func main() {
	scenarioName := flag.String("scenario", "gateway-rest-search", "scenario file name")
	profile := flag.String("profile", "smoke", "smoke, discover, or soak")
	dir := flag.String("scenarios", "/scenarios", "scenario directory")
	out := flag.String("output", "/results", "result directory")
	duration := flag.Duration("stage-duration", 0, "override each stage duration")
	rate := flag.Float64("rate", 0, "override the scenario request rate (operations/second)")
	instance := flag.String("instance-label", "co-located", "VM/container label")
	comparable := flag.Bool("comparable", false, "mark as remotely generated and comparable")
	flag.Parse()
	s, err := loadtest.LoadScenario(*dir, *scenarioName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	resultDir := filepath.Join(*out, resultDirectoryName(time.Now().UTC(), s.Name, *profile, *instance))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cadvisor := env("LOADTEST_CADVISOR", "http://cadvisor:8080/metrics")
	metrics := map[string]string{"search-gateway": cadvisor, "search-gateway-isolated": cadvisor, "crawl-worker": cadvisor, "fixture": cadvisor, "cadvisor": cadvisor, "node-exporter": cadvisor, "host": env("LOADTEST_NODE_EXPORTER", "http://node-exporter:9100/metrics")}
	if !*comparable {
		metrics["loadtest"] = cadvisor
	}
	r, err := loadtest.Run(ctx, s, loadtest.RunConfig{Profile: *profile, Rate: *rate, StageDuration: *duration, Output: resultDir, InstanceLabel: *instance, Comparable: *comparable, Targets: loadtest.Targets{Gateway: env("LOADTEST_GATEWAY", "http://search-gateway:8080"), IsolatedGateway: env("LOADTEST_ISOLATED_GATEWAY", "http://search-gateway-isolated:8080"), Worker: env("LOADTEST_WORKER", "http://crawl-worker:8000"), Fixture: env("LOADTEST_FIXTURE", "http://fixture:8081")}, Metrics: metrics, DockerMetrics: env("LOADTEST_DOCKER_METRICS", "http://fixture:8081/docker-metrics"), Revision: os.Getenv("LOADTEST_GIT_REVISION")})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	unit := "ops/s"
	if s.Mode == "idle" {
		unit = "sessions"
	}
	fmt.Printf("report: %s/report.json; safe capacity: %.2f %s\n", resultDir, r.SafeCapacity, unit)
}

func resultDirectoryName(at time.Time, scenario, profile, instance string) string {
	return fmt.Sprintf("%s-%09d-%s-%s-%s", at.UTC().Format("20060102-150405"), at.Nanosecond(), directorySlug(scenario, 48), directorySlug(profile, 16), directorySlug(instance, 64))
}

func directorySlug(value string, limit int) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(value) {
		if b.Len() >= limit {
			break
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			separator = false
		} else if b.Len() > 0 && !separator {
			b.WriteByte('-')
			separator = true
		}
	}
	if slug := strings.Trim(b.String(), "-"); slug != "" {
		return slug
	}
	return "unknown"
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
