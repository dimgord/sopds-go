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

---

## Project Overview

Simple OPDS Catalog (SOPDS) is an OPDS (Open Publication Distribution System) server for managing and serving e-book collections. It provides OPDS/Web catalog access, automatic book scanning, metadata extraction from FB2 files, and format conversion.

**Two implementations exist:**
- **Python version** (`py/`) - Original, uses MySQL
- **Go version** (`sopds-go/`) - Rewrite, uses PostgreSQL

---

## Go Version (`sopds-go/`)

### Architecture

```
sopds-go/
├── cmd/sopds/main.go      # CLI entry point with cobra
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
│   │   └── repository/    # Repository interfaces
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
│   │   └── audiobookgrouper.go # Multi-file audiobook grouping
│   └── server/            # HTTP server with chi router
│       ├── server.go      # Router setup, middleware
│       ├── handlers.go    # OPDS API handlers
│       └── web.go         # Web UI handlers and templates
└── config.yaml            # Main configuration file
```

### Build & Run

```bash
cd sopds-go
go build -o sopds ./cmd/sopds

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

### Database Setup

```bash
# Create PostgreSQL database
psql -U postgres -c "CREATE USER sopds WITH PASSWORD 'sopds';"
psql -U postgres -c "CREATE DATABASE sopds OWNER sopds;"

# Or use init.sql
psql -U postgres -f init.sql

# Run migrations
./sopds migrate
```

### Database Backup & Restore

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

### MySQL to PostgreSQL Migration

Import data from existing MySQL SOPDS database:

```bash
# Basic import
./sopds import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds

# Clear PostgreSQL tables before import
./sopds import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds --clear
```

### Database Maintenance

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

### Configuration

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
    enabled: false
    users:
      - username: admin
        password: admin

scanner:
  workers: 4                  # Parallel scan workers
  on_start: false             # Scan on server start
  schedule: ""                # Cron schedule (e.g., "0 2 * * *")
  duplicates: normal          # none, normal, strong, clear

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

### Web Interface

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

**Internationalization (i18n):**
- Switch language via URL param: `?lang=en` or `?lang=uk`
- Language preference saved in cookie (1 year)
- To add a new language, edit `internal/server/web.go`:
  1. Add to `supportedLanguages` slice
  2. Add translations to `translations` map

### OPDS Interface

Access at `http://localhost:8081/opds/`

Compatible with OPDS readers like:
- Moon+ Reader
- FBReader
- Aldiko
- Calibre

### Format Conversion

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

### Audiobook Support

Supports audio formats: MP3, M4B, M4A, FLAC, OGG, OPUS

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

### API Endpoints

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
- `GET /web/help` - Help page (supports `?lang=en|uk`)

### Requirements

- Go 1.21+
- PostgreSQL 12+
- Calibre (optional, for MOBI conversion)

### Dependencies

```
github.com/go-chi/chi/v5      # HTTP router
gorm.io/gorm                  # ORM framework
gorm.io/driver/postgres       # PostgreSQL driver for GORM
github.com/spf13/cobra        # CLI framework
github.com/robfig/cron/v3     # Cron scheduler
github.com/google/uuid        # UUID generation
gopkg.in/yaml.v3              # YAML parser
github.com/dhowden/tag        # Audio metadata (ID3, MP4, FLAC, OGG)
github.com/bodgit/sevenzip    # 7z archive support
```

---

## Python Version (`py/`) - Legacy

### Core Components
- **sopdsd.py** - Main daemon entry point
- **sopdscfg.py** - Configuration parser (`conf/sopds.conf`)
- **sopdsdb.py** - MySQL database layer
- **sopdscan.py** - Book collection scanner
- **sopdsparse.py** / **fb2parse.py** - FB2 metadata extraction
- **sopdserve.py** - Built-in HTTP OPDS server
- **sopds.cgi** / **sopds.wsgi** - CGI/WSGI entry points

### Commands

```bash
cd py
./sopdsd.py start     # Start daemon
./sopdsd.py stop      # Stop daemon
./sopdsd.py restart   # Restart daemon
./sopdsd.py status    # Check status
./sopds-scan.py       # Manual scan
```

### MySQL Database Setup

```bash
mysql -uroot -p mysql
> CREATE DATABASE IF NOT EXISTS sopds DEFAULT CHARSET=utf8;
> GRANT ALL ON sopds.* TO 'sopds'@'localhost' IDENTIFIED BY 'sopds';
mysql -usopds -psopds sopds < db/tables.sql
mysql -usopds -psopds sopds < db/genres.sql
```

### Requirements
- Python 3.3+
- MySQL 5+
- mysql-connector-python3
