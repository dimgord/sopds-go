# Rutracker Torrent Fetcher

A bash script to bulk download torrent files from rutracker.org search results with forum filtering support.

## Requirements

- bash
- curl
- python3 (for URL encoding)
- iconv (for Cyrillic text conversion)

## Installation

```bash
chmod +x fetch-torrents.sh
```

## Usage

```bash
./fetch-torrents.sh -c COOKIE -s SEARCH [-f FORUMS] [-p PRESET] [-o OUTDIR] [--list]
```

### Options

| Option | Description |
|--------|-------------|
| `-c COOKIE` | Browser cookie string (required) |
| `-s SEARCH` | Search term (required) |
| `-f FORUMS` | Forum IDs, comma-separated (e.g., `2387,2388`) |
| `-p PRESET` | Use preset forum group |
| `-o OUTDIR` | Output directory (default: `torrents`) |
| `--list` | List forums found in search results |
| `--list-presets` | Show available presets and forum IDs |
| `-h` | Show help |

### Getting the Cookie

1. Log in to rutracker.org in your browser
2. Open Developer Tools (F12) -> Network tab
3. Perform a search on the tracker
4. Find the request to `tracker.php`
5. Copy the `Cookie` header value

Required cookies:
- `bb_session` - Session ID
- `cf_clearance` - Cloudflare clearance token

Example cookie string:
```
bb_session=0-12345-xxxx; cf_clearance=yyyy
```

## Examples

### Basic search (all forums)

```bash
./fetch-torrents.sh -c 'bb_session=...; cf_clearance=...' -s 'Stephen King'
```

### Search with preset

```bash
# All audiobooks
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' -p audiobooks

# Sci-fi audiobooks only
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' -p audio-scifi

# Sci-fi books (text)
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' -p books-scifi
```

### Search with specific forums

```bash
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' -f '2388,2387,399'
```

### Custom output directory

```bash
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' -p audiobooks -o ~/Downloads/asimov
```

### List forums in search results

```bash
./fetch-torrents.sh -c 'COOKIE' -s 'Asimov' --list
```

## Presets

| Preset | Description | Forum IDs |
|--------|-------------|-----------|
| `audiobooks` | All audiobook forums | 2388,2387,399,402,490,499,2137,574,1036,403,1279,1909,1501,525,1580,661,695,467,2127,2325,530,2152,1350,716,2165 |
| `audio-scifi` | Sci-fi/fantasy audiobooks | 2388,2387,661,2348 |
| `audio-lit` | Literature audiobooks | 399,402,490,499,2137,574,467 |
| `books` | All book forums | 2045,2080,2043,2041,2042,2193,1037,21,39 |
| `books-scifi` | Sci-fi/fantasy books | 2045,2080 |

## Forum IDs Reference

### Audiobooks

| ID | Category |
|----|----------|
| 2388 | Foreign sci-fi/fantasy/horror |
| 2387 | Russian sci-fi/fantasy/horror |
| 399 | Foreign literature |
| 402 | Russian literature |
| 490 | Children's literature |
| 499 | Foreign detective/thriller |
| 2137 | Russian detective/thriller |
| 574 | Radio plays and readings |
| 1036 | Biographies |
| 403 | Educational/popular science |
| 1279 | Lossless audiobooks |
| 1909 | Audiobooks (AAC, ALAC) |
| 1501 | English audiobooks |
| 1580 | German audiobooks |
| 525 | Other foreign languages |
| 661 | Romantic fantasy |
| 695 | Poetry |
| 467 | Romance novels |

### Books (Text)

| ID | Category |
|----|----------|
| 2045 | Russian sci-fi/fantasy |
| 2080 | Foreign sci-fi/fantasy |
| 2043 | Russian literature |
| 2041 | Foreign literature (XX-XXI century) |
| 2042 | Foreign literature (before 1900) |
| 2193 | Literary magazines |
| 1037 | Self-published |
| 21 | Books and magazines (general) |
| 39 | Miscellaneous books |

## How It Works

1. Performs a search on rutracker with the given term and forum filters
2. Extracts pagination info and iterates through all result pages
3. Collects all unique torrent IDs
4. Downloads each torrent file with 1-second delay between requests
5. Validates downloaded files (checks for valid bencoded format)
6. Skips already existing files

## Output

- Torrent files are saved as `{topic_id}.torrent`
- Invalid downloads are automatically removed
- Summary shows downloaded/skipped/failed counts

## Notes

- The script uses a 1-second delay between requests to avoid rate limiting
- Cookies expire periodically; refresh them if downloads fail
- The `cf_clearance` cookie is required to bypass Cloudflare protection
- Search results are limited to what rutracker returns (typically up to 500 results)
