# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Rules

**CRITICAL: ALWAYS DOCUMENT CHANGES IMMEDIATELY!**
- After ANY code changes, update PROGRESS.md with what was changed
- Update CLAUDE.md if architecture or major features change
- Do NOT wait for user to ask - document IMMEDIATELY after implementation
- Include: files modified, new methods/functions, bug fixes, performance changes
- NO EXCUSES - if you wrote code, document it in the same response
- This is NON-NEGOTIABLE - user should NEVER have to remind you

**Why:** PROGRESS.md is the canonical change log for this project — revision-numbered, dated, append-only. It's what the maintainer reads to reconstruct *why* code looks the way it does months later. Git history alone is too coarse; commit messages don't carry the diagnostic detail (root-cause analysis, what was tried, which files moved). If you skip the entry, that context is lost.

**Format** (see `PROGRESS.md` revisions 48–51 for good templates):
- New entry at the **top** of the file, numbered `### Revision N - YYYY-MM-DD` with a bold one-line title
- A short prose summary of *what changed and why*, including root cause if it was a bug
- A `**Files Modified:**` section listing each touched file with bullet points naming the specific functions/fields changed
- For multi-issue changes, number them (1, 2, 3) with a short heading per issue

**Also update CLAUDE.md** when architecture, route layout, config schema, or major features change — not for ordinary bug fixes.

---

## Project Overview

Simple OPDS Catalog (SOPDS) is an OPDS (Open Publication Distribution System) server for managing and serving e-book collections. It provides OPDS/Web catalog access, automatic book scanning, metadata extraction from FB2 files, and format conversion. Backed by PostgreSQL.

## Architecture

```
sopds-go/
├── cmd/sopds/main.go      # CLI entry point with cobra
├── cmd/sopds-tts/main.go  # TTS subprocess (ONNX memory isolation)
├── cmd/zipdupes/main.go   # Standalone utility: find duplicate ZIPs in library
├── internal/
│   ├── config/            # YAML configuration parser
│   ├── converter/         # FB2 to EPUB/MOBI conversion (pure Go)
│   ├── database/          # Legacy types (models.go only)
│   ├── domain/            # Domain-Driven Design layer
│   │   ├── book/          # Book aggregate + value objects
│   │   ├── author/        # Author entity
│   │   ├── genre/         # Genre entity
│   │   ├── series/        # Series entity
│   │   ├── catalog/       # Catalog entity
│   │   ├── user/          # User entity
│   │   └── repository/    # Repository interfaces
│   ├── i18n/              # Internationalization
│   │   ├── i18n.go        # Loader, language detection, supportedLanguages list
│   │   └── locales/       # Per-language YAML translation files
│   ├── infrastructure/
│   │   └── persistence/   # GORM implementations
│   │       ├── database.go      # GORM connection + migrations
│   │       ├── models.go        # GORM models
│   │       ├── mappers.go       # Domain <-> GORM conversion
│   │       ├── scopes.go        # Reusable query scopes
│   │       ├── *_repository.go  # Repository implementations
│   │       ├── adapters.go      # Domain <-> Legacy conversion
│   │       └── service.go       # Bridge layer for handlers
│   ├── opds/              # OPDS Atom feed generation
│   ├── scanner/           # Parallel directory scanner, FB2/audio parsers
│   │   ├── scanner.go     # Main scanner logic
│   │   ├── fb2parser.go   # FB2 metadata extraction
│   │   ├── audioparser.go # Audio metadata extraction (ID3, M4A, FLAC, OGG)
│   │   ├── audiobookgrouper.go # Multi-file audiobook grouping
│   │   └── inxparser.go   # Nokia INX index parser (AWB audiobook durations)
│   ├── tts/               # Text-to-speech with piper ONNX models
│   │   ├── piper.go       # ONNX inference, espeak-ng phonemization, WAV encoding
│   │   ├── extractor.go   # FB2 text extraction and chunking
│   │   ├── generator.go   # Job processing, subprocess orchestration
│   │   ├── queue.go       # Background job queue with progress tracking
│   │   └── cache.go       # Per-book audio cache with metadata
│   └── server/            # HTTP server with chi router
│       ├── server.go      # Router setup, middleware
│       ├── handlers.go    # OPDS API handlers
├── sopds-tts-rs/              # Rust TTS subprocess (GPU via CUDA)
│   ├── src/main.rs            # ONNX inference + espeak-ng + WAV encoding
│   ├── Cargo.toml             # ort 2.0.0-rc.10, serde, hound
│   └── flake.nix              # Nix dev shell (ORT 1.22 + cuDNN 9.8 + sm_61)
│       └── web.go         # Web UI handlers and templates
└── config.yaml            # Main configuration file
```

## Build & Run

**IMPORTANT: The executable name is `sopds`, NOT `sopdsgo`!**

```bash
cd sopds-go
task build                      # Builds both sopds and sopds-tts
# Or manually:
go build -o sopds ./cmd/sopds
go build -o sopds-tts ./cmd/sopds-tts  # TTS subprocess helper

# Server management (systemd service - production)
sudo systemctl start sopds.service
sudo systemctl stop sopds.service
sudo systemctl restart sopds.service
sudo systemctl status sopds.service

# CLI commands (development/manual)
./sopds start         # Start server (foreground)
./sopds scan          # Manual library scan
./sopds init          # Create config.yaml template
./sopds migrate       # Run database migrations
```

## Testing

```bash
# Run everything
go test ./...
task test                                   # equivalent

# Single package
go test ./internal/scanner/
go test -v ./internal/scanner/              # with per-test output

# Single test by name (uses Go regex against TestXxx names)
go test -run TestFB2DataToEPUB ./internal/converter/
go test -run TestParseFB2 -v ./internal/scanner/

# Race detector (recommended when touching scanner, tts, or queue code)
go test -race ./...

# Benchmarks
go test -bench=. ./internal/converter/
```

Test files live next to the code (`*_test.go` in each package). `test_baseline.txt` at repo root is a snapshot of expected output from `go test ./...` — diff against it to spot regressions in test suite shape (e.g., a test silently disappearing).

## Utility: zipdupes

`cmd/zipdupes/` is a standalone diagnostic CLI for the book library. It scans directories of `.zip` archives and reports overlap between them — useful when triaging duplicate book collections before deciding what to delete.

```bash
go build -o zipdupes ./cmd/zipdupes

zipdupes <dir1> [dir2]...        # Summary of duplicate file counts per ZIP
zipdupes -l <dir>                # List all files inside each ZIP
zipdupes -l -o <dir>             # List only the files that are duplicates
zipdupes -d <dir>                # Show only ZIPs fully covered by others (safe to delete)
```

Duplicates are matched by `(filename, size)` tuples across all ZIPs in the scanned directories. Not built by default — build manually when needed. Does not touch the database.

## Taskfile (Recommended)

Install [Task](https://taskfile.dev) and use `Taskfile.yml` for common operations:

```bash
task --list           # Show all available tasks

# Common tasks
task build            # Build binary
task migrate          # Run migrations
task start            # Build and start server
task stop             # Stop server
task restart          # Restart server
task scan             # Run library scan

# Systemd service
task service-start    # Start systemd service
task service-stop     # Stop systemd service
task service-restart  # Restart systemd service
task service-logs     # View service logs

# Database
task db-backup        # Backup database
task db-vacuum        # Run VACUUM ANALYZE
task db-stats         # Show table statistics

# Development
task dev              # Build and start
task test             # Run tests
task lint             # Run vet and fmt
task clean            # Remove artifacts
```

## Database Setup

```bash
# Create PostgreSQL database
psql -U postgres -c "CREATE USER sopds WITH PASSWORD 'sopds';"
psql -U postgres -c "CREATE DATABASE sopds OWNER sopds;"

# Or use init.sql
psql -U postgres -f init.sql

# Run migrations
./sopds migrate
```

## Database Backup & Restore

```bash
# Backup (custom format - recommended)
pg_dump -U sopds -h localhost -Fc sopds > sopds_backup.dump

# Backup with timestamp
pg_dump -U sopds -h localhost -Fc sopds > sopds_$(date +%Y%m%d_%H%M%S).dump

# Backup (SQL format - human-readable)
pg_dump -U sopds -h localhost sopds > sopds_backup.sql

# Restore from custom format
pg_restore -U sopds -h localhost -d sopds -c sopds_backup.dump

# Restore from SQL format
psql -U sopds -h localhost -d sopds < sopds_backup.sql

# Full restore (drop and recreate)
dropdb -U sopds sopds && createdb -U sopds sopds && pg_restore -U sopds -d sopds sopds_backup.dump
```

## MySQL to PostgreSQL Migration

Import data from existing MySQL SOPDS database:

```bash
# Basic import
./sopds import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds

# Clear PostgreSQL tables before import
./sopds import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds --clear
```

## Database Maintenance

```bash
# Check table statistics (dead tuples indicate need for vacuum)
PGPASSWORD=sopds psql -U sopds -h localhost -d sopds -c "
SELECT relname, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum,
       pg_size_pretty(pg_table_size(relid)) as size
FROM pg_stat_user_tables ORDER BY n_dead_tup DESC;"

# Manual vacuum (reclaims dead tuple space, updates statistics)
PGPASSWORD=sopds psql -U sopds -h localhost -d sopds -c "VACUUM ANALYZE books;"

# Vacuum all tables
PGPASSWORD=sopds psql -U sopds -h localhost -d sopds -c "VACUUM ANALYZE;"

# Check triggers on books table
PGPASSWORD=sopds psql -U sopds -h localhost -d sopds -c "
SELECT tgname, pg_get_triggerdef(oid)
FROM pg_trigger WHERE tgrelid = 'books'::regclass AND NOT tgisinternal;"

# Check indexes
PGPASSWORD=sopds psql -U sopds -h localhost -d sopds -c "\di+ books*"
```

**Performance Notes:**
- The `books_search_vector_trigger` only fires on title/annotation changes (not avail updates)
- Scanner uses `SET LOCAL synchronous_commit = off` within transactions for faster commits
- ZIP scanning uses 1 SELECT per catalog (GetBookMapByCatalog) instead of 1 per book
- All ZIP operations (inserts, author links, availability updates) in single transaction
- Run `VACUUM ANALYZE` after large imports or scans if queries become slow
- Keep `scanner.workers` at 4-8; more workers cause I/O saturation and lock contention

## Configuration

Main config: `config.yaml`

```yaml
database:
  host: localhost
  port: 5432
  user: sopds
  password: sopds
  name: sopds
  sslmode: disable

library:
  root: /path/to/books        # Book collection path
  formats:                    # File extensions to index
    - .fb2
    - .epub
    - .mobi
    - .pdf
    - .mp3                    # Audiobook formats
    - .m4b
    - .m4a
    - .flac
    - .ogg
    - .opus
  scan_zip: true              # Scan inside ZIP archives
  rescan_zip: false           # Re-scan already processed ZIPs

server:
  bind: 0.0.0.0
  port: 8081
  opds_prefix: /opds          # OPDS API endpoint
  web_prefix: /web            # Web UI endpoint
  auth:
    enabled: false            # Basic auth for anonymous/OPDS users
    users:
      - username: admin
        password: admin
  jwt_secret: ""              # Secret for JWT tokens (auto-generated if empty)

scanner:
  workers: 4                  # Parallel scan workers
  on_start: false             # Scan on server start
  schedule: ""                # Cron schedule (e.g., "0 2 * * *")
  duplicates: normal          # none, normal, strong, clear
  auto_clean: ask             # Missing archives: ask, yes (auto-delete), no (skip)

site:
  title: "SOPDS Library"

logging:
  level: info                 # Log level (info, debug, warn, error)
  file: /var/log/sopds/sopds.log  # Log file path (empty for stderr)
  max_size: 10                # Max size in MB before rotation
  max_backups: 3              # Number of old log files to keep

converters:
  fb2toepub: ""               # Not used (pure Go converter)
  fb2tomobi: "ebook-convert"  # Calibre's ebook-convert path
  temp_dir: /tmp
```

## Web Interface

Access at `http://localhost:8081/web/`

Features:
- Modern responsive UI with Font Awesome icons
- **Multi-language support** (English, Ukrainian) with language switcher
- Advanced search with separate title and author fields
- Pattern filters for language, genre, series
- Scoped search (search within current author/genre/series/catalog/language)
- Browse by: Authors, Genres, Series, Catalogs, Languages, **Audiobooks**
- Hierarchical letter navigation (1-char → 2-char → 3-char drill-down)
- Download books in original format
- Convert FB2 to EPUB/MOBI on-the-fly
- New books section (last 7 days)
- Personal bookshelf (add/remove books)
- Duplicate detection with "See duplicates" links to view all versions of a book
- Help page with usage instructions
- **Audiobook support**: dedicated browser, detail page with tree view, duration/narrator display, headphones badge
- **User authentication**: register, login (email or username), logout, password reset
- Guest mode with warning banner (bookshelf not persisted)
- User dropdown in navigation header

**Internationalization (i18n):**
- Supported languages: English (en), Ukrainian (uk), French (fr), Spanish (es), German (de)
- Language preference saved in cookie (1 year)
- Switch via language selector in UI header
- Translations stored in YAML files: `internal/i18n/locales/*.yaml`

**To add a new language:**
1. Copy `internal/i18n/locales/en.yaml` to `internal/i18n/locales/XX.yaml`
2. Translate all values in the new file
3. Add language code to `supportedLanguages` in `internal/i18n/i18n.go`
4. Add display name to `languageNames` map in same file
5. Rebuild the application

## OPDS Interface

Access at `http://localhost:8081/opds/`

Compatible with OPDS readers like:
- Moon+ Reader
- FBReader
- Aldiko
- Calibre

## Format Conversion

**FB2 to EPUB** - Pure Go implementation (no external dependencies)
- Parses FB2 XML structure
- Converts to valid EPUB with proper metadata
- Embeds images from FB2 binary section
- Generates TOC and stylesheet

**FB2 to MOBI** - Requires Calibre
```bash
# Install Calibre (provides ebook-convert)
sudo dnf install calibre    # Fedora
sudo apt install calibre    # Ubuntu/Debian
```

## Text-to-Speech (TTS)

Generates audio from FB2 ebooks using [piper](https://github.com/rhasspy/piper) ONNX voice models.

**Architecture: Subprocess isolation**
- The main `sopds` process never loads ONNX runtime
- Each text chunk is generated by a separate `sopds-tts` subprocess
- On exit, the OS reclaims all native memory (solves ONNX memory leaks)
- Multiple chunks run in parallel (configurable via `tts.workers`)

**`sopds-tts` binary** (`cmd/sopds-tts/main.go`):
```
sopds-tts <model_path> <output_path>   # text read from stdin
```
Loads ONNX model, runs inference, writes WAV, exits. Must be built alongside `sopds` (`task build` builds both).

**Voice model support:**
- Single-speaker models (standard piper): uses espeak-ng for IPA phonemization
- Multi-speaker models (`num_speakers > 1`): adds `sid` tensor, uses first speaker by default
- Text phoneme type (`phoneme_type: "text"`): skips espeak-ng, maps raw characters directly
- Language code normalization: `uk-UA` matches config key `uk`

**Configuration (config.yaml):**
```yaml
tts:
  enabled: true
  models_dir: "/var/lib/piper/models"   # Directory with .onnx + .onnx.json files
  voices:
    en: "en_US-ljspeech-high"
    uk: "uk_UA-ukrainian_tts-medium"
  default_voice: "en_US-ljspeech-high"
  cache_dir: "/var/lib/sopds/tts_cache"
  workers: 2                             # Parallel chunk workers
  chunk_size: 5000                       # Characters per chunk
```

**Web routes:**
- `POST /web/book/{id}/tts/generate` - Queue TTS generation
- `GET /web/book/{id}/tts/status` - Generation status (JSON, with chunk progress)
- `GET /web/book/{id}/tts` - TTS player page
- `GET /web/book/{id}/tts/chunk/{idx}` - Stream audio chunk

**Requirements:**
- `sopds-tts` binary (built from `cmd/sopds-tts/`)
- ONNX Runtime shared library (`libonnxruntime.so`)
- espeak-ng (for IPA-based voice models)
- Piper voice model files (`.onnx` + `.onnx.json`)

**Rust TTS alternative** (`sopds-tts-rs/`):

Drop-in replacement for `sopds-tts` with CUDA GPU acceleration. Same CLI
interface (`sopds-tts-rs <model> <output>`, text on stdin).

```bash
cd sopds-tts-rs
TMPDIR=/home/dimgord/tmp nix develop ./   # first time: ~15-30 min (ORT source build)
cargo build --release
```

Nix flake uses two nixpkgs: latest (Rust toolchain) + pinned June 2025 (cuDNN 9.8
for Pascal/sm_61 GPU support). See `sopds-tts-rs/README.md` for details.

---

## Audiobook Support

Supports audio formats: MP3, M4B, M4A, FLAC, OGG, OPUS, AWB

**Features:**
- Dedicated audiobook browser at `/web/audio` with filters
- Audiobook detail page at `/web/audio/{id}` with tree view of chapters/tracks
- Individual track download from archives (ZIP/7z) via `/web/audio/{id}/track?file=path`
- Track selection UI: select all, part-level select, individual checkboxes
- "Download Selected" button for bulk track downloads
- Metadata extraction using dhowden/tag library (ID3, MP4 atoms, Vorbis comments)
- Duration display (formatted as "Xh Ym" or "Xm Ys")
- Narrator tracking (stored as author with role='narrator')
- Track count for multi-file audiobooks
- Web UI shows headphones icon for audiobooks

**Archive Audiobook Handling (ZIP and 7z):**
- ZIP and 7z files containing audio are processed as single audiobook entries (not individual files)
- Expected archive structure (same for .zip and .7z):
  ```
  audiobook.zip (or .7z)
  └── Author Name - Book Title/     ← top-level folder (author + title parsed from here)
      ├── Chapter 1/                ← subfolders = parts (for collections)
      │   ├── track1.mp3
      │   └── track2.mp3
      └── Chapter 2/
          └── track1.mp3
  ```
  OR for simple audiobooks:
  ```
  audiobook.zip (or .7z)
  └── Author Name - Book Title/     ← top-level folder
      ├── 01 - Introduction.mp3     ← files directly = tracks
      ├── 02 - Chapter 1.mp3
      └── 03 - Chapter 2.mp3
  ```
- Author and title parsed from **top-level folder inside archive** (not archive filename)
- Supports formats: "Author - Title", "Author_-_Title", "Author — Title"
- Year suffixes like "[2007]" or "(2007)" stripped from title
- Structure stored in `chapters` JSONB field:
  - Collections (has subfolders): `{"type": "collection", "parts": [...]}`
  - Simple audiobooks (flat files): `{"type": "book", "tracks": [...]}`
- Each track stores: name, duration (estimated from file size), size

**Audiobook Detail Page (`/web/audio/{id}`):**
- Tree view for collections: expandable parts with tracks inside
- Flat track list for simple audiobooks
- Duration shown for each track and part
- Download ZIP button
- Add to bookshelf button

**Database fields for audiobooks:**
- `duration_seconds` - Total duration
- `bitrate` - Audio bitrate
- `is_audiobook` - Boolean flag
- `track_count` - Number of tracks
- `chapters` - JSONB chapter/structure data
- `bauthors.role` - 'author' or 'narrator'

**Scanner behavior:**
- ZIP/7z with audio files = ONE audiobook entry (not per-file)
- Author/title parsed from top-level folder inside archive (fallback: archive filename)
- Duration estimated from file sizes (bitrate assumptions: MP3=128kbps, M4B/M4A=64kbps, FLAC=800kbps, OGG/OPUS=96kbps)
- ZIP files use single transaction with `SET LOCAL synchronous_commit = off`
- One SELECT per ZIP to check existing books (instead of one per book)
- Multiple workers process ZIPs in parallel (configurable via `scanner.workers`)
- Recommended: 4-8 workers; more causes I/O saturation and lock contention

**Nokia AWB Audiobook Support:**
- AWB = AMR-WB (Adaptive Multi-Rate Wideband) audio codec from Nokia Audiobook Manager (~2008)
- AWB files treated as regular folder audiobooks (format="folder")
- INX index file (UTF-16 LE/BE) provides accurate track durations (AWB has no metadata tags)
- AWB→MP3 streaming conversion via ffmpeg (VBR ~190kbps, runs at ~430x realtime)
- Requires ffmpeg with libmp3lame; path configurable via `converters.ffmpeg`
- INX parser: `internal/scanner/inxparser.go`

## API Endpoints

The full route table lives in `internal/server/server.go` — grep `r.Get(`, `r.Post(`, `r.Route(` there for the source of truth. The notes below cover the parts that aren't obvious from reading the routes.
**OPDS (Atom feeds):**
- `GET /opds/` - Main menu
- `GET /opds/catalogs` - Browse by folders
- `GET /opds/catalogs/{id}` - Folder contents
- `GET /opds/authors` - Authors list
- `GET /opds/authors/{id}` - Author's books
- `GET /opds/genres` - Genres list
- `GET /opds/genres/{id}` - Genre's books
- `GET /opds/series` - Series list
- `GET /opds/series/{id}` - Series books
- `GET /opds/new` - New books
- `GET /opds/search?q=query` - Search
- `GET /opds/book/{id}/download` - Download book
- `GET /opds/book/{id}/cover` - Book cover
- `GET /opds/book/{id}/epub` - Convert to EPUB
- `GET /opds/book/{id}/mobi` - Convert to MOBI

**`GET /web/search` query parameters** (the language/scope distinction is the trap):
- `q=title` — search in book title
- `author=name` — search in author first+last name
- `desc=1` — also search inside annotation text
- `lang=uk` — **exact** language match (used for scoped search within a language page)
- `lang_pattern=uk` — language **ILIKE pattern** match (used by the free filter dropdown)
- `genre_pattern=comedy`, `series_pattern=Silo` — ILIKE patterns
- `author_id=…`, `genre_id=…`, `series_id=…`, `catalog_id=…` — hidden fields injected when searching from a scoped page (e.g. an author's detail page); restrict results to that entity

**Auth Pages:**
- `GET /web/landing` - Landing page (unauthenticated)
- `GET|POST /web/login` - Login page
- `GET|POST /web/register` - Registration page
- `GET /web/logout` - Logout (clears JWT cookie)
- `GET|POST /web/forgot-password` - Forgot password (5/hour limit)
- `GET|POST /web/reset-password?token=x` - Reset password with token
- `GET /web/verify-email?token=x` - Verify email
- `POST /web/guest` - Continue as guest

**Auth API rate limits** (defined in `server.go`, may surprise you):
- `GET /api/auth/check-username` — 150/min
- `GET /api/auth/check-email` — 150/min
- `GET /api/auth/check-password` — no limit (no DB lookup)
- `POST /web/forgot-password` — 5/hour per IP

**Web UI:**
- `GET /web/` - Home page
- `GET /web/search` - Search with parameters:
  - `q=title` - Search in book title
  - `author=name` - Search in author first+last name
  - `desc=1` - Include annotation in title search
  - `lang=uk` - Filter by language (exact match, for scoped search)
  - `lang_pattern=uk` - Filter by language pattern (ILIKE)
  - `genre_pattern=comedy` - Filter by genre name pattern
  - `series_pattern=Silo` - Filter by series name pattern
  - `author_id=123` - Scope to author (hidden field)
  - `genre_id=456` - Scope to genre (hidden field)
  - `series_id=789` - Scope to series (hidden field)
  - `catalog_id=101` - Scope to catalog (hidden field)
- `GET /web/authors` - Authors
- `GET /web/authors/{id}` - Author's books
- `GET /web/genres` - Genres
- `GET /web/genres/{id}` - Genre's books
- `GET /web/series` - Series
- `GET /web/series/{id}` - Series books
- `GET /web/languages` - Languages
- `GET /web/languages/{lang}` - Language's books
- `GET /web/new` - New books
- `GET /web/audio` - Audiobooks browser with filters
- `GET /web/audio/{id}` - Audiobook detail with track list, checkboxes for selection
- `GET /web/audio/{id}/track?file=path` - Download individual track from archive
- `GET /web/catalogs` - Catalogs
- `GET /web/catalogs/{id}` - Catalog contents
- `GET /web/duplicates/{id}` - View all duplicates of a book
- `GET /web/bookshelf` - User's bookshelf
- `POST /web/bookshelf/add/{id}` - Add book to bookshelf
- `POST /web/bookshelf/remove/{id}` - Remove book from bookshelf
- `GET /web/read/{id}` - Web-based reader for FB2/EPUB/MOBI books
- `GET /web/help` - Help page (supports `?lang=en|uk`)
**Other route conventions:**
- `/opds/...` and `/web/...` are parallel: any browse view (authors, genres, series, catalogs, languages, new, search, audio) exists at both prefixes
- `/web/audio/{id}/track?file=path` streams a single track *out of an archive* without extracting it; `path` is the archive-internal path
- `/web/duplicates/{id}` shows every book that links to the given book via `duplicate_of`

## Requirements

- Go 1.21+
- PostgreSQL 12+
- Calibre (optional, for MOBI conversion)

## Dependencies

```
github.com/go-chi/chi/v5      # HTTP router
gorm.io/gorm                  # ORM framework
gorm.io/driver/postgres       # PostgreSQL driver for GORM
github.com/spf13/cobra        # CLI framework
github.com/robfig/cron/v3     # Cron scheduler
github.com/google/uuid        # UUID generation
gopkg.in/yaml.v3              # YAML parser
github.com/dhowden/tag        # Audio metadata (ID3, MP4, FLAC, OGG)
github.com/golang-jwt/jwt/v5  # JWT authentication
github.com/bodgit/sevenzip    # 7z archive support
github.com/yalue/onnxruntime_go # ONNX Runtime bindings (TTS inference)
```
