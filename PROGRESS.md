# PROGRESS.md

## Project: Simple OPDS Catalog (SOPDS) - Go Version
## Current Version: 0.39

---

### Revision 21 - 2026-01-10
**Changes:**
- Fixed audiobook filter showing all authors instead of only audiobook authors
  - `GetFilterOptions` was missing `AudioOnly` filter application
  - Added `query.Filters.WithAudioOnly()` when `opts.AudioOnly` is true
  - Fixes Chrome crash on mobile when opening author filter dropdown

**Files Modified:**
- `internal/infrastructure/persistence/service.go` - Added AudioOnly filter to GetFilterOptions

---

### Revision 20 - 2026-01-08
**Changes:**
- Implemented file logging from config with rolling support
  - `logging.file` - log file path
  - `logging.max_size` - max size in MB before rotation (default 10)
  - `logging.max_backups` - number of old log files to keep (default 3)
  - `rollingLogWriter` struct implements io.Writer with automatic rotation
  - Rotated files named `.1`, `.2`, `.3` etc (newest first)
  - Falls back to stderr with warning if file/rotation fails

**Files Modified:**
- `cmd/sopds/main.go` - Added `rollingLogWriter` type, `setupLogging()` function
- `internal/config/config.go` - Added `MaxSize`, `MaxBackups` to LoggingConfig
- `config.yaml` - Added `max_size`, `max_backups` settings

---

### Revision 19 - 2026-01-08
**Changes:**
- Fixed audiobook archive path bug
  - `Path` was incorrectly storing full path including filename instead of directory
  - Changed to `Path: filepath.Dir(relPath)` in both `processAudioZip` and `processAudio7z`
- Added individual track download from archives
  - New endpoint `/web/audio/{id}/track?file=path` serves individual files
  - `serveTrackFromZip()` and `serveTrackFrom7z()` helper functions
  - Tracks now store full path inside archive for download
- Audiobook detail page track selection UI
  - Select all checkbox with indeterminate state support
  - Part-level checkboxes for collections (multi-directory audiobooks)
  - Individual track checkboxes
  - "Download Selected" button triggers sequential downloads
  - Download link for each individual track
- Added `Path` field to `AudiobookTrack` struct for storing full path inside archive
- New translations: `audio.selectall`, `audio.downloadsel` (English and Ukrainian)
- CSS styles for select controls, checkboxes, and track download buttons
- Track count in book list now clickable - links to audiobook detail page `/web/audio/{id}`
  - Changed from `<span>` to `<a>` with `meta-link` class
  - Uses i18n `audio.tracks` translation instead of hardcoded "tracks"

**Files Modified:**
- `internal/scanner/scanner.go` - Added Path to AudiobookTrack, fixed archive path bug, updated trackInfo struct
- `internal/server/server.go` - Added `/audio/{id}/track` route
- `internal/server/web.go` - Added handleAudioTrackDownload handler, serveTrackFromZip, serveTrackFrom7z, Path field to AudioTrack, checkboxes/JS/CSS in audiodetail template, new translations, clickable track count in book list

---

### Revision 18 - 2026-01-08
**Changes:**
- Fixed book ID not returned after GORM upsert in `Save()` method
  - GORM populates `model.ID` via RETURNING clause, but domain book was not updated
  - Caused `book_id=0` to be passed to `AddBookAuthor`, resulting in FK constraint violations
  - Added `b.SetID(book.ID(model.ID))` after Create to propagate ID back to domain object
- Added audio formats to config.yaml (mp3, m4b, m4a, flac, ogg, opus)
- Audiobook titles now use folder name instead of filename (better metadata)
- Added "Audiobooks" menu item in web UI to browse all audiobooks
  - New `/web/audio` route with full filter support (language, author, genre)
  - Headphones icon in both upper navigation and menu grid
  - Translations for English and Ukrainian ("nav.audio", "main.audiobooks")
- Audiobook author parsing from topmost folder name
  - Parses "Author - Title" or "Author_-_Title" format
  - Splits author name into first_name/last_name
  - Strips year suffixes like "[2007]" or "(2007)" from title
  - Falls back to Unknown Author if no separator found
- ZIP audiobook grouping: ZIP with audio files = ONE audiobook entry
  - `isAudioZip()` detects ZIPs containing audio files
  - `processAudioZip()` creates single book entry with aggregated metadata
  - Author/title parsed from top-level folder inside ZIP
  - Total duration estimated from file sizes
  - Track count stored
- Audiobook detail page with tree view
  - `/web/audio/{id}` route shows audiobook structure
  - Displays parts/chapters for collections (multi-directory ZIPs)
  - Displays flat track list for simple audiobooks
  - Shows duration for each track/part
  - Download ZIP button, add to bookshelf
  - Modern collapsible UI with Font Awesome icons
- Scanner stores audiobook structure in `chapters` JSONB field
  - `AudiobookStructure`, `AudiobookPart`, `AudiobookTrack` types in scanner
  - Type: "book" for flat structure, "collection" for multi-directory
  - Each track stores name, duration, size
  - Each part stores name, total duration, list of tracks
- Fixed audiobook ZIP parsing to use top-level folder inside ZIP for author/title
  - Expected structure: `ZIP / TopFolder (Author - Title) / [SubFolders] / tracks`
  - Subfolders under top-level folder become parts/chapters
  - Files directly under top-level folder become tracks (simple audiobook)
- Added 7z archive support
  - New dependency: `github.com/bodgit/sevenzip`
  - `process7z()` - processes 7z archives same as ZIP
  - `isAudio7z()` - detects audio 7z files
  - `processAudio7z()` - handles audiobook 7z files
  - Same audiobook structure parsing as ZIP (top-level folder, parts, tracks)

**Files Modified:**
- `internal/infrastructure/persistence/book_repository.go` - Fixed Save() to update domain book ID
- `internal/scanner/scanner.go` - Parse author from topmost folder, AudioMeta struct, parseAudioFolderName(), parseAuthorFullName(), isAudioZip(), processAudioZip(), process7z(), isAudio7z(), processAudio7z(), AudiobookStructure/Part/Track types
- `internal/server/web.go` - Added handleWebAudio handler, handleWebAudioDetail handler, audiodetail template, AudiobookStructure/AudiobookPart/AudioTrack/AudioDetailData types, formatDuration template function, audio translations
- `internal/server/server.go` - Added /web/audio and /web/audio/{id} routes
- `internal/infrastructure/persistence/service.go` - Added AudioOnly to SearchOptions
- `internal/infrastructure/persistence/scopes.go` - Added OnlyAudiobooks scope
- `internal/domain/repository/filters.go` - Added AudioOnly filter and WithAudioOnly method
- `config.yaml` - Added audio formats to library.formats
- `go.mod`, `go.sum` - Added github.com/bodgit/sevenzip dependency

---

### Revision 17 - 2026-01-08
**Changes:**
- Fixed author race condition in concurrent scanner workers
  - Added unique constraint on `authors(first_name, last_name)`
  - Updated `GetOrCreate` to use `ON CONFLICT DO NOTHING` with re-fetch pattern
  - Fixed PostgreSQL sequence not incrementing after explicit ID insert in migration
- Increased `series.ser` column from 64 to 128 characters (some series names exceed 64)
- All FB2 metadata (authors, genres, series) now properly linked to books

**Files Created:**
- `internal/infrastructure/persistence/migrations/009_author_unique_constraint.sql`
- `internal/infrastructure/persistence/migrations/010_increase_series_name.sql`

**Files Modified:**
- `internal/infrastructure/persistence/author_repository.go` - Race-safe GetOrCreate with ON CONFLICT
- `internal/infrastructure/persistence/models.go` - SeriesModel.Name size:128

---

### Revision 16 - 2026-01-07
**Changes:**
- Added audiobook support with full metadata extraction
- Supports audio formats: MP3, M4B, M4A, FLAC, OGG, OPUS
- Audio metadata extraction using dhowden/tag library (ID3, MP4 atoms, Vorbis comments)
- Narrator tracking via author role ('author' vs 'narrator') in bauthors table
- Duration display for audiobooks (formatted as "Xh Ym")
- Track count display for multi-file audiobooks
- Web UI shows headphones icon badge for audiobooks
- OPDS feeds include proper audio MIME types
- Fixed missing unique constraint on (filename, path) for ON CONFLICT
- Fixed JSONB chapters column: changed from string to *string to allow NULL
- Added audiobook fields to domain Book and all conversion layers
- Fixed slow scan performance with multiple optimizations:
  - Enabled `SET LOCAL synchronous_commit = off` within transactions
  - All ZIP operations wrapped in single transaction (one commit per ZIP)
  - Batch availability updates for existing books
  - Added `GetBookMapByCatalog()` for 1-query existence check (replaces N queries per ZIP)
  - Added `Service.Transaction()` method for transactional batch operations
  - Recommended: keep `scanner.workers` at 4-8 to avoid I/O saturation
- Fixed bauthors FK constraint violation: filter out zero IDs from failed GORM upserts before linking authors
- Fixed missing FB2 author/genre/series linking (never implemented in Go version)
  - Added `bookWithMeta` struct to store parsed FB2 metadata alongside book records
  - ZIP batch processing now properly extracts and links authors, genres, and series
  - Individual file processing (`processFile`) now also links FB2 metadata
  - Added `parseFB2MetadataFull()` function that returns full metadata for linking
  - Falls back to Unknown Author only when no authors found in FB2 metadata

**New Database Fields (books table):**
- `duration_seconds` - Total duration in seconds
- `bitrate` - Audio bitrate
- `is_audiobook` - Boolean flag
- `track_count` - Number of tracks (for multi-file audiobooks)
- `chapters` - JSONB column for chapter data

**New Database Fields (bauthors table):**
- `role` - Author role ('author' or 'narrator')

**Files Created:**
- `internal/infrastructure/persistence/migrations/006_audiobook_support.sql` - Database migration for audio fields
- `internal/infrastructure/persistence/migrations/007_unique_filename_path.sql` - Unique constraint for upsert
- `internal/infrastructure/persistence/migrations/008_fix_search_trigger.sql` - Recreate trigger with UPDATE OF condition
- `internal/scanner/audioparser.go` - Audio metadata extraction
- `internal/scanner/audiobookgrouper.go` - Multi-file audiobook grouping logic

**Files Modified:**
- `internal/database/models.go` - Added audio fields to Book struct
- `internal/infrastructure/persistence/models.go` - Added GORM audio fields (*string for chapters), Role to BookAuthorModel
- `internal/domain/book/book.go` - Added audiobook fields, updated Reconstruct(), added getters
- `internal/domain/book/value_objects.go` - Added audio Format constants, IsAudio() method
- `internal/domain/repository/book_repository.go` - Added narrator methods
- `internal/infrastructure/persistence/book_repository.go` - Implemented narrator repository methods
- `internal/infrastructure/persistence/mappers.go` - Added audiobook fields to BookToModel/BookToDomain
- `internal/infrastructure/persistence/adapters.go` - Added audiobook fields to BookToLegacy
- `internal/infrastructure/persistence/database.go` - Added SetAsyncCommit(), SetSyncCommit() methods
- `internal/infrastructure/persistence/service.go` - Added narrator service methods, audiobook fields in Reconstruct calls, UpdateBookAvailBatch, Transaction() with SET LOCAL, GetBookMapByCatalog()
- `internal/infrastructure/persistence/book_repository.go` - Added UpdateAvailabilityBatch, GetBookMapByCatalog for batch operations
- `internal/domain/repository/book_repository.go` - Added UpdateAvailabilityBatch, GetBookMapByCatalog interface methods
- `internal/opds/feed.go` - Added audio MIME types and FormatDuration helper
- `internal/scanner/scanner.go` - Added audio file detection, metadata parsing, optimized ZIP scanning with GetBookMapByCatalog (1 query per ZIP), transaction batching
- `internal/server/web.go` - Updated BookView struct and templates for audiobook display
- `internal/config/config.go` - Added audio formats to defaults

**Web UI Changes:**
- Audiobooks display headphones icon next to title
- Duration shown instead of file size for audiobooks
- Narrators displayed with microphone icon
- Track count shown for multi-file audiobooks

---

### Revision 15 - 2026-01-07
**Changes:**
- Fixed field shadowing bug in i18n: `BooksData.Languages` renamed to `FilterLangs`
- The old field shadowed `PageData.Languages` causing "can't evaluate field Code in type string" error on search page
- Fixed page size, pagination and filters losing URL parameters
- Converted page size buttons to use JavaScript `setPageSize()` function
- Converted pagination links to use JavaScript `goToPage()` function
- Converted "Clear all" filters to use JavaScript `clearFilters()` function
- All navigation now properly preserves search queries and filter parameters
- Fixed fname/lname filters not working (were parsed but never applied)
- Added `FirstNameFilter` and `LastNameFilter` to SearchOptions
- Added `WithAuthorFirstNameExact` and `WithAuthorLastNameExact` scopes
- Added filters to all book listing pages (genre, author, series, language, new books)
- All book lists now support filtering by: language, author first/last name, genre
- Added `NewPeriod` to SearchOptions for new books page
- Updated CLAUDE.md with systemd service management commands
- Fixed filter dropdowns showing language codes instead of human-readable names
- Fixed filter dropdowns only showing options from current page (now shows ALL options in scope)
- Added `GetFilterOptions` method to query distinct filter values for entire scope
- Added `LangOption` struct with Code and Name fields for proper language display
- Added `langsToOptions()` and `genresToLinkedItems()` helper functions

**Filters by page type:**
- Genre page: lang, fname, lname filters (genre already scoped)
- Author page: lang, genre filters (author already scoped, fname/lname not applicable)
- Series page: lang, fname, lname, genre filters
- Language page: fname, lname, genre filters (lang already scoped)
- New books page: lang, fname, lname, genre filters

**Files Modified:**
- `internal/server/web.go` - Updated all book listing handlers to use GetFilterOptions, LangOption struct, helper functions
- `internal/infrastructure/persistence/service.go` - Added GetFilterOptions method, FilterOptions struct
- `internal/infrastructure/persistence/scopes.go` - Added exact name filter scopes
- `internal/domain/repository/filters.go` - Added AuthorFirstName/AuthorLastName fields and methods
- `CLAUDE.md` - Added systemd service commands (start/stop/restart/status)

---

### Revision 14 - 2026-01-04
**Changes:**
- Added internationalization (i18n) system for Web UI
- Supports English (en) and Ukrainian (uk) languages
- Extensible design - add new languages by editing supportedLanguages and translations map
- Language switcher in navigation bar (JavaScript-based, preserves current URL)
- Cookie-based language preference persistence (1 year)
- Full translation of all templates: main, search, books, bookshelf, authors, genres, series, catalogs, languages, error pages
- Help page with full translations

**Translated UI Elements:**
- Navigation menu
- Statistics labels
- Browse menu items
- Book listings (Show, Filters, All Languages, Previous, Next, No books found, Add to Shelf, Duplicates, Remove)
- Empty state messages (No authors/genres/series/languages/catalogs found)
- Bookshelf (title, empty message)

**i18n Architecture:**
- `Language` struct with Code and Name fields
- `supportedLanguages` slice - add new languages here
- `translations` map[string]map[string]string - key → lang → text
- `T(lang, key)` function for translation lookups
- Template function `{{t "key"}}` for inline translations
- `newPageData()` and `addI18n()` helpers initialize Lang and Languages fields
- All handlers call `setLangCookie()` and `addI18n()` for consistent language support

**Adding a New Language:**
1. Add to supportedLanguages: `{Code: "de", Name: "Deutsch"}`
2. Add translations for all keys in translations map

**Files Modified:**
- `internal/server/web.go` - i18n system, all templates translated, addI18n helper, handlers updated
- `internal/server/server.go` - Added /web/help route

---

### Revision 13 - 2026-01-04
**Changes:**
- Enhanced search with separate title and author name fields
- Added pattern filters for language, genre, and series (ILIKE matching)
- Added language scope for searching within a language context
- Schema migration: renamed `doublicat` to `duplicate_of`, `favorite` INT to BOOL
- Fixed duplicate count display (only show when count > 1)
- Added scoped search (search within current author/genre/series/catalog/language)

**New Search Features:**
- Separate title search (q=) and author search (author=) fields
- Both can be combined with AND logic
- Pattern filters via URL parameters:
  - `lang_pattern=uk` - filter by language pattern
  - `genre_pattern=comedy` - filter by genre name
  - `series_pattern=Silo` - filter by series name
- Language scope: when browsing by language, searches stay within that language

**Files Modified:**
- `internal/infrastructure/persistence/service.go` - SearchOptions with TitleQuery/AuthorQuery
- `internal/infrastructure/persistence/scopes.go` - Added WithAuthorName, WithLangPattern, WithGenrePattern, WithSeriesPattern
- `internal/domain/repository/filters.go` - Added AuthorNameQuery and pattern filter fields
- `internal/server/web.go` - Updated search form with author field, language scope
- `internal/server/handlers.go` - Updated OPDS search to use TitleQuery
- `internal/database/models.go` - Changed Duplicate/Favorite field types

**Migration:**
- `005_schema_cleanup.sql` - Renames doublicat to duplicate_of, converts favorite to boolean

---

### Revision 12 - 2026-01-04
**Changes:**
- Added PostgreSQL full-text search (FTS) for book searches
- Search is now 440x faster (4.75ms vs 2099ms)
- Uses tsvector column with GIN index
- Title weighted higher than annotation in search results
- Trigger auto-updates search vector on book insert/update

**Files Created:**
- `internal/infrastructure/persistence/migrations/004_fulltext_search.sql`

**Files Modified:**
- `internal/infrastructure/persistence/scopes.go` - WithKeywords uses FTS
- `internal/infrastructure/persistence/models.go` - Added SearchVector field
- `internal/infrastructure/persistence/database.go` - Multi-statement migrations

---

### Revision 11 - 2026-01-04
**Changes:**
- Migrated from raw pgx queries to GORM ORM with Domain-Driven Design (DDD)
- Created domain layer with entities: Book, Author, Genre, Series, Catalog
- Created repository interfaces in `internal/domain/repository/`
- Implemented GORM repositories in `internal/infrastructure/persistence/`
- Added Service layer as bridge between handlers and repositories
- Moved SQL migrations to persistence package
- Removed 1,500+ lines of raw SQL queries

**Architecture:**
```
internal/
├── domain/                    # Pure business logic
│   ├── book/                  # Book aggregate with value objects
│   ├── author/                # Author entity
│   ├── genre/                 # Genre entity
│   ├── series/                # Series entity
│   ├── catalog/               # Catalog entity
│   └── repository/            # Repository interfaces
│
├── infrastructure/persistence/ # GORM implementations
│   ├── database.go            # GORM connection + migrations
│   ├── models.go              # GORM models with tags
│   ├── mappers.go             # Domain <-> Model conversion
│   ├── scopes.go              # Reusable query scopes
│   ├── *_repository.go        # Repository implementations
│   ├── adapters.go            # Domain <-> Legacy type conversion
│   └── service.go             # Bridge layer for handlers
│
└── database/                  # Legacy (models only)
    └── models.go              # Legacy types for handlers
```

**Files Created:**
- `internal/domain/book/book.go` - Book aggregate root
- `internal/domain/book/value_objects.go` - Format, Language, Availability, Cover
- `internal/domain/author/author.go` - Author entity
- `internal/domain/genre/genre.go` - Genre entity
- `internal/domain/series/series.go` - Series entity
- `internal/domain/catalog/catalog.go` - Catalog entity
- `internal/domain/repository/*.go` - Repository interfaces
- `internal/infrastructure/persistence/*.go` - GORM implementations

**Files Removed:**
- `internal/database/postgres.go` - Replaced by persistence/database.go
- `internal/database/queries.go` - Replaced by repositories

**Files Modified:**
- `cmd/sopds/main.go` - Uses persistence.NewDB and persistence.NewService
- `internal/server/server.go` - Uses persistence.Service
- `internal/server/handlers.go` - Uses service methods
- `internal/server/web.go` - Uses service methods
- `internal/scanner/scanner.go` - Uses persistence.Service

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
| GORM ORM | Done |
| Domain-Driven Design | Done |
| Full-Text Search | Done |
| Advanced Search (title+author) | Done |
| Pattern Filters | Done |
| Scoped Search | Done |
| Internationalization (i18n) | Done |
| Help Page | Done |
| Audiobook Support | Done |

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
