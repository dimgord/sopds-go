# SOPDS-Go

Go implementation of Simple OPDS Catalog Server for managing and serving e-book collections.

**Version: 0.31**

## Features

- **OPDS 1.2** catalog server compatible with Moon+ Reader, FBReader, Aldiko, Calibre
- **Modern Web UI** with responsive design, Font Awesome icons, search, and navigation
- **FB2 to EPUB/MOBI conversion** - EPUB is pure Go, MOBI uses Calibre
- **Parallel library scanning** with configurable workers and progress tracking
- **FB2 metadata extraction** (title, authors, genres, series, annotations, covers)
- **ZIP archive scanning** for compressed book collections
- **PostgreSQL database** with embedded migrations
- **MySQL migration tool** - import existing SOPDS MySQL database
- **Basic HTTP authentication** support
- **Scheduled automatic scanning** (cron format)
- **Hierarchical navigation** - letter-based drill-down for large collections
- **Personal bookshelf** - save books to your reading list
- **Duplicate detection** - automatic marking of duplicate books during scan
- **Duplicates viewer** - browse all versions of a book to compare formats/sizes
- **Browse by language** - filter books by language

## Quick Start

```bash
# Build
go build -o sopdsgo ./cmd/sopds

# Initialize database (as postgres superuser)
psql -U postgres -f init.sql

# Create config file
./sopdsgo init

# Edit config.yaml with your settings
vim config.yaml

# Run migrations
./sopdsgo migrate

# Start server
./sopdsgo start
```

Access the interfaces:
- **Web UI**: http://localhost:8081/web/
- **OPDS**: http://localhost:8081/opds/

## Requirements

- Go 1.21+
- PostgreSQL 12+
- Calibre (optional, for MOBI conversion)

## Installation

### From Source

```bash
git clone <repository>
cd sopds-go
go build -o sopdsgo ./cmd/sopds
```

### Database Setup

```bash
# Create database and user (run as postgres superuser)
psql -U postgres -f init.sql

# Run migrations
./sopdsgo migrate
```

### Install Calibre (for MOBI conversion)

```bash
# Fedora
sudo dnf install calibre

# Ubuntu/Debian
sudo apt install calibre
```

## Configuration

Create `config.yaml` (or run `./sopdsgo init`):

```yaml
database:
  host: localhost
  port: 5432
  name: sopds
  user: sopds
  password: sopds
  sslmode: disable

library:
  root: /path/to/your/books
  formats:
    - .fb2
    - .epub
    - .mobi
    - .pdf
    - .djvu
  scan_zip: true
  rescan_zip: false

server:
  bind: 0.0.0.0
  port: 8081
  opds_prefix: /opds
  web_prefix: /web
  auth:
    enabled: false
    users:
      - username: guest
        password: guest

scanner:
  workers: 4
  schedule: "0 3 * * *"  # Cron format: 3 AM daily
  on_start: false
  duplicates: normal     # none, normal, strong, clear

site:
  title: "SOPDS Library"

converters:
  fb2toepub: ""              # Not used (pure Go)
  fb2tomobi: "ebook-convert" # Calibre path
  temp_dir: /tmp
```

## Commands

| Command | Description |
|---------|-------------|
| `./sopdsgo start` | Start the server |
| `./sopdsgo stop` | Stop the server |
| `./sopdsgo status` | Check server status |
| `./sopdsgo scan` | Run manual library scan |
| `./sopdsgo migrate` | Run database migrations |
| `./sopdsgo init` | Create default config.yaml |
| `./sopdsgo import-mysql` | Import from MySQL SOPDS database |
| `./sopdsgo version` | Show version |

## Database Backup & Restore

```bash
# Backup
pg_dump -U sopds -Fc sopds > sopds_backup.dump

# Restore
pg_restore -U sopds -d sopds -c sopds_backup.dump

# Full restore (drop and recreate)
dropdb -U sopds sopds && createdb -U sopds sopds && pg_restore -U sopds -d sopds sopds_backup.dump
```

## MySQL Migration

Import data from existing MySQL SOPDS database:

```bash
./sopdsgo import-mysql \
  --mysql-host localhost \
  --mysql-user sopds \
  --mysql-password sopds \
  --mysql-db sopds \
  --clear  # Optional: clear PostgreSQL tables first
```

## API Endpoints

### OPDS Catalog (Atom feeds)

| Endpoint | Description |
|----------|-------------|
| `GET /opds/` | Main menu |
| `GET /opds/catalogs` | Browse by directory |
| `GET /opds/catalogs/{id}` | Catalog contents |
| `GET /opds/authors` | Authors list (with letter navigation) |
| `GET /opds/authors/{id}` | Author's books |
| `GET /opds/titles` | Books by title |
| `GET /opds/genres` | Genre sections |
| `GET /opds/genres/{id}` | Genre books |
| `GET /opds/series` | Series list (with letter navigation) |
| `GET /opds/series/{id}` | Series books |
| `GET /opds/new` | New books (last 7 days) |
| `GET /opds/search?q={query}` | Search books |
| `GET /opds/book/{id}/download` | Download book |
| `GET /opds/book/{id}/cover` | Book cover image |
| `GET /opds/book/{id}/epub` | Convert FB2 to EPUB |
| `GET /opds/book/{id}/mobi` | Convert FB2 to MOBI |

### Web Interface

| Endpoint | Description |
|----------|-------------|
| `GET /web/` | Home page with statistics |
| `GET /web/search?q={query}` | Search results |
| `GET /web/authors` | Authors (hierarchical letter navigation) |
| `GET /web/authors/{id}` | Author's books |
| `GET /web/genres` | Genre list |
| `GET /web/genres/{id}` | Genre books |
| `GET /web/series` | Series (hierarchical letter navigation) |
| `GET /web/series/{id}` | Series books |
| `GET /web/languages` | Languages list with book counts |
| `GET /web/languages/{lang}` | Books in language |
| `GET /web/new` | New books |
| `GET /web/catalogs` | Catalog folders |
| `GET /web/catalogs/{id}` | Catalog contents |
| `GET /web/duplicates/{id}` | View all duplicates of a book |
| `GET /web/bookshelf` | User's bookshelf |
| `POST /web/bookshelf/add/{id}` | Add book to bookshelf |
| `POST /web/bookshelf/remove/{id}` | Remove book from bookshelf |

## Project Structure

```
sopds-go/
├── cmd/sopds/
│   └── main.go              # CLI entry point
├── internal/
│   ├── config/
│   │   └── config.go        # YAML configuration
│   ├── converter/
│   │   └── converter.go     # FB2 to EPUB/MOBI conversion
│   ├── database/
│   │   ├── postgres.go      # PostgreSQL connection
│   │   ├── models.go        # Data models
│   │   ├── queries.go       # SQL queries
│   │   └── migrations/      # SQL migrations
│   ├── scanner/
│   │   ├── scanner.go       # Directory scanner
│   │   └── fb2parser.go     # FB2 metadata parser
│   ├── server/
│   │   ├── server.go        # HTTP server setup
│   │   ├── handlers.go      # OPDS handlers
│   │   └── web.go           # Web UI handlers & templates
│   └── opds/
│       └── feed.go          # OPDS feed generation
├── config.yaml              # Configuration
├── init.sql                 # Database initialization
├── go.mod
└── README.md
```

## Web Interface Screenshots

The web interface features:
- Modern gradient design with Font Awesome icons
- Library statistics (books, authors, genres, series)
- Quick navigation menu
- Search by title or author
- Hierarchical letter navigation (1→2→3 characters) for large author/series lists
- Download buttons for FB2, EPUB, MOBI formats
- Personal bookshelf to save books for later
- "Duplicates" button to view all versions of a book
- Browse by language with book counts
- Responsive layout for mobile devices

## License

Same as original SOPDS project.
