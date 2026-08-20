package dbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// ArangoClient talks to ArangoDB Oasis (or any ArangoDB instance) over its
// HTTP REST API using AQL. ArangoDB has no Bolt/Cypher support, so this is a
// deliberately separate implementation - documented in the README as a
// query-language difference rather than hidden.
type ArangoClient struct {
	platformName string
	baseURL      string // e.g. https://<instance>.arangodb.cloud:8529
	user         string
	password     string
	database     string
	httpClient   *http.Client
	rng          *rand.Rand
}

func NewArangoClient(platformName, baseURL, user, password, database string) *ArangoClient {
	if database == "" {
		database = "_system"
	}
	return &ArangoClient{
		platformName: platformName,
		baseURL:      strings.TrimRight(baseURL, "/"),
		user:         user,
		password:     password,
		database:     database,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (a *ArangoClient) Name() string { return a.platformName }

func (a *ArangoClient) Connect(ctx context.Context) error {
	// ArangoDB Oasis uses basic auth over HTTPS; a lightweight version check
	// both confirms reachability and confirms credentials.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/_api/version", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(a.user, a.password)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: connectivity check: %w", a.platformName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: connectivity check status %d: %s", a.platformName, resp.StatusCode, body)
	}
	return nil
}

func (a *ArangoClient) Close(ctx context.Context) error { return nil }

// aqlQuery executes a single AQL query with bind variables and returns the
// raw response body plus how long the request took.
func (a *ArangoClient) aqlQuery(ctx context.Context, query string, bindVars map[string]any) (time.Duration, []byte, error) {
	payload := map[string]any{"query": query, "bindVars": bindVars}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}

	url := fmt.Sprintf("%s/_db/%s/_api/cursor", a.baseURL, a.database)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(a.user, a.password)

	t0 := time.Now()
	resp, err := a.httpClient.Do(req)
	elapsed := time.Since(t0)
	if err != nil {
		return elapsed, nil, fmt.Errorf("%s: request failed: %w", a.platformName, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return elapsed, respBody, fmt.Errorf("%s: AQL error status %d: %s", a.platformName, resp.StatusCode, respBody)
	}
	return elapsed, respBody, nil
}

func (a *ArangoClient) Reset(ctx context.Context) error {
	// Truncate is a single fast operation (unlike a per-document AQL
	// REMOVE loop) and is a no-op if the collection doesn't exist yet.
	for _, coll := range []string{"knows", "person"} { // edges first, since they reference person docs
		url := fmt.Sprintf("%s/_db/%s/_api/collection/%s/truncate", a.baseURL, a.database, coll)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(a.user, a.password)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("%s: truncating %s: %w", a.platformName, coll, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 404 means the collection doesn't exist yet - fine on a fresh instance.
		if resp.StatusCode >= 300 && resp.StatusCode != 404 {
			return fmt.Errorf("%s: truncating %s status %d: %s", a.platformName, coll, resp.StatusCode, body)
		}
	}
	return nil
}

func (a *ArangoClient) EnsureConstraintsAndIndexes(ctx context.Context) error {
	// Create the "person" document collection and "knows" edge collection if
	// missing, then a persistent (hash-like) index on region.
	for _, coll := range []struct {
		name string
		typ  int // 2 = document, 3 = edge
	}{{"person", 2}, {"knows", 3}} {
		body, _ := json.Marshal(map[string]any{"name": coll.name, "type": coll.typ})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			fmt.Sprintf("%s/_db/%s/_api/collection", a.baseURL, a.database), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(a.user, a.password)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("%s: creating collection %s: %w", a.platformName, coll.name, err)
		}
		resp.Body.Close() // 409 (already exists) is fine and ignored deliberately
	}

	idxBody, _ := json.Marshal(map[string]any{
		"type":   "persistent",
		"fields": []string{"region"},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/_db/%s/_api/index?collection=person", a.baseURL, a.database), bytes.NewReader(idxBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(a.user, a.password)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: creating region index: %w", a.platformName, err)
	}
	resp.Body.Close()
	return nil
}

func (a *ArangoClient) LoadNodes(ctx context.Context, nodes []Node, batchSize int) (LoadStats, error) {
	start := time.Now()
	for i := 0; i < len(nodes); i += batchSize {
		end := min(i+batchSize, len(nodes))
		docs := make([]map[string]any, 0, end-i)
		for _, n := range nodes[i:end] {
			docs = append(docs, map[string]any{"_key": n.ID, "region": n.Region, "age": n.Age})
		}
		body, _ := json.Marshal(docs)
		url := fmt.Sprintf("%s/_db/%s/_api/document/person?onDuplicate=ignore", a.baseURL, a.database)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(a.user, a.password)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return LoadStats{}, fmt.Errorf("%s: loading node batch at %d: %w", a.platformName, i, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return LoadStats{}, fmt.Errorf("%s: node batch at %d status %d: %s", a.platformName, i, resp.StatusCode, respBody)
		}
	}
	return LoadStats{ItemsLoaded: len(nodes), Wall: time.Since(start)}, nil
}

func (a *ArangoClient) LoadRelationships(ctx context.Context, rels []Relationship, batchSize int) (LoadStats, error) {
	start := time.Now()
	for i := 0; i < len(rels); i += batchSize {
		end := min(i+batchSize, len(rels))
		docs := make([]map[string]any, 0, end-i)
		for _, r := range rels[i:end] {
			docs = append(docs, map[string]any{
				"_from": "person/" + r.Source,
				"_to":   "person/" + r.Target,
			})
		}
		body, _ := json.Marshal(docs)
		url := fmt.Sprintf("%s/_db/%s/_api/document/knows?onDuplicate=ignore", a.baseURL, a.database)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(a.user, a.password)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return LoadStats{}, fmt.Errorf("%s: loading rel batch at %d: %w", a.platformName, i, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return LoadStats{}, fmt.Errorf("%s: rel batch at %d status %d: %s", a.platformName, i, resp.StatusCode, respBody)
		}
	}
	return LoadStats{ItemsLoaded: len(rels), Wall: time.Since(start)}, nil
}

func (a *ArangoClient) Traversal(ctx context.Context, startNodeID string, hops int) (time.Duration, error) {
	q := fmt.Sprintf(`
		FOR v IN %d..%d OUTBOUND CONCAT('person/', @id) knows
		RETURN v`, hops, hops)
	elapsed, _, err := a.aqlQuery(ctx, q, map[string]any{"id": startNodeID})
	return elapsed, err
}

func (a *ArangoClient) PointLookup(ctx context.Context, nodeID string) (time.Duration, error) {
	q := `RETURN DOCUMENT(CONCAT('person/', @id))`
	elapsed, _, err := a.aqlQuery(ctx, q, map[string]any{"id": nodeID})
	return elapsed, err
}

func (a *ArangoClient) IndexedLookup(ctx context.Context, region string) (time.Duration, error) {
	q := `FOR p IN person FILTER p.region == @region LIMIT 50 RETURN p._key`
	elapsed, _, err := a.aqlQuery(ctx, q, map[string]any{"region": region})
	return elapsed, err
}

func (a *ArangoClient) Aggregation(ctx context.Context) (time.Duration, error) {
	q := `FOR p IN person COLLECT region = p.region WITH COUNT INTO c SORT c DESC RETURN {region, c}`
	elapsed, _, err := a.aqlQuery(ctx, q, nil)
	return elapsed, err
}

func (a *ArangoClient) ReadOp(ctx context.Context) error {
	id := fmt.Sprintf("n%d", a.rng.Intn(500000))
	_, _, err := a.aqlQuery(ctx, `RETURN DOCUMENT(CONCAT('person/', @id))`, map[string]any{"id": id})
	return err
}

func (a *ArangoClient) WriteOp(ctx context.Context, seq int) error {
	id := fmt.Sprintf("n%d", a.rng.Intn(500000))
	q := `UPDATE @id WITH { writeCounter: (DOCUMENT(CONCAT('person/', @id)).writeCounter OR 0) + 1 } IN person`
	_, _, err := a.aqlQuery(ctx, q, map[string]any{"id": id})
	return err
}

func (a *ArangoClient) Footprint(ctx context.Context) (map[string]string, error) {
	result := map[string]string{}
	url := fmt.Sprintf("%s/_db/%s/_api/collection/person/figures", a.baseURL, a.database)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.SetBasicAuth(a.user, a.password)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		result["stored_data_size"] = "not observable (figures request failed)"
		return result, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		if figures, ok := parsed["figures"].(map[string]any); ok {
			b, _ := json.Marshal(figures)
			result["stored_data_size"] = string(b)
		}
	}
	if _, ok := result["stored_data_size"]; !ok {
		result["stored_data_size"] = "not observable"
	}
	return result, nil
}
