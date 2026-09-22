package loadtest

import (
	"math"
	"sort"
	"sync"
	"time"
)

type Stats struct {
	mu                              sync.Mutex
	started                         time.Time
	latencies                       []float64
	success, errors, missed, active int64
	classes                         map[string]int64
}

func NewStats() *Stats  { return &Stats{started: time.Now(), classes: map[string]int64{}} }
func (s *Stats) Begin() { s.mu.Lock(); s.active++; s.mu.Unlock() }
func (s *Stats) End(d time.Duration, class string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	s.latencies = append(s.latencies, float64(d)/float64(time.Millisecond))
	if class == "" {
		s.success++
	} else {
		s.errors++
		s.classes[class]++
	}
}
func (s *Stats) Miss() { s.mu.Lock(); s.missed++; s.mu.Unlock() }
func (s *Stats) Snapshot() (Latency, int64, int64, int64, float64, map[string]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := append([]float64(nil), s.latencies...)
	sort.Float64s(l)
	q := func(p float64) float64 {
		if len(l) == 0 {
			return 0
		}
		return l[int(math.Ceil(p*float64(len(l))))-1]
	}
	classes := map[string]int64{}
	for k, v := range s.classes {
		classes[k] = v
	}
	elapsed := time.Since(s.started).Seconds()
	rate := 0.0
	if elapsed > 0 {
		rate = float64(s.success) / elapsed
	}
	return Latency{Count: int64(len(l)), P50MS: q(.50), P95MS: q(.95), P99MS: q(.99)}, s.errors, s.missed, s.active, rate, classes
}
