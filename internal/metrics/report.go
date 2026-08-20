package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlatformResult is everything measured for one platform in one run.
// This struct is what gets serialized to JSON (machine-readable) and
// rendered to Markdown (the README results tables).
type PlatformResult struct {
	Platform   string            `json:"platform"`
	RunAt      time.Time         `json:"run_at"`
	Specs      map[string]string `json:"advertised_specs"`
	Caveats    []string          `json:"caveats"`
	FailedStep string            `json:"failed_step,omitempty"` // set + rest partial if a run failed part way

	NodeLoad LoadResult `json:"node_load"`
	RelLoad  LoadResult `json:"rel_load"`

	Traversal1Hop QueryResult `json:"traversal_1_hop"`
	Traversal2Hop QueryResult `json:"traversal_2_hop"`
	Traversal3Hop QueryResult `json:"traversal_3_hop"`
	PointLookup   QueryResult `json:"point_lookup"`
	IndexedLookup QueryResult `json:"indexed_lookup"`
	Aggregation   QueryResult `json:"aggregation"`
	IndexedOn     string      `json:"indexed_on"`

	Mixed []MixedResult `json:"mixed_workload"`

	Footprint map[string]string `json:"footprint"`
}

type LoadResult struct {
	ItemsLoaded    int     `json:"items_loaded"`
	WallSeconds    float64 `json:"wall_seconds"`
	ItemsPerSecond float64 `json:"items_per_second"`
}

type QueryResult struct {
	Iterations int     `json:"iterations"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	MeanMs     float64 `json:"mean_ms"`
	MinMs      float64 `json:"min_ms"`
	MaxMs      float64 `json:"max_ms"`
	Errors     int     `json:"errors"`
}

func FromLatencySet(l *LatencySet, errors int) QueryResult {
	return QueryResult{
		Iterations: l.Count(),
		P50Ms:      l.P50(),
		P95Ms:      l.P95(),
		MeanMs:     l.Mean(),
		MinMs:      l.Min(),
		MaxMs:      l.Max(),
		Errors:     errors,
	}
}

type MixedResult struct {
	Concurrency   int     `json:"concurrency"`
	WriteRatio    float64 `json:"write_ratio"`
	DurationSec   float64 `json:"duration_seconds"`
	TotalOps      int     `json:"total_ops"`
	Errors        int     `json:"errors"`
	QueriesPerSec float64 `json:"queries_per_second"`
}

func WriteJSON(dir string, r PlatformResult) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s.json", r.Platform))
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}

// WriteMarkdownSummary renders the full cross-platform results matrix the
// README needs, from whatever JSON result files are already on disk.
func WriteMarkdownSummary(dir string, results []PlatformResult) (string, error) {
	var b strings.Builder
	b.WriteString("# Benchmark results\n\n")
	b.WriteString(fmt.Sprintf("_Generated %s_\n\n", time.Now().Format(time.RFC3339)))

	b.WriteString("## Data loading\n\n")
	b.WriteString("| Platform | Nodes loaded | Node throughput (n/s) | Rels loaded | Rel throughput (r/s) | Total wall time |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range results {
		total := r.NodeLoad.WallSeconds + r.RelLoad.WallSeconds
		b.WriteString(fmt.Sprintf("| %s | %d | %.1f | %d | %.1f | %.1fs |\n",
			r.Platform, r.NodeLoad.ItemsLoaded, r.NodeLoad.ItemsPerSecond,
			r.RelLoad.ItemsLoaded, r.RelLoad.ItemsPerSecond, total))
	}

	b.WriteString("\n## Traversals (p50 / p95, ms)\n\n")
	b.WriteString("| Platform | 1-hop | 2-hop | 3-hop |\n|---|---|---|---|\n")
	for _, r := range results {
		b.WriteString(fmt.Sprintf("| %s | %.2f / %.2f | %.2f / %.2f | %.2f / %.2f |\n",
			r.Platform,
			r.Traversal1Hop.P50Ms, r.Traversal1Hop.P95Ms,
			r.Traversal2Hop.P50Ms, r.Traversal2Hop.P95Ms,
			r.Traversal3Hop.P50Ms, r.Traversal3Hop.P95Ms))
	}

	b.WriteString("\n## Lookups (p50 / p95, ms)\n\n")
	b.WriteString("| Platform | Point lookup | Indexed lookup | Indexed on |\n|---|---|---|---|\n")
	for _, r := range results {
		b.WriteString(fmt.Sprintf("| %s | %.2f / %.2f | %.2f / %.2f | %s |\n",
			r.Platform, r.PointLookup.P50Ms, r.PointLookup.P95Ms,
			r.IndexedLookup.P50Ms, r.IndexedLookup.P95Ms, r.IndexedOn))
	}

	b.WriteString("\n## Aggregation (p50 / p95, ms)\n\n")
	b.WriteString("| Platform | p50 | p95 |\n|---|---|---|\n")
	for _, r := range results {
		b.WriteString(fmt.Sprintf("| %s | %.2f | %.2f |\n", r.Platform, r.Aggregation.P50Ms, r.Aggregation.P95Ms))
	}

	b.WriteString("\n## Mixed read/write throughput\n\n")
	b.WriteString("| Platform | Concurrency | Write ratio | QPS | Errors |\n|---|---|---|---|---|\n")
	for _, r := range results {
		for _, m := range r.Mixed {
			b.WriteString(fmt.Sprintf("| %s | %d | %.0f%% | %.1f | %d |\n",
				r.Platform, m.Concurrency, m.WriteRatio*100, m.QueriesPerSec, m.Errors))
		}
	}

	b.WriteString("\n## Footprint\n\n")
	b.WriteString("| Platform | Notes |\n|---|---|\n")
	for _, r := range results {
		var parts []string
		for k, v := range r.Footprint {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		b.WriteString(fmt.Sprintf("| %s | %s |\n", r.Platform, strings.Join(parts, "; ")))
	}

	b.WriteString("\n## Caveats\n\n")
	for _, r := range results {
		if len(r.Caveats) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("- **%s**: %s\n", r.Platform, strings.Join(r.Caveats, "; ")))
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "RESULTS.md")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}
