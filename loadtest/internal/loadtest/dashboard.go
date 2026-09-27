package loadtest

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Dashboard follows the sibling Cloud harness's aggregate-only terminal view.
// It never renders fixture inputs, request URLs, or response data.
type Dashboard struct {
	output   io.Writer
	terminal bool
	history  []Sample
}

func NewDashboard(output io.Writer) *Dashboard {
	terminal := false
	if file, ok := output.(*os.File); ok {
		if info, err := file.Stat(); err == nil {
			terminal = info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
		}
	}
	return &Dashboard{output: output, terminal: terminal}
}

func (d *Dashboard) Update(phase string, target float64, sample Sample) {
	d.history = append(d.history, sample)
	if len(d.history) > 32 {
		d.history = d.history[len(d.history)-32:]
	}
	if !d.terminal {
		fmt.Fprintf(d.output, "%s target=%.2f achieved=%.2f active=%d p95=%.1fms errors=%d missed=%d\n", phase, target, sample.AchievedRate, sample.Active, sample.Latency.P95MS, sample.Errors, sample.Missed)
		return
	}
	fmt.Fprint(d.output, "\033[H\033[2J")
	fmt.Fprintf(d.output, "Searchbase load test — %s\n\n", phase)
	fmt.Fprintf(d.output, "Rate    target %8.2f/s  achieved %8.2f/s  %s\n", target, sample.AchievedRate, sparkline(d.history, func(s Sample) float64 { return s.AchievedRate }))
	fmt.Fprintf(d.output, "Latency p50 %8.1f ms  p95 %8.1f ms  p99 %8.1f ms  %s\n", sample.Latency.P50MS, sample.Latency.P95MS, sample.Latency.P99MS, sparkline(d.history, func(s Sample) float64 { return s.Latency.P95MS }))
	fmt.Fprintf(d.output, "Active %d  errors %d  missed %d\n", sample.Active, sample.Errors, sample.Missed)
	if sample.ObserverHealth != "" {
		fmt.Fprintln(d.output, "Observer unavailable; run will be invalid for sizing")
	}
	names := make([]string, 0, len(sample.Components))
	for name := range sample.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(d.output, "\nResources")
	for _, name := range names {
		c := sample.Components[name]
		fmt.Fprintf(d.output, "%-17s CPU %5.1f%%  memory %8.1f MiB  limit %8.1f MiB\n", name, c.CPUUtilization*100, c.WorkingSetBytes/(1024*1024), c.LimitBytes/(1024*1024))
	}
}

func sparkline(history []Sample, value func(Sample) float64) string {
	const glyphs = "▁▂▃▄▅▆▇█"
	runes := []rune(glyphs)
	max := 0.0
	for _, sample := range history {
		if v := value(sample); v > max {
			max = v
		}
	}
	if max == 0 {
		return strings.Repeat(string(runes[0]), len(history))
	}
	var b strings.Builder
	for _, sample := range history {
		index := int(value(sample) / max * float64(len(runes)-1))
		if index < 0 {
			index = 0
		}
		if index >= len(runes) {
			index = len(runes) - 1
		}
		b.WriteRune(runes[index])
	}
	return b.String()
}
