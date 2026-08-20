package metrics

import (
	"sort"
	"time"
)

// LatencySet holds repeated measurements of one logical query type and
// computes the percentiles the assignment asks for (p50 / p95), not just
// an average, which hides tail latency.
type LatencySet struct {
	samples []time.Duration
}

func NewLatencySet() *LatencySet { return &LatencySet{} }

func (l *LatencySet) Add(d time.Duration) { l.samples = append(l.samples, d) }

func (l *LatencySet) Count() int { return len(l.samples) }

// Percentile returns the requested percentile (0-100) in milliseconds.
// Uses nearest-rank on a sorted copy - simple and adequate for >=100 samples.
func (l *LatencySet) Percentile(p float64) float64 {
	if len(l.samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(l.samples))
	copy(sorted, l.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	rank := int(p/100*float64(len(sorted)-1) + 0.5)
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return float64(sorted[rank]) / float64(time.Millisecond)
}

func (l *LatencySet) P50() float64 { return l.Percentile(50) }
func (l *LatencySet) P95() float64 { return l.Percentile(95) }

func (l *LatencySet) Mean() float64 {
	if len(l.samples) == 0 {
		return 0
	}
	var sum time.Duration
	for _, s := range l.samples {
		sum += s
	}
	return float64(sum) / float64(len(l.samples)) / float64(time.Millisecond)
}

func (l *LatencySet) Min() float64 {
	if len(l.samples) == 0 {
		return 0
	}
	m := l.samples[0]
	for _, s := range l.samples {
		if s < m {
			m = s
		}
	}
	return float64(m) / float64(time.Millisecond)
}

func (l *LatencySet) Max() float64 {
	if len(l.samples) == 0 {
		return 0
	}
	m := l.samples[0]
	for _, s := range l.samples {
		if s > m {
			m = s
		}
	}
	return float64(m) / float64(time.Millisecond)
}

// Throughput computes ops/sec given a count and wall-clock duration.
func Throughput(count int, wall time.Duration) float64 {
	if wall <= 0 {
		return 0
	}
	return float64(count) / wall.Seconds()
}
