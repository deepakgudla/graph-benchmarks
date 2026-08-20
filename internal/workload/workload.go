package workload

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deepakgudla/graph-benchmark/internal/config"
	"github.com/deepakgudla/graph-benchmark/internal/dbclient"
	"github.com/deepakgudla/graph-benchmark/internal/metrics"
)

// Run executes the full required benchmark suite (5.2 in the assignment)
// against a single already-constructed client, and returns a populated
// PlatformResult. It's intentionally platform-agnostic: everything here
// goes through the dbclient.Client interface.
func Run(ctx context.Context, client dbclient.Client, nodes []dbclient.Node, edges []dbclient.Relationship, wc config.RunConfig) metrics.PlatformResult {
	result := metrics.PlatformResult{
		Platform:  client.Name(),
		RunAt:     time.Now(),
		IndexedOn: "region",
	}
	addCaveat := func(format string, args ...any) {
		result.Caveats = append(result.Caveats, fmt.Sprintf(format, args...))
	}

	if err := client.Connect(ctx); err != nil {
		result.FailedStep = "connect"
		addCaveat("connection failed: %v", err)
		return result
	}
	defer client.Close(ctx)

	if err := client.EnsureConstraintsAndIndexes(ctx); err != nil {
		addCaveat("index setup failed (queries below ran without the intended index): %v", err)
	}

	// --- Data loading ---
	nodeStats, err := client.LoadNodes(ctx, nodes, wc.Workload.BatchSize)
	if err != nil {
		result.FailedStep = "load_nodes"
		addCaveat("node load failed: %v", err)
		return result
	}
	result.NodeLoad = metrics.LoadResult{ItemsLoaded: nodeStats.ItemsLoaded, WallSeconds: nodeStats.Wall.Seconds(), ItemsPerSecond: nodeStats.PerSecond()}

	relStats, err := client.LoadRelationships(ctx, edges, wc.Workload.BatchSize)
	if err != nil {
		result.FailedStep = "load_relationships"
		addCaveat("relationship load failed: %v", err)
		return result
	}
	result.RelLoad = metrics.LoadResult{ItemsLoaded: relStats.ItemsLoaded, WallSeconds: relStats.Wall.Seconds(), ItemsPerSecond: relStats.PerSecond()}

	rng := rand.New(rand.NewSource(7))
	sampleIDs := sampleNodeIDs(nodes, rng, 500)
	sampleRegions := []string{"north", "south", "east", "west", "central"}

	// --- Warm-up (discarded) ---
	for i := 0; i < wc.Workload.WarmupIterations; i++ {
		id := sampleIDs[i%len(sampleIDs)]
		_, _ = client.Traversal(ctx, id, 1)
		_, _ = client.PointLookup(ctx, id)
		_, _ = client.IndexedLookup(ctx, sampleRegions[i%len(sampleRegions)])
		_, _ = client.Aggregation(ctx)
	}

	// --- Traversals ---
	result.Traversal1Hop = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.Traversal(ctx, sampleIDs[i%len(sampleIDs)], 1)
	})
	result.Traversal2Hop = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.Traversal(ctx, sampleIDs[i%len(sampleIDs)], 2)
	})
	result.Traversal3Hop = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.Traversal(ctx, sampleIDs[i%len(sampleIDs)], 3)
	})

	// --- Lookups ---
	result.PointLookup = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.PointLookup(ctx, sampleIDs[i%len(sampleIDs)])
	})
	result.IndexedLookup = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.IndexedLookup(ctx, sampleRegions[i%len(sampleRegions)])
	})

	// --- Aggregation ---
	result.Aggregation = measure(ctx, wc.Workload.ReadIterations, func(i int) (time.Duration, error) {
		return client.Aggregation(ctx)
	})

	// --- Mixed read/write, swept across concurrency levels ---
	for _, c := range wc.Workload.MixedConcurrencies {
		mr := runMixed(ctx, client, c, wc.Workload.MixedWriteRatio, time.Duration(wc.Workload.MixedDurationSeconds)*time.Second)
		result.Mixed = append(result.Mixed, mr)
	}

	// --- Footprint ---
	if fp, err := client.Footprint(ctx); err == nil {
		result.Footprint = fp
	} else {
		addCaveat("footprint collection failed: %v", err)
	}

	return result
}

func sampleNodeIDs(nodes []dbclient.Node, rng *rand.Rand, n int) []string {
	if n > len(nodes) {
		n = len(nodes)
	}
	idx := rng.Perm(len(nodes))[:n]
	ids := make([]string, 0, n)
	for _, i := range idx {
		ids = append(ids, nodes[i].ID)
	}
	return ids
}

// measure runs fn `iterations` times and folds successful latencies into a
// LatencySet, counting (but not aborting on) errors - a single flaky
// iteration shouldn't kill the whole benchmark run.
func measure(ctx context.Context, iterations int, fn func(i int) (time.Duration, error)) metrics.QueryResult {
	ls := metrics.NewLatencySet()
	errs := 0
	for i := 0; i < iterations; i++ {
		d, err := fn(i)
		if err != nil {
			errs++
			continue
		}
		ls.Add(d)
	}
	return metrics.FromLatencySet(ls, errs)
}

// runMixed drives `concurrency` goroutines issuing read/write ops for
// `duration`, at the given write ratio, and reports sustained QPS - the
// "concurrent read/write throughput" metric required by the assignment.
func runMixed(ctx context.Context, client dbclient.Client, concurrency int, writeRatio float64, duration time.Duration) metrics.MixedResult {
	var totalOps int64
	var errCount int64
	var seq int64

	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerSeed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(workerSeed))
			for {
				select {
				case <-runCtx.Done():
					return
				default:
				}
				var err error
				if rng.Float64() < writeRatio {
					n := atomic.AddInt64(&seq, 1)
					err = client.WriteOp(runCtx, int(n))
				} else {
					err = client.ReadOp(runCtx)
				}
				atomic.AddInt64(&totalOps, 1)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
				}
			}
		}(int64(w) + time.Now().UnixNano())
	}
	start := time.Now()
	wg.Wait()
	elapsed := time.Since(start)

	return metrics.MixedResult{
		Concurrency:   concurrency,
		WriteRatio:    writeRatio,
		DurationSec:   elapsed.Seconds(),
		TotalOps:      int(totalOps),
		Errors:        int(errCount),
		QueriesPerSec: metrics.Throughput(int(totalOps), elapsed),
	}
}
