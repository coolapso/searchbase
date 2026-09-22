package loadtest

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// ParsePrometheus accepts the Prometheus text exposition format and retains
// numeric samples by metric and label set. Counter resets are detected by the
// caller from successive snapshots.
func ParsePrometheus(in string) (map[string][]Metric, error) {
	out := map[string][]Metric{}
	s := bufio.NewScanner(strings.NewReader(in))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		nameAndLabels, valueText, ok := prometheusValue(line)
		if !ok {
			continue // An unrelated malformed exporter sample must not discard valid metrics.
		}
		name, labels := nameAndLabels, map[string]string{}
		if i := strings.IndexByte(name, '{'); i >= 0 {
			if !strings.HasSuffix(name, "}") {
				return nil, fmt.Errorf("invalid labels")
			}
			raw := name[i+1 : len(name)-1]
			name = name[:i]
			for _, pair := range strings.Split(raw, ",") {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) != 2 {
					continue
				}
				labels[kv[0]] = strings.Trim(kv[1], "\"")
			}
		}
		v, err := strconv.ParseFloat(valueText, 64)
		if err != nil {
			continue
		}
		out[name] = append(out[name], Metric{Labels: labels, Value: v})
	}
	return out, s.Err()
}

// prometheusValue parses from the right so quoted label values may contain
// spaces. The optional timestamp is a long integer in milliseconds.
func prometheusValue(line string) (string, string, bool) {
	head, last, ok := splitLastField(line)
	if !ok {
		return "", "", false
	}
	if _, err := strconv.ParseFloat(last, 64); err != nil {
		return "", "", false
	}
	if len(last) >= 10 && allDigits(last) {
		var value string
		head, value, ok = splitLastField(head)
		if !ok {
			return "", "", false
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", "", false
		}
		return head, value, true
	}
	return head, last, true
}

func splitLastField(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	i := strings.LastIndexAny(value, " \t")
	if i < 1 || i == len(value)-1 {
		return "", "", false
	}
	return strings.TrimSpace(value[:i]), value[i+1:], true
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

type Metric struct {
	Labels map[string]string
	Value  float64
}

func CounterReset(previous, current float64) bool { return current < previous }
