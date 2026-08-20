package dbclient

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// queryTimeout bounds any single query. Without this, a platform-specific
// failure (e.g. an unindexed full label scan) hangs silently for as long as
// the platform lets it, instead of failing loudly and getting recorded as
// an error - which is what actually happened running this benchmark against
// Memgraph before EnsureConstraintsAndIndexes had a Memgraph-compatible
// fallback.
const queryTimeout = 30 * time.Second

// loadBatchTimeout is more generous than queryTimeout: burstable free-tier
// instances (e.g. CognoDB's 0.5 vCPU tier) can run fast for a while and then
// throttle hard once their burst CPU credits are spent partway through a
// sustained write load - a real, observed failure mode, not a hypothetical
// one. A short timeout here would misreport "the platform is broken" when
// the accurate finding is "the platform throttles under sustained write
// load," which is exactly the kind of thing worth measuring, not hiding.
const loadBatchTimeout = 3 * time.Minute

// maxLoadRetries: retry a timed-out load batch a few times with backoff
// before giving up. Throttling is often transient (credits regenerate),
// so one slow batch shouldn't fail the whole load.
const maxLoadRetries = 3

// runLoadBatch executes fn (a single UNWIND-based load batch) with a
// generous timeout and retries on failure, so transient throttling on a
// burstable free-tier instance doesn't kill an otherwise-successful load.
func runLoadBatch(ctx context.Context, fn func(qctx context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= maxLoadRetries; attempt++ {
		qctx, cancel := context.WithTimeout(ctx, loadBatchTimeout)
		err := fn(qctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxLoadRetries {
			backoff := time.Duration(attempt) * 5 * time.Second
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", maxLoadRetries, lastErr)
}

// CypherClient talks to any Bolt-protocol, Cypher-speaking platform via the
// official Neo4j Go driver. Per the assignment's setup instructions this is
// exactly what CognoDB Cloud expects (bolt+s://... , user "cognodb"), and it
// is also what Neo4j AuraDB and Memgraph Cloud speak - so the *same* code
// path and the *same* query text run against all three, which is the
// cleanest way to satisfy "same logical queries on every platform".
type CypherClient struct {
	platformName string
	uri          string
	user         string
	password     string
	database     string

	driver neo4j.DriverWithContext
}

func NewCypherClient(platformName, uri, user, password, database string) *CypherClient {
	return &CypherClient{
		platformName: platformName,
		uri:          uri,
		user:         user,
		password:     password,
		database:     database,
	}
}

func (c *CypherClient) Name() string { return c.platformName }

func (c *CypherClient) Connect(ctx context.Context) error {
	drv, err := neo4j.NewDriverWithContext(c.uri, neo4j.BasicAuth(c.user, c.password, ""))
	if err != nil {
		return fmt.Errorf("%s: creating driver: %w", c.platformName, err)
	}
	if err := drv.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("%s: connectivity check failed: %w", c.platformName, err)
	}
	c.driver = drv
	return nil
}

func (c *CypherClient) Close(ctx context.Context) error {
	if c.driver == nil {
		return nil
	}
	return c.driver.Close(ctx)
}

func (c *CypherClient) session(ctx context.Context, mode neo4j.AccessMode) neo4j.SessionWithContext {
	cfg := neo4j.SessionConfig{AccessMode: mode}
	if c.database != "" {
		cfg.DatabaseName = c.database
	}
	return c.driver.NewSession(ctx, cfg)
}

// runAndTime executes one query and returns how long it took to *fully*
// complete - not just how long it took the server to acknowledge the RUN
// message. sess.Run() alone only guarantees the initial acknowledgement;
// without an explicit Consume(), a benchmark can under-report latency
// (especially for write queries with no RETURN, or aggregates that must
// finish scanning before producing their one result row). Consume() blocks
// until the query has genuinely finished and all records are drained.
func (c *CypherClient) runAndTime(ctx context.Context, mode neo4j.AccessMode, cypher string, params map[string]any) (time.Duration, error) {
	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	sess := c.session(qctx, mode)
	defer sess.Close(qctx)

	t0 := time.Now()
	result, err := sess.Run(qctx, cypher, params)
	if err != nil {
		return 0, err
	}
	if _, err := result.Consume(qctx); err != nil {
		return 0, err
	}
	return time.Since(t0), nil
}

func (c *CypherClient) Reset(ctx context.Context) error {
	sess := c.session(ctx, neo4j.AccessModeWrite)
	defer sess.Close(ctx)

	const batchSize = 2000
	const maxIterations = 5000 // safety valve - ~10M nodes worth of batches
	for i := 0; i < maxIterations; i++ {
		var deletedCount int64
		err := runLoadBatch(ctx, func(qctx context.Context) error {
			result, err := sess.Run(qctx,
				`MATCH (n) WITH n LIMIT $batch DETACH DELETE n RETURN count(n) AS deleted`,
				map[string]any{"batch": batchSize})
			if err != nil {
				return err
			}
			rec, err := result.Single(qctx)
			if err != nil {
				return err
			}
			deleted, _ := rec.Get("deleted")
			deletedCount, _ = deleted.(int64)
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: reset batch %d: %w", c.platformName, i, err)
		}
		if deletedCount == 0 {
			return nil // fully clean
		}
	}
	return fmt.Errorf("%s: reset did not converge after %d batches - graph may be larger than expected", c.platformName, maxIterations)
}

func (c *CypherClient) EnsureConstraintsAndIndexes(ctx context.Context) error {
	sess := c.session(ctx, neo4j.AccessModeWrite)
	defer sess.Close(ctx)

	// Neo4j 5.x / CognoDB / Aura style - idempotent thanks to IF NOT EXISTS.
	neo4jStmts := []string{
		`CREATE CONSTRAINT bench_person_id IF NOT EXISTS FOR (p:Person) REQUIRE p.id IS UNIQUE`,
		`CREATE INDEX bench_person_region IF NOT EXISTS FOR (p:Person) ON (p.region)`,
	}
	// Memgraph's Cypher dialect uses older/simpler constraint+index syntax
	// with no IF NOT EXISTS support - if the Neo4j-style statements above
	// fail, fall back to this instead of silently leaving the graph
	// unindexed (which turns every relationship load into a full label
	// scan and is what made an early run against Memgraph take over an hour).
	memgraphStmts := []string{
		`CREATE CONSTRAINT ON (p:Person) ASSERT p.id IS UNIQUE`,
		`CREATE INDEX ON :Person(region)`,
	}

	runAll := func(stmts []string) error {
		for _, s := range stmts {
			qctx, cancel := context.WithTimeout(ctx, queryTimeout)
			result, err := sess.Run(qctx, s, nil)
			if err == nil {
				_, err = result.Consume(qctx)
			}
			cancel()
			if err != nil {
				return fmt.Errorf("%q: %w", s, err)
			}
		}
		return nil
	}

	if err := runAll(neo4jStmts); err != nil {
		if err2 := runAll(memgraphStmts); err2 != nil {
			// "already exists" on a second run is expected and harmless -
			// Memgraph's CREATE CONSTRAINT/INDEX has no IF NOT EXISTS clause.
			if !strings.Contains(strings.ToLower(err2.Error()), "already exists") {
				return fmt.Errorf("%s: index setup failed under both Neo4j syntax (%v) and Memgraph syntax: %w", c.platformName, err, err2)
			}
		}
	}
	return nil
}

func (c *CypherClient) LoadNodes(ctx context.Context, nodes []Node, batchSize int) (LoadStats, error) {
	start := time.Now()
	sess := c.session(ctx, neo4j.AccessModeWrite)
	defer sess.Close(ctx)

	for i := 0; i < len(nodes); i += batchSize {
		end := min(i+batchSize, len(nodes))
		batch := make([]map[string]any, 0, end-i)
		for _, n := range nodes[i:end] {
			batch = append(batch, map[string]any{"id": n.ID, "region": n.Region, "age": n.Age})
		}

		err := runLoadBatch(ctx, func(qctx context.Context) error {
			result, err := sess.Run(qctx,
				// MERGE (not CREATE) matters here: a batch that actually
				// committed server-side but whose acknowledgment arrived
				// after our client-side deadline gets retried by
				// runLoadBatch. CREATE would then collide with the
				// uniqueness constraint on a node that already exists from
				// the "failed" first attempt; MERGE makes the retry a safe
				// no-op instead.
				`UNWIND $rows AS row
				 MERGE (p:Person {id: row.id})
				 ON CREATE SET p.region = row.region, p.age = row.age`,
				map[string]any{"rows": batch})
			if err != nil {
				return err
			}
			_, err = result.Consume(qctx) // ensure this batch is actually committed before starting the next
			return err
		})
		if err != nil {
			return LoadStats{}, fmt.Errorf("%s: loading node batch at %d: %w", c.platformName, i, err)
		}
	}
	return LoadStats{ItemsLoaded: len(nodes), Wall: time.Since(start)}, nil
}

func (c *CypherClient) LoadRelationships(ctx context.Context, rels []Relationship, batchSize int) (LoadStats, error) {
	start := time.Now()
	sess := c.session(ctx, neo4j.AccessModeWrite)
	defer sess.Close(ctx)

	for i := 0; i < len(rels); i += batchSize {
		end := min(i+batchSize, len(rels))
		batch := make([]map[string]any, 0, end-i)
		for _, r := range rels[i:end] {
			batch = append(batch, map[string]any{"src": r.Source, "dst": r.Target})
		}

		err := runLoadBatch(ctx, func(qctx context.Context) error {
			result, err := sess.Run(qctx,
				// MERGE for the same reason as LoadNodes: a retried batch
				// must be a safe no-op, not a duplicate edge. There's no
				// uniqueness constraint on relationships to catch this
				// loudly, so with CREATE it would fail silently and inflate
				// relationship counts / traversal results instead.
				`UNWIND $rows AS row
				 MATCH (a:Person {id: row.src}), (b:Person {id: row.dst})
				 MERGE (a)-[:KNOWS]->(b)`,
				map[string]any{"rows": batch})
			if err != nil {
				return err
			}
			_, err = result.Consume(qctx)
			return err
		})
		if err != nil {
			return LoadStats{}, fmt.Errorf("%s: loading rel batch at %d: %w", c.platformName, i, err)
		}
	}
	return LoadStats{ItemsLoaded: len(rels), Wall: time.Since(start)}, nil
}

func (c *CypherClient) Traversal(ctx context.Context, startNodeID string, hops int) (time.Duration, error) {
	var q string
	switch hops {
	case 1:
		q = `MATCH (p:Person {id:$id})-[:KNOWS]->(n) RETURN count(n) AS c`
	case 2:
		q = `MATCH (p:Person {id:$id})-[:KNOWS]->()-[:KNOWS]->(n) RETURN count(n) AS c`
	case 3:
		q = `MATCH (p:Person {id:$id})-[:KNOWS]->()-[:KNOWS]->()-[:KNOWS]->(n) RETURN count(n) AS c`
	default:
		return 0, fmt.Errorf("unsupported hop depth %d", hops)
	}
	d, err := c.runAndTime(ctx, neo4j.AccessModeRead, q, map[string]any{"id": startNodeID})
	if err != nil {
		return 0, fmt.Errorf("%s: traversal(%d): %w", c.platformName, hops, err)
	}
	return d, nil
}

func (c *CypherClient) PointLookup(ctx context.Context, nodeID string) (time.Duration, error) {
	d, err := c.runAndTime(ctx, neo4j.AccessModeRead,
		`MATCH (p:Person {id:$id}) RETURN p.id, p.region, p.age`, map[string]any{"id": nodeID})
	if err != nil {
		return 0, fmt.Errorf("%s: point lookup: %w", c.platformName, err)
	}
	return d, nil
}

func (c *CypherClient) IndexedLookup(ctx context.Context, region string) (time.Duration, error) {
	d, err := c.runAndTime(ctx, neo4j.AccessModeRead,
		`MATCH (p:Person {region:$region}) RETURN p.id LIMIT 50`, map[string]any{"region": region})
	if err != nil {
		return 0, fmt.Errorf("%s: indexed lookup: %w", c.platformName, err)
	}
	return d, nil
}

func (c *CypherClient) Aggregation(ctx context.Context) (time.Duration, error) {
	d, err := c.runAndTime(ctx, neo4j.AccessModeRead,
		`MATCH (p:Person) RETURN p.region AS region, count(*) AS c ORDER BY c DESC`, nil)
	if err != nil {
		return 0, fmt.Errorf("%s: aggregation: %w", c.platformName, err)
	}
	return d, nil
}

func (c *CypherClient) ReadOp(ctx context.Context) error {
	// rand.Intn (package-level) is internally lock-protected and safe to
	// call from many goroutines at once, unlike a private *rand.Rand -
	// this matters here because ReadOp is called concurrently by up to
	// dozens of goroutines in the mixed workload.
	id := fmt.Sprintf("n%d", rand.Intn(500000))
	_, err := c.runAndTime(ctx, neo4j.AccessModeRead, `MATCH (p:Person {id:$id}) RETURN p.id`, map[string]any{"id": id})
	return err
}

func (c *CypherClient) WriteOp(ctx context.Context, seq int) error {
	// Bump a counter property rather than growing the graph unboundedly -
	// keeps repeated benchmark runs from exhausting free-tier storage.
	id := fmt.Sprintf("n%d", rand.Intn(500000))
	_, err := c.runAndTime(ctx, neo4j.AccessModeWrite,
		`MATCH (p:Person {id:$id}) SET p.writeCounter = coalesce(p.writeCounter, 0) + 1`,
		map[string]any{"id": id})
	return err
}

func (c *CypherClient) Footprint(ctx context.Context) (map[string]string, error) {
	// Most managed Cypher platforms don't expose storage/memory via Cypher
	// itself. Report what's queryable and mark the rest explicitly as not
	// observable, per the assignment's instruction to be honest about gaps.
	result := map[string]string{
		"stored_data_size": "not observable via Cypher - check platform console",
		"memory_usage":     "not observable via Cypher - check platform console",
	}

	sess := c.session(ctx, neo4j.AccessModeRead)
	defer sess.Close(ctx)

	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	t0 := time.Now()
	res, err := sess.Run(qctx, `MATCH (n) RETURN count(n) AS nodeCount`, nil)
	if err == nil {
		if rec, err2 := res.Single(qctx); err2 == nil {
			if v, ok := rec.Get("nodeCount"); ok {
				result["node_count"] = fmt.Sprintf("%v", v)
			}
		}
	}
	result["node_count_query_latency_ms"] = fmt.Sprintf("%.2f", time.Since(t0).Seconds()*1000)
	return result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
