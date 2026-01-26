# PROGRESS.md

## Project: Simple OPDS Catalog (SOPDS) - Go Version
## Current Version: 0.51

---

### Revision 37 - 2026-01-26
**Feature: Web-Based FB2 Reader:**
- Implemented in-browser ebook reader for FB2, EPUB, and MOBI formats
- Non-FB2 formats converted to FB2 using Calibre's ebook-convert
- Route: `GET /web/read/{id}` - opens book in reader

**Reader Features:**
- Full book content display with proper typography
- Table of Contents navigation (collapsible sidebar)
- Font size controls (+/-)
- Light/dark mode toggle
- Reading position saved in localStorage
- Images embedded as data URIs
- Responsive design (mobile-friendly TOC)

**Files Created:**
- `internal/converter/reader.go` - FB2 to reader HTML conversion
  - `ReaderContent` struct with title, authors, TOC, HTML, cover
  - `FB2ToReaderHTML()` parses FB2 and generates reader-friendly HTML
  - `ConvertToFB2()` converts EPUB/MOBI to FB2 via Calibre

**Files Modified:**
- `internal/server/web.go`
  - Added `ReaderData` struct for reader template
  - Added `handleWebReader()` handler
  - Added `readFrom7z()` for reading books from 7z archives
  - Added `renderReaderTemplate()` with reader template
  - Added `readerTemplate` constant with full HTML/CSS/JS
  - Added "Read" button to book list template for FB2/EPUB/MOBI
  - Added "Read" button to bookshelf template
- `internal/server/server.go` - Registered `/web/read/{id}` route
- `internal/i18n/locales/*.yaml` - Added reader translations (all 5 languages)

**i18n Keys Added:**
- `reader.read` - Read button label
- `reader.toc` - Table of Contents
- `reader.font_smaller` / `reader.font_larger` - Font controls
- `reader.dark_mode` - Dark mode toggle
- `reader.close` - Close button
- `reader.not_supported` - Error for unsupported formats
- `reader.conversion_failed` - Conversion error message
- `reader.loading` - Loading indicator

**Bug Fix: Audiobook player link for all formats:**
- Changed condition from `Format=="FOLDER"` to `IsAudiobook`
- Now 7z/zip single-file audiobooks properly link to audio player page
- Single-track audiobooks show "Audiobook" instead of "1 Tracks"

---

### Revision 36 - 2026-01-26
**Feature: Favicon and Placeholder Covers:**
- Added SVG favicon with book icon in app theme colors
  - Routes: `/favicon.ico` and `/favicon.svg`
  - Added `<link rel="icon">` to HTML templates

**Feature: Placeholder Covers for Missing Book/Audiobook Covers:**
- Added fallback placeholder SVG images for books/audiobooks without covers
- Books use book icon placeholder (3 book spines)
- Audiobooks use headphones icon placeholder
  - Replaces 404 responses with 200 + placeholder image
  - Nordic-themed design (matching app colors) with book icon
  - Aspect ratio 2:3 (200x300) - standard book cover dimensions
  - "No Cover" text label
- Created `placeholderCoverSVG` constant in handlers.go
- Created `servePlaceholderCover()` function for serving the placeholder
- Updated `handleCover()` to return placeholder instead of 404 for:
  - Invalid book ID (non-numeric)
  - Non-existent book ID
  - Non-FB2 formats without embedded covers
  - FB2 files without cover images
  - File not found errors
- Updated `serveAudioCover()` to return placeholder for all error cases
- Updated `handleAudioTrackCover()` to return placeholder for all error cases:
  - Invalid audiobook ID
  - Missing file parameter
  - Audiobook not found
  - Not an audiobook
  - Cover not found in track
- **Bug fix**: Fixed nil pointer panic when GetBook returns nil book without error
  - GetBook returns `nil, nil` for non-existent books (not an error)
  - Added `book == nil` check alongside error check

**Bug Fix: UI language switching when filtering books:**
- The `lang` URL parameter was used for both UI language AND book filtering
- Filtering books by language (e.g., Japanese) would switch UI if it matched a supported language
- Fix: UI language now only set via cookie (JavaScript switcher)
- URL `lang` parameter is now exclusively for book language filtering
- Removed `setLangCookie()` function and all 18 calls to it

**Bug Fix: FOLDER button 404:**
- Folder-based audiobooks showed "FOLDER" as download button, which caused 404
- Fix: For folder format, show headphones button with track count linking to audio detail page
- Applied to both book list template and bookshelf template

**Bug Fix: Subfolder track playback in archive audiobooks:**
- Tracks inside subfolders within ZIP/7z archives couldn't be played
- Two issues fixed:
  1. Archive path construction when `path == filename` (data bug workaround)
  2. Track matching now uses multiple strategies:
     - Exact path match
     - Suffix match for relative paths
     - Basename match for just filenames
- Created `serveFileFromZip()` and `serveFileFrom7z()` helper functions
- Handles cases where tracks don't have full path stored (legacy data)

**Bug Fix: Empty track path in audiobook template:**
- Legacy audiobook data has empty `path` field in tracks (only `name` stored)
- Template was generating `?file=` (empty) causing 400 errors
- Fix: Use `{{or $track.Path $track.Name}}` to fallback to filename
- Applied to both collection tracks and flat track list templates

**Feature: Password Confirmation with Show/Hide Toggle:**
- Added password confirmation field to registration form
- Both password fields have eye icon toggle for show/hide
- Real-time validation checks passwords match before enabling submit
- When password changes, confirm field re-validates automatically

**Feature: Centralized i18n with Language Resource Files:**
- Created `internal/i18n/` package for internationalization
- Language files stored in `internal/i18n/locales/*.yaml`
- To add a new language: copy `en.yaml`, translate, add code to `supportedLanguages`
- All translations consolidated from web.go and server.go into YAML files
- Uses Go's `embed` to compile translations into binary
- **Supported languages (5):** English, Ukrainian, French, Spanish, German

**Files Created:**
- `internal/i18n/i18n.go` - i18n package with YAML loading
- `internal/i18n/locales/en.yaml` - English translations
- `internal/i18n/locales/uk.yaml` - Ukrainian translations (Українська)
- `internal/i18n/locales/fr.yaml` - French translations (Français)
- `internal/i18n/locales/es.yaml` - Spanish translations (Español)
- `internal/i18n/locales/de.yaml` - German translations (Deutsch)

**Files Modified:**
- `internal/server/web.go` - Uses i18n package instead of inline translations
- `internal/server/server.go` - Uses i18n package for auth translations
- `internal/server/auth_templates.go` - Password confirmation with toggle

**Files Created:**
- `internal/server/favicon.go` - SVG favicon with book icon

**Files Modified:**
- `internal/server/handlers.go` - Added placeholderCoverSVG, servePlaceholderCover(), updated handleCover() and serveAudioCover()
- `internal/server/server.go` - Added favicon routes (/favicon.ico, /favicon.svg)
- `internal/server/web.go` - Added favicon link to HTML templates, updated handleAudioTrackCover() to use placeholder

---

### Revision 35 - 2026-01-25
**Feature: SMTP Email Sending for Authentication:**
- Added `SMTPConfig` to configuration (`internal/config/config.go`)
  - Supports SMTP with STARTTLS (port 587) or implicit TLS (port 465)
  - Configurable: host, port, username, password, from address
- Created `EmailService` (`internal/server/email.go`)
  - `SendVerificationEmail()` - sends email verification links
  - `SendPasswordResetEmail()` - sends password reset links
  - Falls back to logging tokens when SMTP is disabled (dev mode)
- Updated `auth.go` to use EmailService instead of just logging
- Fixed auth template rendering (was returning empty page)
  - Templates define `{{define "content"}}` blocks
  - Now properly clones base template and executes "auth" template
- Fixed user dropdown hover issue (menu disappeared on hover)
  - Changed from margin-top gap to padding-top with transparent outer
  - Added primary color background on hover for visibility
- Added users table migration (`012_users.sql`) to schema

**Configuration (config.yaml):**
```yaml
smtp:
  enabled: false
  host: smtp.gmail.com
  port: 587
  username: ""
  password: ""
  from: "SOPDS Library <noreply@example.com>"
  use_tls: false
  use_starttls: true
```

**Files Modified:**
- `internal/config/config.go` - Added `SMTPConfig`
- `internal/server/email.go` - New file, email sending service
- `internal/server/server.go` - Added `emailService` to Server, template fix
- `internal/server/auth.go` - Use emailService for verification/reset emails
- `internal/server/web.go` - Fixed dropdown hover CSS, audiodetail PageData
- `config.yaml` - Added smtp section, fixed site.url

---

### Revision 34 - 2026-01-25
**Fix: Folder-Based Audiobook Path Calculation & M4B Grouping:**
- Fixed bug where folder audiobooks were stored with incorrect path
  - Problem: `relPath` included the folder name itself (e.g., "Music/Author - Title")
  - Fix: Now uses parent directory as path (e.g., "Music"), folder name as filename
  - This matches the pattern used by regular files
- **M4B files now included in folder grouping**
  - Folders with multiple M4B files are grouped as audiobook collections
  - Single M4B in a folder still processed as individual audiobook
  - Use case: collection folders like "Author - short stories (1)" with multiple M4B files
- Added cleanup of old individual audio files when creating grouped audiobook
  - `MarkAudioFilesInFolderDeleted()` marks old individual entries as unavailable
  - Affects all audio formats: mp3, m4a, m4b, flac, ogg, opus
  - Prevents duplicate entries when rescanning after grouping was added
- Fixed audio track serving for folder audiobooks
  - `serveTrackFromFolder()` now handles multiple path formats:
    1. Full filesystem paths (starts with /)
    2. Paths relative to library root (FolderName/file.mp3)
    3. Just filenames
  - Constructs correct full path using book.Path and trackPath
  - Added debug logging for path resolution issues
- Added cover extraction support for folder audiobooks
  - `extractTrackCoverFromFolder()` reads cover from audio file metadata
- **Per-track covers for folder audiobooks**
  - Audio player updates cover image when switching tracks
  - Each M4B/audio file can have its own embedded cover art
  - Uses preloading to avoid flicker when switching tracks
- **Fixed main book cover endpoint for folder audiobooks**
  - `serveAudioCover()` now correctly constructs folder path with `book.Filename`
  - Iterates through ALL tracks to find one with embedded cover (not just first)
  - Previously returned 404 when first track had no cover
  - Now serves cover from any track that has embedded art
- **Header now shows current track info**
  - Added "Now Playing" indicator with pulsing speaker icon
  - Shows current track name in header when playing
  - Cover image always visible (placeholder if none)
  - Cover updates per-track for folder audiobooks
  - Header remains sticky at top when scrolling

**Files Modified:**
- `internal/scanner/scanner.go` - Fixed `processAudioGroup()`, include M4B in grouping
- `internal/infrastructure/persistence/service.go` - Added `MarkAudioFilesInFolderDeleted()` with M4B
- `internal/server/web.go` - Fixed `serveTrackFromFolder()`, added `extractTrackCoverFromFolder()`, added "Now Playing" header indicator, cover placeholder, per-track cover updates
- `internal/server/handlers.go` - Fixed `serveAudioCover()` folder path and track iteration

---

### Revision 33 - 2026-01-25
**Taskfile for Project Automation:**
- Added Taskfile.yml for common project tasks
- Commands: build, init, migrate, start, stop, restart, status, scan
- Systemd: service-start, service-stop, service-restart, service-status, service-logs
- Database: db-backup, db-restore, db-vacuum, db-stats
- Duplicates: dupes, dupes-clear
- Development: dev, test, vet, fmt, lint, clean
- Import: import-mysql, version

**Files Created:**
- `Taskfile.yml` - Task runner configuration

---

### Revision 32 - 2026-01-25
**User Authentication System:**
- Added complete user authentication with JWT tokens
  - Registration with real-time validation (username/email availability check)
  - Login by email OR username
  - Logout functionality
  - Password reset with token (logged to console, no SMTP)
  - Email verification required before login
- Rate limiting for security
  - Username/email check: 150 requests/minute (prevents enumeration)
  - Forgot password: 5 requests/hour (prevents abuse)
- Password requirements with real-time feedback
  - Minimum 8 characters
  - At least 1 lowercase, 1 uppercase, 1 digit
  - Color-coded validation in registration form
- Username validation
  - 3-30 characters
  - Alphanumeric and underscore only
  - Trimmed (no leading/trailing spaces)
- Anonymous guest mode
  - "Continue as guest" option on landing page
  - Basic auth credentials work for guest mode (if configured)
  - Warning banner for anonymous users (bookshelf not saved)
- User dropdown in navigation
  - Shows username for logged-in users
  - Shows "Guest" for anonymous users
  - Login/Register links for unauthenticated
  - Logout link for authenticated
- Bookshelf migration on login
  - Anonymous bookshelf items copied to user account
  - Existing items not overwritten
- JWT-based sessions
  - HTTP-only cookie for security
  - 24-hour expiration
  - Optional jwt_secret in config.yaml

**Files Created:**
- `internal/infrastructure/persistence/migrations/012_users.sql` - Users table, bookshelf user_id
- `internal/domain/user/user.go` - User entity with validation
- `internal/domain/repository/user_repository.go` - Repository interface
- `internal/infrastructure/persistence/user_repository.go` - GORM implementation
- `internal/server/auth.go` - JWT utilities, rate limiting, auth handlers
- `internal/server/auth_templates.go` - Auth page templates (landing, login, register, etc.)

**Files Modified:**
- `internal/infrastructure/persistence/models.go` - Added UserID to BookshelfModel
- `internal/infrastructure/persistence/repositories.go` - Added Users repository
- `internal/infrastructure/persistence/service.go` - Added MigrateAnonBookshelf method
- `internal/config/config.go` - Added JWTSecret to ServerConfig
- `internal/server/server.go` - Added userRepo, authHandlers, auth routes, authMiddleware
- `internal/server/web.go` - Added Auth field to PageData, auth translations, user dropdown, guest warning

**New Dependencies:**
- `github.com/golang-jwt/jwt/v5` - JWT token handling

**Auth Page Templates:**
- Landing page with login/register/guest options
- Login form with email/username support
- Registration form with real-time validation
- Forgot password form
- Reset password form with password strength indicator
- Message page for verification errors

---

### Revision 31 - 2026-01-25
**Folder-Based Audiobook Grouping:**
- Audio files in same folder now grouped as single audiobook entry
  - Uses existing `AudiobookGrouper` for grouping logic
  - Creates "folder" format audiobooks (similar to archive-based)
  - Track structure stored in `chapters` JSONB field
  - Properly links authors, narrators, and genres from metadata
- @eaDir directories skipped during scan (Synology metadata folders)
- M4B files processed individually (single-file audiobooks with chapters)

**Cyrillic Encoding Fix:**
- Fixed Windows-1251 text incorrectly read as Latin-1 in ID3 tags
  - Added `fixCyrillicEncoding()` to detect and convert mojibake
  - Uses `golang.org/x/text/encoding/charmap` for Windows-1251 decoding
- Fixed UTF-16 metadata misread as UTF-8 (common in OGG Vorbis comments)
  - Detects "Б . А к у н и н" pattern (null bytes appearing as spaces)
  - Strips null bytes and removes alternating space pattern
  - Prevents PostgreSQL "invalid byte sequence for encoding UTF8: 0x00" error

**OGG Duration Fix:**
- Fixed Vorbis sample rate detection (was reading wrong offset)
  - After 8-byte codec header, skip 4 bytes (remaining version + channels)
  - Previously skipped 5 bytes, reading sample rate from wrong position
  - Fixed duration calculation: 163M granules / 44100 Hz = 3697s (was calculating 947881s)

**Folder Audiobook Playback Fix:**
- Added track endpoint support for "folder" format audiobooks
  - `handleAudioTrackDownload` now handles "folder" format (was only zip/7z)
  - New `serveTrackFromFolder()` function serves files directly from disk
  - Uses `http.ServeContent()` for proper HTTP range request support (seeking)
  - Security check ensures file path is within library root

**Folder Audiobook Author/Title Parsing:**
- Always parse author and title from folder name (format: "Author - Title")
  - Metadata "artist" field often contains narrator, not author
  - Metadata artists now treated as narrators for folder audiobooks
  - `parseTitleFromFolderName()` extracts title after separator
  - Handles " - ", " – " (en-dash), and "_-_" separators
  - Removes year suffixes like "(2020)" or "[2020]"

**Folder Audiobook Cover Fix:**
- Fixed cover detection for folder-based audiobooks
  - `serveAudioCover` now handles "folder" format correctly
  - Uses first track path from chapters JSON for @eaDir lookup
  - Added `getFolderCoverInDir()` helper for folder cover detection
- Added SYNOAUDIO patterns to @eaDir cover lookup
  - `SYNOAUDIO_01APIC_03.jpg` (Audio Station album art)
  - `SYNOAUDIO_01APIC_00/01/02.jpg` variants

**Files Modified:**
- `internal/scanner/duration.go`:
  - Fixed OGG Vorbis sample rate offset (skip 4 bytes, not 5)
- `internal/scanner/scanner.go`:
  - Added `audioGrouper` to Scanner struct
  - Modified walk to collect audio files separately, skip @eaDir
  - Added `processAudioFolders()` to group and process collected audio files
  - Added `processAudioGroup()` to create single audiobook entry from folder
  - Author linking now always parses folder name first, treats metadata as narrators
- `internal/scanner/audioparser.go`:
  - Added `fixCyrillicEncoding()` for Windows-1251 detection and fix
  - Added `looksLikeMojibake()`, `hasCyrillic()` helper functions
  - Added `looksLikeUTF16AsUTF8()`, `fixUTF16AsUTF8()` for UTF-16 fix
  - All metadata fields now passed through encoding fix
- `internal/scanner/audiobookgrouper.go`:
  - `createGroup()` now parses title from folder name
  - Added `parseTitleFromFolderName()` function
  - Added `removeYearSuffix()` and `isDigits()` helpers
  - Metadata authors treated as narrators
- `internal/server/handlers.go`:
  - Added SYNOAUDIO patterns to `getEaDirCover()`
  - `serveAudioCover()` now handles "folder" format
  - Added `getFolderCoverInDir()` helper function
  - Added `encoding/json` import
- `internal/server/web.go`:
  - Added `serveTrackFromFolder()` for folder audiobook playback

**New Dependencies:**
- `golang.org/x/text/encoding/charmap` - Character set conversion

---

### Revision 30 - 2026-01-25
**@eaDir and Folder Cover Support:**
- Added Synology @eaDir thumbnail support for audiobook covers
  - `getEaDirCover()` checks for SYNOPHOTO_THUMB_*.jpg patterns
  - Looks in `@eaDir/{filename}/` and `@eaDir/` directories
  - Fast cover serving without parsing audio files
- Added folder cover support (cover.jpg, folder.jpg)
  - `getFolderCover()` checks for common cover image names
  - Supports both .jpg and .png formats
  - Scanner detects folder covers during scan
- Cover priority: @eaDir → folder cover → embedded in audio → archive extraction

**Duration Extraction Improvements:**
- Fixed memory issues with large M4B files in archives
  - Only read first 10MB for M4B/M4A (moov atom often at beginning)
  - Prevents memory exhaustion for 700MB+ files
- Improved fallback estimation bitrate (96kbps for AAC, more accurate)
- Disabled track-specific cover loading (caused 502 errors for large files)

**Bug Fixes:**
- Fixed nil pointer panic when accessing deleted audiobook
- Added null check for book in handleWebAudioDetail

**Files Modified:**
- `internal/server/handlers.go`:
  - `getEaDirCover()` - Synology thumbnail lookup
  - `getFolderCover()` - Folder cover lookup
  - Updated `serveAudioCover()` to check @eaDir and folder covers first
- `internal/scanner/scanner.go`:
  - `hasEaDirCover()` - Check @eaDir during scan
  - `hasFolderCover()` - Check folder covers during scan
  - Limited archive file reading to 10MB for M4B duration extraction
- `internal/server/web.go`:
  - Disabled track-specific cover fetching
  - Added nil check for book

---

### Revision 29 - 2026-01-25
**Audiobook Player Enhancements:**
- Extracted real audio duration from files during scan
  - New `duration.go` with native Go parsers for MP4, MP3, FLAC, OGG duration extraction
  - MP4: Parse mvhd atom for timescale and duration
  - MP3: Parse Xing/Info header for frame count, fallback to bitrate estimation
  - FLAC: Parse STREAMINFO block for sample rate and total samples
  - OGG: Read last granule position from end of file
  - Tested accuracy: MP3 within 0.01s, M4B exact match with ffprobe
- Made track rows clickable to switch playback
  - Added `onclick="player.onTrackClick(this, event)"` to track `<li>` elements
  - Added `event.stopPropagation()` on checkbox, play button, download link to prevent row click
  - Added cursor:pointer and hover effect to track list items
- Implemented per-track position persistence
  - `trackPositions` map stores position for each track by path
  - `saveCurrentTrackPosition()` saves position before switching tracks
  - `saveState()` now includes `trackPositions` in cookie
  - `loadState()` restores per-track positions from cookie
  - `loadTrack()` restores position for specific track being loaded
- Made header with cover sticky (always visible)
  - Added `position: sticky; top: 0; z-index: 100;` to `.audio-header`
  - Header stays visible while scrolling through track list
- Added track-specific cover images support
  - New endpoint `/web/audio/{id}/cover?file={trackPath}` for per-track covers
  - `handleAudioTrackCover()` extracts cover from specific audio file in archive
  - `extractTrackCoverFromZip()` and `extractTrackCoverFrom7z()` helpers
  - `extractCoverFromAudioData()` uses dhowden/tag to extract embedded cover
  - Player attempts to load track-specific cover when switching tracks
  - Falls back to book cover if track has no embedded cover

**Files Created:**
- `internal/scanner/duration.go` - Audio duration extraction for MP4, MP3, FLAC, OGG

**Files Modified:**
- `internal/scanner/audioparser.go` - Uses `GetAudioDurationFromReader()` for real duration
- `internal/server/web.go` - Audiodetail template changes:
  - Track rows clickable with onclick handler
  - Per-track position storage in AudioPlayer JS class
  - Sticky header CSS
  - Track cover update on switch
  - New `handleAudioTrackCover()` handler
- `internal/server/server.go` - Added `/audio/{id}/cover` route

---

### Revision 28 - 2026-01-25
**Bug Fixes:**
- Fixed bookshelf "Added" state not persisting after navigation
  - Added `OnBookshelf` field to `BookView` struct
  - Load user's bookshelf IDs and check when building book views
  - Template now shows "Added" button if book is already on shelf
- Fixed bookshelf button not working on mobile/audiodetail page
  - Changed audiodetail bookshelf link from `<a href>` to JavaScript `onclick`
  - Now uses same `bookshelfAction()` function as regular book list
- Fixed pagination showing "Next" when books count equals page size
  - Changed `hasMore` calculation from `len(books) >= pageSize` to use `pagination.TotalCount`
  - Now correctly shows "Next" only when more pages exist
- Fixed audio cover not displaying for 7z audiobooks
  - Check `book.IsAudiobook` flag, not just audio format extension
  - Handle standalone 7z/zip archive files as archives (not audio files)
  - Correctly build archive path from book.Path + book.Filename

**Files Modified:**
- `internal/server/web.go` - Added OnBookshelf to BookView, updated handlers
- `internal/server/handlers.go` - Fixed audio cover extraction for 7z archives
- `internal/domain/repository/bookshelf_repository.go` - Added GetBookIDs interface
- `internal/infrastructure/persistence/bookshelf_repository.go` - Implemented GetBookIDs
- `internal/infrastructure/persistence/service.go` - Added GetBookShelfIDs service method

---

### Revision 27 - 2026-01-25
**Changes:**
- Added missing file detection for regular files (cat_type=0)
  - Scanner now detects when standalone files (e.g., 7z audiobooks) are deleted/renamed
  - Prompts for confirmation before marking as unavailable (respects `auto_clean` config)
  - Similar to existing ZIP archive cleanup logic
- Added audio cover display for audiobook detail pages
  - Set `HasCover: true` for audiobooks in handleWebAudioDetail
  - Cover extraction from audio files (MP3, M4B, M4A, FLAC, OGG, OPUS)
  - Supports covers embedded in files inside ZIP and 7z archives
  - Template has onerror fallback to hide image if no cover found
- Fixed ZIP cover extraction to buffer data for tag.ReadFrom (requires io.ReadSeeker)
- Fixed duplicate detection to separate audiobooks from text books
  - Audiobooks only match other audiobooks
  - Non-audiobooks only match other non-audiobooks
  - Prevents audiobook being marked as duplicate of FB2 with same title

**Files Modified:**
- `internal/scanner/scanner.go` - Added checkMissingRegularFiles() method
- `internal/infrastructure/persistence/service.go` - Added GetRegularFileBooks(), MarkBooksUnavailable()
- `internal/infrastructure/persistence/book_repository.go` - Added is_audiobook to duplicate detection queries
- `internal/server/web.go` - Set HasCover for audiobooks in handleWebAudioDetail
- `internal/server/handlers.go` - Fixed ZIP audio cover extraction buffering

**New Dependencies:**
- `github.com/saracen/go7z` - 7z archive reading for cover extraction

---

### Revision 26 - 2026-01-25
**Changes:**
- Optimized duplicate detection performance
  - Added functional indexes on `LOWER(title)` for case-insensitive matching
  - New index `idx_books_dup_strong` for strong mode (title + format + filesize)
  - New index `idx_books_dup_normal` for normal mode (title)
- Implemented incremental duplicate detection
  - Only checks newly added books against existing ones
  - O(n) for n new books instead of O(N log N) for N total books
  - Tracks new book IDs during scan for targeted duplicate checking
- Added progress reporting during duplicate detection
  - Logs progress every 500 books processed
  - Shows percentage completion

**Files Modified:**
- `internal/infrastructure/persistence/migrations/011_duplicate_detection_index.sql` - New indexes
- `internal/domain/repository/book_repository.go` - Added MarkDuplicatesIncremental interface
- `internal/infrastructure/persistence/book_repository.go` - Implemented incremental method
- `internal/infrastructure/persistence/service.go` - Added service method
- `internal/scanner/scanner.go` - Track new book IDs, use incremental detection

---

### Revision 25 - 2026-01-25
**Changes:**
- Implemented full-featured audiobook player UI
  - Sticky player bar at bottom of page with gradient design
  - Progress bar with seek functionality (click or drag to seek)
  - Time display: current time / total duration
  - Playback controls: prev, -15s, play/pause, +15s, next
  - Speed control: 0.5x, 0.75x, 1x, 1.25x, 1.5x, 1.75x, 2x (saved in cookie)
  - Volume toggle (mute/unmute)
  - Buffering indicator on progress bar
  - Auto-play next track when current ends
- Track list enhancements:
  - Now-playing highlight with pulsing icon animation
  - Auto-scroll to current track
  - Auto-expand collapsed folder when track plays
  - Track icon changes from music note to speaker when playing
- Cookie-based progress persistence:
  - Saves current track index and position per book (30 days)
  - Restores position when returning to audiobook page
  - Playback speed saved globally (365 days)
- Keyboard shortcuts:
  - Space: play/pause
  - Left/Right arrows: seek -5s/+5s (Shift: -30s/+30s)
  - Up/Down arrows: volume +/-10%
  - M: toggle mute
  - N: next track
  - P: previous track (or restart if >3s played)
- Mobile responsive design:
  - Simplified controls on small screens
  - Touch-friendly seek bar
- Player bar positioned below track list (sticky, not fixed at viewport bottom)

**Files Modified:**
- `internal/server/web.go` - Complete rewrite of audiodetail template with AudioPlayer class

---

### Revision 24 - 2026-01-10
**Changes:**
- Added more progress phases during scan
  - "Loading catalogs from database..." - when loading ZIP catalog cache
  - "Detecting duplicates..." - when running duplicate detection
  - Previously these phases showed nothing, making scan appear stuck
- Fixed progress not displaying due to buffering and throttling
  - Added `os.Stdout.Sync()` to flush output immediately
  - Only throttle "scanning" phase, always show phase changes
- Skip duplicate detection if no new books added (major performance fix)
  - Duplicate detection on 568K books was taking 3+ minutes
  - Now skips entirely when BooksAdded == 0

**Files Modified:**
- `internal/scanner/scanner.go` - Added phase reports, fixed throttle, skip duplicates if no new books
- `cmd/sopds/main.go` - Handle new phases in printProgress, add stdout sync

---

### Revision 23 - 2026-01-10
**Changes:**
- Added `scanner.auto_clean` config option for missing archives
  - `ask` (default) - prompt user for confirmation
  - `yes` - auto-delete without asking
  - `no` - skip check entirely, never delete
- Fixed DeleteInCatalogs not deleting catalog entries
  - Was only deleting books, leaving catalog entries in DB
  - Now deletes both books AND catalog entries
  - Fixes repeated prompts about missing archives on every scan

**Files Modified:**
- `internal/config/config.go` - Added AutoClean field to ScannerConfig
- `internal/scanner/scanner.go` - Check auto_clean config before prompting
- `internal/infrastructure/persistence/book_repository.go` - DeleteInCatalogs now also deletes catalogs
- `config.yaml` - Added auto_clean: yes setting

---

### Revision 22 - 2026-01-10
**Changes:**
- Added play buttons for audio tracks in audiobook detail page
  - Green play button next to each track's download button
  - Click to play/pause, icon toggles between play/pause
  - Hidden HTML5 audio element for streaming
  - Auto-stops previous track when playing new one
  - Resets icon when track ends

**Files Modified:**
- `internal/server/web.go` - Added track-play button, playTrack() JS function, audio element, CSS styles

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
| User Authentication | Done |

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
| users | User accounts |
