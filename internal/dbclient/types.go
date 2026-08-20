package dbclient

import (
	"context"
	"time"
)

// Node is a minimal graph node: an ID plus a small set of synthetic
// properties used for point/indexed lookups and aggregations.
type Node struct {
	ID     string
	Region string // low-cardinality property, indexed -> used for "indexed lookup"
	Age    int    // used for aggregation (group-by / count)
}

// Relationship is a directed edge between two node IDs.
type Relationship struct {
	Source string
	Target string
	Type   string
}

// LoadStats captures ingest throughput for a load phase.
type LoadStats struct {
	ItemsLoaded int
	Wall        time.Duration
}

func (l LoadStats) PerSecond() float64 {
	if l.Wall <= 0 {
		return 0
	}
	return float64(l.ItemsLoaded) / l.Wall.Seconds()
}

// Client is the interface every platform driver must satisfy. Keeping the
// surface identical across platforms is what makes the benchmark fair: the
// workload package never needs to know which database it's talking to.
type Client interface {
	// Name is the human-readable platform name used in reports.
	Name() string

	// Connect opens the connection/session pool. Should fail fast if the
	// platform is unreachable so the harness can report it as a failed run.
	Connect(ctx context.Context) error
	Close(ctx context.Context) error

	// Reset wipes all benchmark data (nodes/relationships or documents/edges)
	// from the platform, so a subsequent LoadNodes/LoadRelationships starts
	// from an empty graph instead of accumulating duplicates across runs.
	// Safe to call on an already-empty instance.
	Reset(ctx context.Context) error

	// Schema / indexing. Each platform documents which property it indexed.
	EnsureConstraintsAndIndexes(ctx context.Context) error

	// Ingest
	LoadNodes(ctx context.Context, nodes []Node, batchSize int) (LoadStats, error)
	LoadRelationships(ctx context.Context, rels []Relationship, batchSize int) (LoadStats, error)

	// Read workloads - each call executes exactly one logical query and
	// returns how long it took. The workload package handles repetition,
	// warm-up, and percentile aggregation.
	Traversal(ctx context.Context, startNodeID string, hops int) (time.Duration, error)
	PointLookup(ctx context.Context, nodeID string) (time.Duration, error)
	IndexedLookup(ctx context.Context, region string) (time.Duration, error)
	Aggregation(ctx context.Context) (time.Duration, error)

	// Mixed workload primitives, used under concurrency by the workload
	// package. WriteOp should be cheap and idempotent-ish (e.g. a property
	// bump or a tiny node insert) so repeated runs don't blow past free-tier
	// storage limits.
	ReadOp(ctx context.Context) error
	WriteOp(ctx context.Context, seq int) error

	// Footprint reports whatever the platform exposes (stored size, memory,
	// instance spec). Return "not observable" values rather than erroring
	// when a platform doesn't expose this.
	Footprint(ctx context.Context) (map[string]string, error)
}
