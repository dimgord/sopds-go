# PROGRESS.md

## Project: Simple OPDS Catalog (SOPDS) - Go Version
## Current Version: 0.31

---

### Revision 10 - 2026-01-03
**Changes:**
- Added "See duplicates" feature to view all versions of a book
- Books with duplicates show a "Duplicates (N)" button linking to duplicates page
- Duplicate books show "Duplicates" button to find original and other copies
- Duplicates page displays original book and all its duplicates for easy comparison

**Files Modified:**
- `internal/database/queries.go` - Added GetDuplicateCount, GetBookDuplicates functions
- `internal/server/web.go` - Added DuplicateOf/DuplicateCount to BookView, handleWebDuplicates handler, updated templates
- `internal/server/server.go` - Added /web/duplicates/{id} route
- `CLAUDE.md` - Updated documentation with new features and endpoints

---

### Revision 9 - 2025-12-26
**Changes:**
- Implemented cover image extraction from FB2 files (base64 decoding)
- Implemented bookshelf feature (user reading lists)
- Added web bookshelf page with add/remove functionality
- Added "Add to Bookshelf" button on all book listings
- Cover images served from `/book/{id}/cover` endpoint

**Files Modified:**
- `internal/server/handlers.go` - Added cover extraction, bookshelf handlers
- `internal/server/web.go` - Added bookshelf template, TotalCount field, button styles
- `internal/database/queries.go` - Added bookshelf database functions
- `internal/server/server.go` - Added bookshelf routes

---

### Revision 8 - 2025-12-26
**Changes:**
- Updated all documentation files (CLAUDE.md, README.md, PROGRESS.md)
- Added comprehensive documentation for all features

**Files Modified:**
- `CLAUDE.md` - Full rewrite with architecture, commands, API endpoints
- `README.md` - Updated with all new features and configuration options
- `PROGRESS.md` - Added all revision history

---

### Revision 7 - 2025-12-26
**Changes:**
- Implemented FB2 to EPUB converter in pure Go (`internal/converter/`)
- Implemented FB2 to MOBI conversion using Calibre's ebook-convert
- Wired converters to server handlers (`/book/{id}/epub`, `/book/{id}/mobi`)
- EPUB conversion includes: metadata, body content, images, TOC, stylesheet

**Files Created:**
- `internal/converter/converter.go`

**Files Modified:**
- `internal/server/server.go` - Added converter initialization
- `internal/server/handlers.go` - Implemented handleConvertEPUB, handleConvertMOBI
- `go.mod`, `go.sum` - Added github.com/google/uuid dependency

---

### Revision 6 - 2025-12-26
**Changes:**
- Complete Web UI rewrite with modern design
- Fixed download 404 error (OPDSPrefix in templates)
- Fixed pagination 404 error (WebPrefix in URLs)
- Added EPUB/MOBI download buttons (conditional on config)
- Implemented hierarchical letter navigation (1→2→3 char drill-down)
- Added cloud-style prefix buttons for author/series navigation
- Modern CSS with gradients, Font Awesome 6 icons, responsive design
- Fixed template field names (Stats.BooksCount instead of Stats.Books)

**Files Modified:**
- `internal/server/web.go` - Complete rewrite
- `internal/database/queries.go` - Added GetAuthorsByPrefix, GetSeriesByPrefix

---

### Revision 5 - 2025-12-26
**Changes:**
- Added MySQL to PostgreSQL migration command (`import-mysql`)
- Fixed MySQL connection timeout during large imports (read all data to memory first)
- Fixed PostgreSQL constraint violations (drop/recreate FK constraints during import)
- Added `--clear` flag to clear PostgreSQL tables before import
- Added progress reporting during migration

**Files Modified:**
- `cmd/sopds/main.go` - Added import-mysql command with all migration functions

---

### Revision 4 - 2025-12-25
**Changes:**
- Added progress tracking and ETA to library scanner
- Added `ProgressInfo` struct with: TotalFiles, ProcessedFiles, BooksAdded, BooksSkipped, Elapsed, Rate, ETA, Phase
- Added `ProgressCallback` for CLI progress display
- Pre-count phase before scanning for accurate progress
- Progress bar display with spinner animation

**Files Modified:**
- `internal/scanner/scanner.go` - Added progress tracking
- `cmd/sopds/main.go` - Added progress display for scan command

---

### Revision 3 - 2025-12-25
**Changes:**
- Added `sopds-go/README.md` with full documentation
  - Quick start guide
  - Configuration reference
  - API endpoints
  - Project structure

**Files Modified:**
- `sopds-go/README.md` (new)

---

### Revision 2 - 2025-12-25
**Changes:**
- Added `init.sql` for PostgreSQL database initialization
  - Creates database and user
  - Grants schema permissions (required for PostgreSQL 15+)
- Fixed `002_genres.sql` - escaped single quotes in Russian text

**Files Modified:**
- `sopds-go/init.sql` (new)
- `sopds-go/internal/database/migrations/002_genres.sql` (fixed)

---

### Revision 1 - 2025-12-25
**Changes:**
- Complete rewrite of SOPDS from Python to Go
- New technology stack:
  - Go 1.21+ with PostgreSQL (replacing MySQL)
  - YAML configuration format (replacing INI)
  - chi router for HTTP
  - pgx for PostgreSQL
  - encoding/xml for OPDS feeds

**New Files Created (sopds-go/):**
- `cmd/sopds/main.go` - CLI with cobra (start/stop/status/scan/migrate/init)
- `internal/config/config.go` - YAML configuration parser
- `internal/database/postgres.go` - PostgreSQL connection pool
- `internal/database/models.go` - Data models (Book, Author, Genre, Series, etc.)
- `internal/database/queries.go` - All database queries
- `internal/database/migrations/001_initial.sql` - PostgreSQL schema
- `internal/database/migrations/002_genres.sql` - Genre data
- `internal/scanner/scanner.go` - Parallel directory scanner with goroutines
- `internal/scanner/fb2parser.go` - FB2 metadata extraction
- `internal/server/server.go` - HTTP server with chi router
- `internal/server/handlers.go` - OPDS/Web request handlers
- `internal/opds/feed.go` - OPDS Atom feed generation
- `config.yaml` - Default configuration template
- `go.mod`, `go.sum` - Go module files
- `CLAUDE.md` - Project documentation for Claude Code
- `PROGRESS.md` - Development progress tracking

**Key Improvements over Python version:**
- Parallel scanning with configurable worker count
- RESTful routes instead of `?id=` parameter encoding
- Proper graceful shutdown with signal handling
- Embedded SQL migrations
- Type-safe YAML configuration
- Modern Web UI with responsive design
- Built-in FB2 to EPUB conversion (pure Go)
- MySQL migration tool for existing databases

---

## Feature Summary

| Feature | Status |
|---------|--------|
| OPDS 1.2 Catalog | Done |
| Web Interface | Done |
| FB2 Metadata Extraction | Done |
| ZIP Archive Scanning | Done |
| Parallel Scanning | Done |
| Progress Tracking | Done |
| Basic Auth | Done |
| Scheduled Scanning | Done |
| FB2 to EPUB | Done |
| FB2 to MOBI | Done |
| MySQL Migration | Done |
| PostgreSQL Backup/Restore | Done |
| Cover Images | Done |
| Duplicate Detection | Done |
| Duplicates Viewer | Done |
| Bookshelf | Done |

---

## Database Tables

| Table | Description |
|-------|-------------|
| books | Book metadata and file info |
| authors | Author names |
| bauthors | Book-author relationships |
| genres | Genre definitions |
| bgenres | Book-genre relationships |
| series | Series names |
| bseries | Book-series relationships |
| catalogs | Directory structure |
| bookshelf | User reading lists |
