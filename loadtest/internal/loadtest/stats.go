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
	buckets                         map[int]int64
	count                           int64
	success, errors, missed, active int64
	classes                         map[string]int64
}

func NewStats() *Stats {
	return &Stats{buckets: map[int]int64{}, classes: map[string]int64{}}
}
func (s *Stats) Start() {
	s.mu.Lock()
	if s.started.IsZero() {
		s.started = time.Now()
	}
	s.mu.Unlock()
}
func (s *Stats) Begin() {
	s.mu.Lock()
	if s.started.IsZero() {
		s.started = time.Now()
	}
	s.active++
	s.mu.Unlock()
}

const histogramBase = 1.01

func bucketFor(d time.Duration) int {
	ms := math.Max(.001, float64(d)/float64(time.Millisecond))
	return int(math.Ceil(math.Log(ms/.001) / math.Log(histogramBase)))
}
func bucketUpper(index int) float64 { return .001 * math.Pow(histogramBase, float64(index)) }
func (s *Stats) End(d time.Duration, class string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active > 0 {
		s.active--
	}
	s.buckets[bucketFor(d)]++
	s.count++
	if class == "" {
		s.success++
	} else {
		s.errors++
		s.classes[class]++
	}
}
func (s *Stats) Miss()            { s.mu.Lock(); s.missed++; s.mu.Unlock() }
func (s *Stats) Successes() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.success }
func (s *Stats) Histogram() []HistogramBucket {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]int, 0, len(s.buckets))
	for k := range s.buckets {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]HistogramBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, HistogramBucket{UpperBoundMS: bucketUpper(k), Count: s.buckets[k]})
	}
	return out
}
func (s *Stats) Snapshot() (Latency, int64, int64, int64, float64, map[string]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]int, 0, len(s.buckets))
	for k := range s.buckets {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	q := func(p float64) float64 {
		if s.count == 0 {
			return 0
		}
		wanted := int64(math.Ceil(p * float64(s.count)))
		var seen int64
		for _, k := range keys {
			seen += s.buckets[k]
			if seen >= wanted {
				return bucketUpper(k)
			}
		}
		return bucketUpper(keys[len(keys)-1])
	}
	classes := map[string]int64{}
	for k, v := range s.classes {
		classes[k] = v
	}
	elapsed := 0.0
	if !s.started.IsZero() {
		elapsed = time.Since(s.started).Seconds()
	}
	rate := 0.0
	if elapsed > 0 {
		rate = float64(s.success) / elapsed
	}
	return Latency{Count: s.count, P50MS: q(.50), P95MS: q(.95), P99MS: q(.99)}, s.errors, s.missed, s.active, rate, classes
}
