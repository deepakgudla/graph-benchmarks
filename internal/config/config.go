package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// PlatformConfig describes one managed graph database under test.
// Credentials are never stored here directly - only the *name* of the
// environment variable to read them from, per the assignment's rule about
// not committing secrets.
type PlatformConfig struct {
	Name string `json:"name"` // e.g. "cognodb", "neo4j-aura", "memgraph"
	// Driver selects which dbclient implementation to construct:
	//   "cypher-bolt"  -> works for CognoDB, Neo4j AuraDB, Memgraph Cloud
	//   "arangodb-aql" -> ArangoDB Oasis
	Driver string `json:"driver"`

	URIEnv      string `json:"uri_env"`      // env var holding the connection URI
	UserEnv     string `json:"user_env"`     // env var holding the username
	PasswordEnv string `json:"password_env"` // env var holding the password
	Database    string `json:"database"`     // logical database/graph name, if applicable

	// Advertised free/entry-tier specs - documented here so the README
	// results table can be generated straight from config instead of by hand.
	AdvertisedVCPU    string `json:"advertised_vcpu"`
	AdvertisedRAM     string `json:"advertised_ram"`
	AdvertisedStorage string `json:"advertised_storage"`
	Region            string `json:"region"`
}

type WorkloadConfig struct {
	BatchSize            int     `json:"batch_size"`             // load batch size
	WarmupIterations     int     `json:"warmup_iterations"`      // per query type, discarded
	ReadIterations       int     `json:"read_iterations"`        // per query type, measured (>=100 recommended)
	MixedDurationSeconds int     `json:"mixed_duration_seconds"` // length of each concurrency-sweep run
	MixedConcurrencies   []int   `json:"mixed_concurrencies"`    // e.g. [1, 10, 40]
	MixedWriteRatio      float64 `json:"mixed_write_ratio"`      // e.g. 0.2 = 20% writes
}

type DatasetConfig struct {
	NodesCSV string `json:"nodes_csv"`
	EdgesCSV string `json:"edges_csv"`
}

type RunConfig struct {
	Platforms  []PlatformConfig `json:"platforms"`
	Dataset    DatasetConfig    `json:"dataset"`
	Workload   WorkloadConfig   `json:"workload"`
	ResultsDir string           `json:"results_dir"`
}

func Load(path string) (*RunConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg RunConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.Workload.BatchSize == 0 {
		cfg.Workload.BatchSize = 1000
	}
	if cfg.Workload.ReadIterations == 0 {
		cfg.Workload.ReadIterations = 100
	}
	if cfg.Workload.WarmupIterations == 0 {
		cfg.Workload.WarmupIterations = 10
	}
	if len(cfg.Workload.MixedConcurrencies) == 0 {
		cfg.Workload.MixedConcurrencies = []int{1, 10, 40}
	}
	if cfg.Workload.MixedDurationSeconds == 0 {
		cfg.Workload.MixedDurationSeconds = 20
	}
	if cfg.ResultsDir == "" {
		cfg.ResultsDir = "results"
	}
	return &cfg, nil
}

// ResolveSecret reads a credential from the environment, erroring loudly if
// it's missing rather than silently connecting with an empty string.
func ResolveSecret(envVar string) (string, error) {
	if envVar == "" {
		return "", nil
	}
	v, ok := os.LookupEnv(envVar)
	if !ok || v == "" {
		return "", fmt.Errorf("environment variable %s is not set (see .env.example)", envVar)
	}
	return v, nil
}
