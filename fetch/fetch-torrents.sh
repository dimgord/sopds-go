#!/bin/bash
# Fetch all torrents from rutracker search results
# Usage: ./fetch-torrents.sh -c "cookie" -s "search" [-f "forum_ids"] [-o output_dir]

set -e

# Common forum IDs for quick reference
PRESETS="
Presets (use with -p):
  audiobooks    All audiobook forums
  audio-scifi   Sci-fi/fantasy audiobooks
  audio-lit     Literature audiobooks
  books         All book forums
  books-scifi   Sci-fi/fantasy books

Common forum IDs:
  Audiobooks:
    2388  Foreign sci-fi/fantasy/horror (audio)
    2387  Russian sci-fi/fantasy/horror (audio)
    399   Foreign literature (audio)
    402   Russian literature (audio)
    490   Children's literature (audio)
    499   Foreign detective/thriller (audio)
    2137  Russian detective/thriller (audio)
    574   Radio plays and readings
    1036  Biographies (audio)
    403   Educational/popular science (audio)
    1279  Lossless audiobooks
    1909  Audiobooks (AAC, ALAC)
    1501  English audiobooks

  Books:
    2045  Russian sci-fi/fantasy
    2080  Foreign sci-fi/fantasy
    2043  Russian literature
    2041  Foreign literature (XX-XXI)
    2042  Foreign literature (before 1900)
"

usage() {
    echo "Usage: $0 -c COOKIE -s SEARCH [-f FORUMS] [-p PRESET] [-o OUTDIR] [--list]"
    echo ""
    echo "Options:"
    echo "  -c COOKIE    Browser cookie string (required)"
    echo "  -s SEARCH    Search term (required)"
    echo "  -f FORUMS    Forum IDs, comma-separated (e.g., '2387,2388')"
    echo "  -p PRESET    Use preset forum group (see --list-presets)"
    echo "  -o OUTDIR    Output directory (default: torrents)"
    echo "  --list       List forums found in search results"
    echo "  --list-presets  Show available presets and forum IDs"
    echo "  -h           Show this help"
    echo ""
    echo "Example:"
    echo "  $0 -c 'bb_session=...' -s 'Asimov' -p audio-scifi"
    echo "  $0 -c 'bb_session=...' -s 'Asimov' -f '2387,2388,399'"
    exit 1
}

list_presets() {
    echo "$PRESETS"
    exit 0
}

OUTDIR="torrents"
COOKIE=""
SEARCH=""
FORUMS=""
PRESET=""
LIST_FORUMS=0

while [[ $# -gt 0 ]]; do
    case $1 in
        -c) COOKIE="$2"; shift 2 ;;
        -s) SEARCH="$2"; shift 2 ;;
        -f) FORUMS="$2"; shift 2 ;;
        -p) PRESET="$2"; shift 2 ;;
        -o) OUTDIR="$2"; shift 2 ;;
        --list) LIST_FORUMS=1; shift ;;
        --list-presets) list_presets ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

# Apply presets
case $PRESET in
    audiobooks)
        FORUMS="2388,2387,399,402,490,499,2137,574,1036,403,1279,1909,1501,525,1580,661,695,467,2127,2325,530,2152,1350,716,2165"
        ;;
    audio-scifi)
        FORUMS="2388,2387,661,2348"
        ;;
    audio-lit)
        FORUMS="399,402,490,499,2137,574,467"
        ;;
    books)
        FORUMS="2045,2080,2043,2041,2042,2193,1037,21,39"
        ;;
    books-scifi)
        FORUMS="2045,2080"
        ;;
    "") ;;
    *)
        echo "Unknown preset: $PRESET"
        echo "Use --list-presets to see available presets"
        exit 1
        ;;
esac

if [ -z "$COOKIE" ] || [ -z "$SEARCH" ]; then
    echo "Error: Cookie and search term are required"
    usage
fi

mkdir -p "$OUTDIR"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

UA="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"

# URL encode search term
SEARCH_ENCODED=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$SEARCH'))")

echo "Searching for: $SEARCH"
[ -n "$FORUMS" ] && echo "Forums: $FORUMS"
echo "Output directory: $OUTDIR"
echo ""

# Build forum filter for POST data
FORUM_DATA=""
if [ -n "$FORUMS" ]; then
    for fid in $(echo "$FORUMS" | tr ',' ' '); do
        FORUM_DATA="${FORUM_DATA}&f[]=${fid}"
    done
fi

# Fetch first page
echo "Fetching search results..."
curl -s "https://rutracker.org/forum/tracker.php" \
    -b "$COOKIE" \
    -H "referer: https://rutracker.org/forum/tracker.php" \
    -H "user-agent: $UA" \
    -H "content-type: application/x-www-form-urlencoded" \
    --data-raw "nm=${SEARCH_ENCODED}${FORUM_DATA}" \
    -o "$TMPDIR/page1.html"

# List forums mode
if [ $LIST_FORUMS -eq 1 ]; then
    echo "Forums found in search results:"
    echo ""
    # Extract forum IDs from results and look up names
    FOUND_FORUMS=$(grep -oP 'viewforum\.php\?f=\K\d+' "$TMPDIR/page1.html" 2>/dev/null | sort -u)

    for fid in $FOUND_FORUMS; do
        # Try to get forum name from the page
        fname=$(iconv -f cp1251 -t utf-8 "$TMPDIR/page1.html" 2>/dev/null | grep -oP "f=${fid}[^>]*>[^<]+" | head -1 | sed 's/.*>//')
        if [ -n "$fname" ]; then
            echo "  $fid: $fname"
        else
            echo "  $fid"
        fi
    done
    exit 0
fi

# Extract search_id for pagination
SEARCH_ID=$(grep -oP 'search_id=\K[a-zA-Z0-9]+' "$TMPDIR/page1.html" | head -1)

if [ -z "$SEARCH_ID" ]; then
    echo "Error: Could not find search_id. Check your cookie or search term."
    echo "Response preview:"
    head -50 "$TMPDIR/page1.html" | iconv -f cp1251 -t utf-8 2>/dev/null || head -50 "$TMPDIR/page1.html"
    exit 1
fi

echo "Search ID: $SEARCH_ID"

# Find max page
MAX_START=$(grep -oP 'start=\K\d+' "$TMPDIR/page1.html" | sort -rn | head -1)
MAX_START=${MAX_START:-0}
TOTAL_PAGES=$((MAX_START / 50 + 1))

echo "Found $TOTAL_PAGES page(s)"
echo ""

# Collect all torrent IDs
ALL_IDS="$TMPDIR/all_ids.txt"
> "$ALL_IDS"

for start in $(seq 0 50 $MAX_START); do
    page=$((start / 50 + 1))
    echo -n "Fetching page $page/$TOTAL_PAGES... "

    if [ $start -eq 0 ]; then
        cp "$TMPDIR/page1.html" "$TMPDIR/current.html"
    else
        curl -s "https://rutracker.org/forum/tracker.php?search_id=$SEARCH_ID&start=$start" \
            -b "$COOKIE" \
            -H "referer: https://rutracker.org/forum/tracker.php" \
            -H "user-agent: $UA" \
            -o "$TMPDIR/current.html"
        sleep 1
    fi

    count=$(grep -oP 'dl\.php\?t=\K[0-9]+' "$TMPDIR/current.html" | tee -a "$ALL_IDS" | wc -l)
    echo "$count torrents"
done

# Remove duplicates
sort -u "$ALL_IDS" -o "$ALL_IDS"
TOTAL=$(wc -l < "$ALL_IDS")

if [ "$TOTAL" -eq 0 ]; then
    echo "No torrents found."
    exit 0
fi

echo ""
echo "Total unique torrents: $TOTAL"
echo "Downloading..."
echo ""

# Download torrents
n=0
failed=0
skipped=0
while read -r id; do
    n=$((n + 1))
    outfile="$OUTDIR/${id}.torrent"

    if [ -f "$outfile" ]; then
        skipped=$((skipped + 1))
        continue
    fi

    echo -n "[$n/$TOTAL] $id... "

    curl -s "https://rutracker.org/forum/dl.php?t=$id" \
        -b "$COOKIE" \
        -H "referer: https://rutracker.org/forum/tracker.php" \
        -H "user-agent: $UA" \
        -o "$outfile"

    # Validate torrent (bencoded dict starts with 'd')
    if [ -f "$outfile" ] && head -c1 "$outfile" | grep -q "d"; then
        echo "OK"
    else
        echo "FAILED"
        rm -f "$outfile"
        failed=$((failed + 1))
    fi

    sleep 1
done < "$ALL_IDS"

echo ""
echo "=== Done ==="
downloaded=$((TOTAL - skipped - failed))
echo "Downloaded: $downloaded"
[ $skipped -gt 0 ] && echo "Skipped (existing): $skipped"
[ $failed -gt 0 ] && echo "Failed: $failed"
echo ""
echo "Output: $OUTDIR/"
ls "$OUTDIR"/*.torrent 2>/dev/null | wc -l | xargs -I{} echo "Total files: {}"
du -sh "$OUTDIR"
