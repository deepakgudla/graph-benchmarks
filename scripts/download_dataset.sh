#!/usr/bin/env bash
# Downloads the SNAP soc-Pokec directed social network edge list.
# https://snap.stanford.edu/data/soc-Pokec.html
# ~1.6M nodes / ~30M edges raw - we sample this down in `prepare-data`.
set -euo pipefail

OUT_DIR="${1:-.}"
URL="https://snap.stanford.edu/data/soc-pokec-relationships.txt.gz"

mkdir -p "$OUT_DIR"
echo "Downloading $URL ..."
curl -L -o "$OUT_DIR/soc-pokec-relationships.txt.gz" "$URL"
gunzip -k -f "$OUT_DIR/soc-pokec-relationships.txt.gz"
echo "Done: $OUT_DIR/soc-pokec-relationships.txt"
