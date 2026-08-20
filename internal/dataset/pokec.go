// Package dataset prepares the benchmark's input graph. We use a sample of
// the SNAP soc-Pokec social network (https://snap.stanford.edu/data/soc-Pokec.html),
// a real directed social graph with ~1.6M nodes / ~30M edges. We downsample
// to roughly 100k-300k relationships so it comfortably fits every platform's
// free/entry tier, per the assignment's fairness note.
//
// Download the raw edge list yourself first (see scripts/download_dataset.sh):
//
//	soc-pokec-relationships.txt  (tab-separated "src<TAB>dst" per line, one per directed edge)
//
//	Then run: go run ./cmd/benchmark prepare-data --input soc-pokec-relationships.txt \
//	             --target-edges 200000 --nodes-out data/nodes.csv --edges-out data/edges.csv
package dataset

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"

	"github.com/deepakgudla/graph-benchmark/internal/dbclient"
)

var regions = []string{"north", "south", "east", "west", "central"}

// Prepare reads the raw SNAP edge list, keeps the first targetEdges edges
// touching a contiguous prefix of node IDs (so the resulting subgraph stays
// connected rather than being a sparse random sample), assigns synthetic
// "region" and "age" properties to each node (needed for the indexed-lookup
// and aggregation workloads, since the raw edge list carries no properties),
// and writes normalized nodes.csv / edges.csv.
func Prepare(inputPath string, targetEdges int, nodesOut, edgesOut string) error {
	in, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", inputPath, err)
	}
	defer in.Close()

	rng := rand.New(rand.NewSource(42)) // fixed seed -> reproducible dataset

	seen := make(map[string]struct{})
	var edges []dbclient.Relationship

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() && len(edges) < targetEdges {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		src, dst := "n"+parts[0], "n"+parts[1]
		edges = append(edges, dbclient.Relationship{Source: src, Target: dst, Type: "KNOWS"})
		seen[src] = struct{}{}
		seen[dst] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", inputPath, err)
	}
	if len(edges) < targetEdges {
		return fmt.Errorf("input only yielded %d edges, wanted %d - use a larger source file", len(edges), targetEdges)
	}

	nodes := make([]dbclient.Node, 0, len(seen))
	for id := range seen {
		nodes = append(nodes, dbclient.Node{
			ID:     id,
			Region: regions[rng.Intn(len(regions))],
			Age:    18 + rng.Intn(60),
		})
	}

	if err := writeNodesCSV(nodesOut, nodes); err != nil {
		return err
	}
	if err := writeEdgesCSV(edgesOut, edges); err != nil {
		return err
	}

	fmt.Printf("prepared %d nodes / %d edges -> %s, %s\n", len(nodes), len(edges), nodesOut, edgesOut)
	return nil
}

func writeNodesCSV(path string, nodes []dbclient.Node) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"id", "region", "age"}); err != nil {
		return err
	}
	for _, n := range nodes {
		if err := w.Write([]string{n.ID, n.Region, strconv.Itoa(n.Age)}); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeEdgesCSV(path string, edges []dbclient.Relationship) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"src", "dst"}); err != nil {
		return err
	}
	for _, e := range edges {
		if err := w.Write([]string{e.Source, e.Target}); err != nil {
			return err
		}
	}
	return w.Error()
}

// Load reads previously-prepared nodes.csv / edges.csv back into memory.
func Load(nodesPath, edgesPath string) ([]dbclient.Node, []dbclient.Relationship, error) {
	nodes, err := loadNodes(nodesPath)
	if err != nil {
		return nil, nil, err
	}
	edges, err := loadEdges(edgesPath)
	if err != nil {
		return nil, nil, err
	}
	return nodes, edges, nil
}

func loadNodes(path string) ([]dbclient.Node, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	nodes := make([]dbclient.Node, 0, len(rows)-1)
	for _, row := range rows[1:] {
		age, _ := strconv.Atoi(row[2])
		nodes = append(nodes, dbclient.Node{ID: row[0], Region: row[1], Age: age})
	}
	return nodes, nil
}

func loadEdges(path string) ([]dbclient.Relationship, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	edges := make([]dbclient.Relationship, 0, len(rows)-1)
	for _, row := range rows[1:] {
		edges = append(edges, dbclient.Relationship{Source: row[0], Target: row[1], Type: "KNOWS"})
	}
	return edges, nil
}
