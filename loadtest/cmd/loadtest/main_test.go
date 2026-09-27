package main

import (
	"strings"
	"testing"
	"time"
)

func TestResultDirectoryName(t *testing.T) {
	at := time.Date(2026, 9, 24, 15, 18, 28, 178366035, time.UTC)
	got := resultDirectoryName(at, "worker-javascript", "discover", "crawler-worker-javascript-8g-2cpu")
	want := "20260924-151828-178366035-worker-javascript-discover-crawler-worker-javascript-8g-2cpu"
	if got != want {
		t.Fatalf("directory name = %q, want %q", got, want)
	}
}

func TestResultDirectoryNameSanitizesLabels(t *testing.T) {
	got := resultDirectoryName(time.Unix(0, 0), "../worker", "DISCOVER", "../../secrets / 8 GiB")
	if strings.Contains(got, "..") || strings.Contains(got, "/") || strings.Contains(got, "_") || !strings.HasSuffix(got, "-secrets-8-gib") {
		t.Fatalf("unsafe directory name: %q", got)
	}
}
