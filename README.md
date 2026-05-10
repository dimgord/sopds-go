# SOPDS-Go

[![Latest Release](https://img.shields.io/github/v/release/dimgord/sopds-go?display_name=tag&sort=semver)](https://github.com/dimgord/sopds-go/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/dimgord/sopds-go)](go.mod)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dimgord/sopds-go/ci.yml?branch=main)](https://github.com/dimgord/sopds-go/actions)

Go implementation of **Simple OPDS Catalog Server** — manage and serve large e-book collections via OPDS feeds and a modern web UI. Designed for self-hosted home libraries: scans nested ZIP archives of FB2 books, extracts metadata, generates EPUB/MOBI on the fly, and even produces audiobooks via local TTS.

A modernized Go rewrite of the original **Simple OPDS** Python project (v0.23) by **Dmitry V. Shelepnev** (Дмитрий Шелепнёв, © 2014; contact: admin@sopds.ru). The upstream homepage (`www.sopds.ru`) and any related public source repositories appear to be offline as of 2026. PostgreSQL persistence (vs. MySQL upstream), parallel scanning, GPU-accelerated TTS, and a redesigned responsive UI.

## Features

### Catalog & Browsing
- **OPDS 1.2** catalog server, compatible with Moon+ Reader, FBReader, Aldiko, Calibre
- **Modern Web UI** with responsive design, Font Awesome icons, full-text search
- **Hierarchical navigation** — letter-based drill-down (1→2→3 chars) for large author/series lists
- **Browse by language**, by genre, by series, by new arrivals (last 7 days)
- **Personal bookshelf** — save books to a reading list
- **Duplicate detection + viewer** — automatic marking + side-by-side comparison

### Library Management
- **Parallel scanning** with configurable workers + progress tracking
- **FB2 metadata extraction** — title, authors, genres, series, annotations, covers
- **Nested ZIP archive scanning** — common format for book corpora
- **PostgreSQL** persistence with embedded migrations
- **Scheduled rescans** in cron format
- **MySQL migration tool** — drop-in import from the original Python SOPDS database

### Conversion & TTS
- **FB2 → EPUB** — pure Go, no external dependencies
- **FB2 → MOBI** — via Calibre (`ebook-convert`)
- **FB2 → MP3 audiobook** — via Piper TTS, with optional CUDA GPU acceleration through the [Rust subproject](#rust-subprojects)
- **Multi-speaker / multi-language voices** — Ukrainian, Russian, English, more

### Operations
- **Basic HTTP authentication** support
- **Static binaries** for Linux + macOS, amd64/arm64 (see [Releases](https://github.com/dimgord/sopds-go/releases))
- **Docker image** at `ghcr.io/dimgord/sopds-go`
- **Nix flake** — `nix run github:dimgord/sopds-go`

## Quick Start

```bash
# Build
go build -o sopds ./cmd/sopds

# Initialize database (as postgres superuser)
psql -U postgres -f init.sql

# Create config file
./sopds init

# Edit config.yaml with your settings
vim config.yaml

# Run migrations
./sopds migrate

# Start server
./sopds start
```

Access the interfaces:
- **Web UI**: http://localhost:8081/web/
- **OPDS**: http://localhost:8081/opds/

## Requirements

- **Go 1.24+** for building from source
- **PostgreSQL 14+** for persistence
- **Calibre** (`ebook-convert`) — optional, for MOBI conversion
- **Espeak-ng** + Piper voice models — optional, for TTS / audiobooks
- **NVIDIA GPU + CUDA 12** — optional, for accelerated TTS via [`sopds-tts-rs`](sopds-tts-rs/)

## Installation

### Pre-built binaries (recommended)

Grab the latest release for your platform from [GitHub Releases](https://github.com/dimgord/sopds-go/releases) — Linux/macOS, amd64/arm64.

```bash
# Linux x86_64 example
curl -LO https://github.com/dimgord/sopds-go/releases/latest/download/sopds-go_linux_amd64.tar.gz
tar xzf sopds-go_linux_amd64.tar.gz
sudo install sopds /usr/local/bin/
```

### Docker

```bash
docker run -d \
  --name sopds \
  -p 8081:8081 \
  -v /path/to/your/books:/library:ro \
  -v /path/to/config.yaml:/etc/sopds/config.yaml:ro \
  ghcr.io/dimgord/sopds-go:latest
```

### Nix flake

```bash
# Run directly without installing
nix run github:dimgord/sopds-go -- start

# Add to a flake.nix
inputs.sopds-go.url = "github:dimgord/sopds-go";
```

### From source

```bash
git clone https://github.com/dimgord/sopds-go.git
cd sopds-go
go build -o sopds ./cmd/sopds
```

### Database Setup

```bash
# Create database and user (run as postgres superuser)
psql -U postgres -f init.sql

# Run migrations
./sopds migrate
```

### Install Calibre (for MOBI conversion)

```bash
# Fedora
sudo dnf install calibre

# Ubuntu/Debian
sudo apt install calibre
```

## Configuration

Create `config.yaml` (or run `./sopds init`):

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
| `./sopds start` | Start the server |
| `./sopds stop` | Stop the server |
| `./sopds status` | Check server status |
| `./sopds scan` | Run manual library scan |
| `./sopds migrate` | Run database migrations |
| `./sopds init` | Create default config.yaml |
| `./sopds import-mysql` | Import from MySQL SOPDS database |
| `./sopds version` | Show version |

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
./sopds import-mysql \
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
├── cmd/sopds/main.go         # CLI entry point (cobra)
├── internal/
│   ├── config/               # YAML configuration loader
│   ├── converter/            # FB2 → EPUB (pure Go), FB2 → MOBI (Calibre)
│   ├── database/             # PostgreSQL: connection, models, queries, migrations
│   ├── scanner/              # Library walker, FB2 parser, ZIP-archive walker
│   ├── server/               # HTTP server (chi), OPDS handlers, Web UI handlers
│   ├── opds/                 # Atom-feed generator
│   ├── tts/                  # Piper TTS integration (Go path)
│   └── infrastructure/       # Persistence layer abstractions
├── sopds-tts-rs/             # Rust port of TTS — CUDA-accelerated (see below)
├── zipdupes-rs/              # Rust port of FB2-archive deduplicator
├── fb2converters/            # Drop-in directory for additional converter binaries
├── config.yaml.example       # Example configuration
├── init.sql                  # Database initialization
├── Taskfile.yml              # task-runner recipes (build / test / docker / etc.)
├── go.mod
└── README.md
```

## Rust subprojects

Two CLI utilities have Rust ports for performance/GPU reasons. Both are CLI-compatible drop-in replacements; the Go originals remain in-tree as fallbacks.

### `sopds-tts-rs/` — CUDA-accelerated TTS

Drop-in for `sopds-tts`. Runs Piper ONNX models via [`ort`](https://github.com/pykeio/ort) (Rust ONNX Runtime binding) with the **CUDA execution provider** for dramatic speedup on systems with NVIDIA GPUs. Pascal-class GPUs (sm_61, e.g. GTX 1070) are explicitly supported via a pinned cuDNN 9.8 in the flake.

```bash
cd sopds-tts-rs
nix develop ./       # first time: ~15-30 min (ORT source build with CUDA EP)
cargo build --release
./target/release/sopds-tts-rs <model> <output> < text.txt
```

Wire it into sopds-go's pipeline by setting `tts.binary: /path/to/sopds-tts-rs` in `config.yaml`.

### `zipdupes-rs/` — Rust port of FB2-archive duplicate finder

Same CLI as Go `zipdupes`. Significantly faster on cold-cache scans of large book corpora (tested on a 1TB+ FB2 ZIP archive collection). Use whichever you prefer.

```bash
cd zipdupes-rs
cargo build --release
./target/release/zipdupes /path/to/archive/dir
```

## Web UI

Modern responsive layout with Font Awesome icons, gradient header, library statistics on the home page, full-text + hierarchical navigation, download buttons for all supported formats, personal bookshelf, duplicates browser, and language filtering. Optimized for both desktop and mobile screens.

## Related projects

- **[fbe-go](https://github.com/dimgord/fbe-go)** — FictionBook editor. Same author, same FB2 ecosystem, complementary role: sopds-go *serves* books; fbe-go *edits* them. Native macOS/Linux desktop app (Wails + Svelte + ProseMirror) with full FB2 round-trip + XSD validation. Use it to fix/enrich metadata before adding files to your sopds-go library.

## License

**[GNU Affero General Public License v3.0](LICENSE)** (AGPL-3.0).

AGPL extends GPL with one important provision: if you run a modified version
of sopds-go as a network-accessible service (e.g. a public OPDS catalog or
web UI), you must offer the modified source to your users. Self-hosting for
personal use is unrestricted.

The original **Simple OPDS** Python implementation by Dmitry V. Shelepnev
(Дмитрий Шелепнёв, © 2014, admin@sopds.ru) is licensed under
**GPL-3.0-or-later** ("any later version" clause). This Go rewrite
follows the same copyleft principle, upgraded to AGPL-3.0 specifically
because OPDS catalogs are inherently network services. The "or any
later version" wording in the original license is what makes the
upgrade to AGPL-3.0 cleanly compatible.
