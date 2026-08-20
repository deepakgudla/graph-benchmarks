# Benchmark results

_Generated 2026-08-20T01:27:57+05:30_

## Data loading

| Platform | Nodes loaded | Node throughput (n/s) | Rels loaded | Rel throughput (r/s) | Total wall time |
|---|---|---|---|---|---|
| cognodb | 91489 | 1136.5 | 200000 | 1895.6 | 186.0s |
| neo4j-aura-free | 91489 | 8808.6 | 200000 | 8590.5 | 33.7s |
| memgraph-cloud | 91489 | 53.6 | 200000 | 29.6 | 8464.7s |
| arangodb-oasis-free | 91489 | 4426.4 | 200000 | 8918.0 | 43.1s |

## Traversals (p50 / p95, ms)

| Platform | 1-hop | 2-hop | 3-hop |
|---|---|---|---|
| cognodb | 303.45 / 308.43 | 303.65 / 316.00 | 306.47 / 320.34 |
| neo4j-aura-free | 122.42 / 138.54 | 118.99 / 132.88 | 121.96 / 159.59 |
| memgraph-cloud | 184.28 / 198.02 | 184.17 / 206.77 | 185.20 / 195.29 |
| arangodb-oasis-free | 26.03 / 33.29 | 26.08 / 52.61 | 26.01 / 32.68 |

## Lookups (p50 / p95, ms)

| Platform | Point lookup | Indexed lookup | Indexed on |
|---|---|---|---|
| cognodb | 305.26 / 314.36 | 349.25 / 661.38 | region |
| neo4j-aura-free | 125.56 / 140.02 | 126.09 / 143.16 | region |
| memgraph-cloud | 182.87 / 195.33 | 155.73 / 166.24 | region |
| arangodb-oasis-free | 26.09 / 48.71 | 25.98 / 32.93 | region |

## Aggregation (p50 / p95, ms)

| Platform | p50 | p95 |
|---|---|---|
| cognodb | 549.01 | 648.53 |
| neo4j-aura-free | 163.74 | 188.04 |
| memgraph-cloud | 189.00 | 205.06 |
| arangodb-oasis-free | 0.00 | 0.00 |

## Mixed read/write throughput

| Platform | Concurrency | Write ratio | QPS | Errors |
|---|---|---|---|---|
| cognodb | 1 | 20% | 3.4 | 1 |
| cognodb | 10 | 20% | 29.6 | 10 |
| cognodb | 40 | 20% | 92.7 | 40 |
| neo4j-aura-free | 1 | 20% | 8.2 | 1 |
| neo4j-aura-free | 10 | 20% | 75.7 | 10 |
| neo4j-aura-free | 40 | 20% | 288.3 | 40 |
| memgraph-cloud | 1 | 20% | 5.2 | 1 |
| memgraph-cloud | 10 | 20% | 35.2 | 10 |
| memgraph-cloud | 40 | 20% | 37.1 | 40 |
| arangodb-oasis-free | 1 | 20% | 34.5 | 116 |
| arangodb-oasis-free | 10 | 20% | 297.9 | 1021 |
| arangodb-oasis-free | 40 | 20% | 635.5 | 2167 |

## Footprint

| Platform | Notes |
|---|---|
| cognodb | memory_usage=not observable via Cypher - check platform console; node_count=91489; node_count_query_latency_ms=1709.91; stored_data_size=not observable via Cypher - check platform console |
| neo4j-aura-free | stored_data_size=not observable via Cypher - check platform console; memory_usage=not observable via Cypher - check platform console; node_count=91489; node_count_query_latency_ms=801.91 |
| memgraph-cloud | memory_usage=not observable via Cypher - check platform console; node_count=91489; node_count_query_latency_ms=1312.48; stored_data_size=not observable via Cypher - check platform console |
| arangodb-oasis-free | stored_data_size={"cacheInUse":false,"cacheSize":0,"cacheUsage":0,"documentsSize":6921846,"indexes":{"count":2,"size":4493899}} |

## Caveats

