# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
│   ├── database/          # PostgreSQL layer with pgx, embedded migrations
│   ├── opds/              # OPDS Atom feed generation
│   ├── scanner/           # Parallel directory scanner, FB2 parser
│   └── server/            # HTTP server with chi router
│       ├── server.go      # Router setup, middleware
│       ├── handlers.go    # OPDS API handlers
│       └── web.go         # Web UI handlers and templates
└── config.yaml            # Main configuration file
```

### Build & Run

```bash
cd sopds-go
go build -o sopdsgo ./cmd/sopds

# Server management
./sopdsgo start       # Start server (foreground)
./sopdsgo stop        # Stop server
./sopdsgo status      # Check status
./sopdsgo scan        # Manual library scan
./sopdsgo init        # Create config.yaml template
./sopdsgo migrate     # Run database migrations
```

### Database Setup

```bash
# Create PostgreSQL database
psql -U postgres -c "CREATE USER sopds WITH PASSWORD 'sopds';"
psql -U postgres -c "CREATE DATABASE sopds OWNER sopds;"

# Or use init.sql
psql -U postgres -f init.sql

# Run migrations
./sopdsgo migrate
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
./sopdsgo import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds

# Clear PostgreSQL tables before import
./sopdsgo import-mysql --mysql-host localhost --mysql-user sopds --mysql-password sopds --mysql-db sopds --clear
```

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

converters:
  fb2toepub: ""               # Not used (pure Go converter)
  fb2tomobi: "ebook-convert"  # Calibre's ebook-convert path
  temp_dir: /tmp
```

### Web Interface

Access at `http://localhost:8081/web/`

Features:
- Modern responsive UI with Font Awesome icons
- Book search by title
- Browse by: Authors, Genres, Series, Catalogs, Languages
- Hierarchical letter navigation (1-char → 2-char → 3-char drill-down)
- Download books in original format
- Convert FB2 to EPUB/MOBI on-the-fly
- New books section (last 7 days)
- Personal bookshelf (add/remove books)
- Duplicate detection with "See duplicates" links to view all versions of a book

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
- `GET /web/search?q=query` - Search
- `GET /web/authors` - Authors
- `GET /web/authors/{id}` - Author's books
- `GET /web/genres` - Genres
- `GET /web/genres/{id}` - Genre's books
- `GET /web/series` - Series
- `GET /web/series/{id}` - Series books
- `GET /web/languages` - Languages
- `GET /web/languages/{lang}` - Language's books
- `GET /web/new` - New books
- `GET /web/catalogs` - Catalogs
- `GET /web/catalogs/{id}` - Catalog contents
- `GET /web/duplicates/{id}` - View all duplicates of a book
- `GET /web/bookshelf` - User's bookshelf
- `POST /web/bookshelf/add/{id}` - Add book to bookshelf
- `POST /web/bookshelf/remove/{id}` - Remove book from bookshelf

### Requirements

- Go 1.21+
- PostgreSQL 12+
- Calibre (optional, for MOBI conversion)

### Dependencies

```
github.com/go-chi/chi/v5      # HTTP router
github.com/jackc/pgx/v5       # PostgreSQL driver
github.com/spf13/cobra        # CLI framework
github.com/robfig/cron/v3     # Cron scheduler
github.com/google/uuid        # UUID generation
gopkg.in/yaml.v3              # YAML parser
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
