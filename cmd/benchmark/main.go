// Command benchmark is the single entrypoint for the whole harness:
//
//	go run ./cmd/benchmark prepare-data -input soc-pokec-relationships.txt -target-edges 200000
//	go run ./cmd/benchmark run -config configs/platforms.json -platform cognodb
//	go run ./cmd/benchmark run -config configs/platforms.json            # all platforms
//	go run ./cmd/benchmark report -config configs/platforms.json         # build RESULTS.md from results/*.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/deepakgudla/graph-benchmark/internal/config"
	"github.com/deepakgudla/graph-benchmark/internal/dataset"
	"github.com/deepakgudla/graph-benchmark/internal/dbclient"
	"github.com/deepakgudla/graph-benchmark/internal/metrics"
	"github.com/deepakgudla/graph-benchmark/internal/workload"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "prepare-data":
		cmdPrepareData(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "report":
		cmdReport(os.Args[2:])
	case "reset":
		cmdReset(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  benchmark prepare-data -input <raw-edge-list> [-target-edges 200000] [-nodes-out data/nodes.csv] [-edges-out data/edges.csv]
  benchmark run -config configs/platforms.json [-platform <name>] [-no-reset]
  benchmark reset -config configs/platforms.json [-platform <name>]
  benchmark report -config configs/platforms.json`)
}

func cmdPrepareData(args []string) {
	fs := flag.NewFlagSet("prepare-data", flag.ExitOnError)
	input := fs.String("input", "", "path to raw soc-pokec-relationships.txt")
	targetEdges := fs.Int("target-edges", 200000, "number of relationships to sample")
	nodesOut := fs.String("nodes-out", "data/nodes.csv", "output path for node CSV")
	edgesOut := fs.String("edges-out", "data/edges.csv", "output path for edge CSV")
	fs.Parse(args)

	if *input == "" {
		log.Fatal("prepare-data: -input is required (download it with scripts/download_dataset.sh first)")
	}
	if err := os.MkdirAll(filepath.Dir(*nodesOut), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := dataset.Prepare(*input, *targetEdges, *nodesOut, *edgesOut); err != nil {
		log.Fatalf("prepare-data failed: %v", err)
	}
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "configs/platforms.json", "path to platform config YAML")
	onlyPlatform := fs.String("platform", "", "if set, only run this one platform (by name in config)")
	noReset := fs.Bool("no-reset", false, "skip wiping existing data before loading (default: reset first, so re-runs don't accumulate duplicates)")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	nodes, edges, err := dataset.Load(cfg.Dataset.NodesCSV, cfg.Dataset.EdgesCSV)
	if err != nil {
		log.Fatalf("loading dataset (did you run prepare-data?): %v", err)
	}
	log.Printf("dataset: %d nodes, %d relationships", len(nodes), len(edges))

	ctx := context.Background()

	for _, pc := range cfg.Platforms {
		if *onlyPlatform != "" && pc.Name != *onlyPlatform {
			continue
		}
		log.Printf("=== running benchmark: %s ===", pc.Name)

		client, err := dbclient.New(pc)
		if err != nil {
			log.Printf("skipping %s: %v", pc.Name, err)
			continue
		}

		if !*noReset {
			if err := client.Connect(ctx); err != nil {
				log.Printf("skipping %s: connect failed before reset: %v", pc.Name, err)
				continue
			}
			log.Printf("resetting %s (wiping existing data before load)...", pc.Name)
			if err := client.Reset(ctx); err != nil {
				log.Printf("skipping %s: reset failed: %v", pc.Name, err)
				client.Close(ctx)
				continue
			}
			client.Close(ctx)
			// workload.Run opens its own connection below, so this instance
			// is done with its job - a fresh Connect() there is cheap and
			// keeps Run() usable standalone (e.g. from other callers/tests).
		}

		result := workload.Run(ctx, client, nodes, edges, *cfg)
		result.Specs = map[string]string{
			"vcpu":    pc.AdvertisedVCPU,
			"ram":     pc.AdvertisedRAM,
			"storage": pc.AdvertisedStorage,
			"region":  pc.Region,
		}

		path, err := metrics.WriteJSON(cfg.ResultsDir, result)
		if err != nil {
			log.Printf("failed to write results for %s: %v", pc.Name, err)
			continue
		}
		log.Printf("=== %s done, results -> %s ===", pc.Name, path)
		if result.FailedStep != "" {
			log.Printf("!!! %s FAILED at step %q - see caveats in the result file", pc.Name, result.FailedStep)
		}
	}
}

func cmdReset(args []string) {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	configPath := fs.String("config", "configs/platforms.json", "path to platform config YAML")
	onlyPlatform := fs.String("platform", "", "if set, only reset this one platform (by name in config)")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	for _, pc := range cfg.Platforms {
		if *onlyPlatform != "" && pc.Name != *onlyPlatform {
			continue
		}
		client, err := dbclient.New(pc)
		if err != nil {
			log.Printf("skipping %s: %v", pc.Name, err)
			continue
		}
		if err := client.Connect(ctx); err != nil {
			log.Printf("skipping %s: connect failed: %v", pc.Name, err)
			continue
		}
		log.Printf("resetting %s ...", pc.Name)
		if err := client.Reset(ctx); err != nil {
			log.Printf("%s: reset failed: %v", pc.Name, err)
		} else {
			log.Printf("%s: reset complete", pc.Name)
		}
		client.Close(ctx)
	}
}

func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	configPath := fs.String("config", "configs/platforms.json", "path to platform config YAML")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	var results []metrics.PlatformResult
	for _, pc := range cfg.Platforms {
		path := filepath.Join(cfg.ResultsDir, pc.Name+".json")
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("no results for %s at %s, skipping (%v)", pc.Name, path, err)
			continue
		}
		var r metrics.PlatformResult
		if err := json.Unmarshal(b, &r); err != nil {
			log.Printf("bad results file %s: %v", path, err)
			continue
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		log.Fatal("no result files found - run `benchmark run` first")
	}

	path, err := metrics.WriteMarkdownSummary(cfg.ResultsDir, results)
	if err != nil {
		log.Fatalf("writing summary: %v", err)
	}
	log.Printf("wrote %s", path)
}
