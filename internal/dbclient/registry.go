package dbclient

import (
	"fmt"

	"github.com/deepakgudla/graph-benchmark/internal/config"
)

// New constructs the correct Client implementation for a platform config,
// resolving credentials from the environment variables named in the config.
func New(pc config.PlatformConfig) (Client, error) {
	uri, err := config.ResolveSecret(pc.URIEnv)
	if err != nil {
		return nil, err
	}
	user, err := config.ResolveSecret(pc.UserEnv)
	if err != nil {
		return nil, err
	}
	pass, err := config.ResolveSecret(pc.PasswordEnv)
	if err != nil {
		return nil, err
	}

	switch pc.Driver {
	case "cypher-bolt":
		// Covers CognoDB Cloud, Neo4j AuraDB Free, and Memgraph Cloud - all
		// speak Bolt + Cypher, so this one implementation is reused as-is.
		return NewCypherClient(pc.Name, uri, user, pass, pc.Database), nil
	case "arangodb-aql":
		return NewArangoClient(pc.Name, uri, user, pass, pc.Database), nil
	default:
		return nil, fmt.Errorf("unknown driver %q for platform %q (add an implementation in internal/dbclient)", pc.Driver, pc.Name)
	}
}
