#!/usr/bin/env bash
set -euo pipefail

DIR="${1:?Usage: $0 <folder> [output.tsv]}"
OUT="${2:-torrent_listing.tsv}"

if ! command -v aria2c >/dev/null 2>&1; then
  echo "ERROR: aria2c not found. Install it first (e.g. brew install aria2)."
  exit 1
fi

# Header
printf "torrent\tcontained_file\n" > "$OUT"

# Iterate torrents (non-recursive; change -maxdepth if you want recursion)
find "$DIR" -maxdepth 1 -type f -name '*.torrent' -print0 |
while IFS= read -r -d '' TORRENT; do
  TOR_BASENAME="$(basename "$TORRENT")"

  # Parse lines like: "  1|some/path/file.ext (123MiB)"
  aria2c --show-files "$TORRENT" 2>/dev/null |
  awk -v tor="$TOR_BASENAME" '
    BEGIN { OFS="\t" }
    /^[[:space:]]*[0-9]+[[:space:]]*\|/ {
      line=$0
      sub(/^[[:space:]]*[0-9]+[[:space:]]*\|[[:space:]]*/, "", line)
      sub(/[[:space:]]*\([0-9].*$/, "", line)  # drop trailing "(size ...)"
      print tor, line
    }
  ' >> "$OUT"

done

echo "Wrote: $OUT"

