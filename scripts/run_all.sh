#!/usr/bin/env bash
# End-to-end run: download+sample the dataset (if not already present),
# benchmark every platform in configs/platforms.json, then build RESULTS.md.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -f .env ]; then
  set -a; source .env; set +a
fi

if [ ! -f soc-pokec-relationships.txt ]; then
  ./scripts/download_dataset.sh .
fi

if [ ! -f data/nodes.csv ]; then
  go run ./cmd/benchmark prepare-data \
    -input soc-pokec-relationships.txt \
    -target-edges 200000 \
    -nodes-out data/nodes.csv \
    -edges-out data/edges.csv
fi

go run ./cmd/benchmark run -config configs/platforms.json
go run ./cmd/benchmark report -config configs/platforms.json

echo "Done. See results/RESULTS.md"
