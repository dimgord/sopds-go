package scanner

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/robfig/cron/v3"
	"github.com/dimgord/sopds-go/internal/config"
	"github.com/dimgord/sopds-go/internal/database"
	"github.com/dimgord/sopds-go/internal/infrastructure/persistence"
)

// bookWithMeta holds a book and its parsed FB2 metadata for batch processing
type bookWithMeta struct {
	Book    *database.Book
	Authors []database.Author
	Genres  []string
	Series  []SeriesInfo
}

// Scanner scans library directory for books
type Scanner struct {
	config           *config.Config
	svc              *persistence.Service
	parser           *FB2Parser
	audioParser      *AudioParser
	audioGrouper     *AudiobookGrouper
	cron             *cron.Cron
	extSet           map[string]bool
	audioExtSet      map[string]bool // Audio-specific extensions
	stats            ScanStats
	statsMu          sync.Mutex
	isRunning        atomic.Bool
	progressCallback ProgressCallback
	confirmCallback  ConfirmCallback
	lastProgress     time.Time
	knownZips        map[string]int64 // path -> cat_id cache
	newBookIDs       []int64          // IDs of books added in current scan (for incremental duplicate detection)
	newBookIDsMu     sync.Mutex       // Protects newBookIDs
}

// ConfirmCallback is called to confirm destructive operations.
// It receives a message explaining what will happen and why.
// Returns true to proceed, false to skip the operation.
type ConfirmCallback func(message string) bool

// ScanStats holds scanning statistics
type ScanStats struct {
	StartTime       time.Time
	EndTime         time.Time
	BooksAdded      int64
	BooksSkipped    int64
	BooksDeleted    int64
	ArchivesScanned int64
	ArchivesSkipped int64
	BadArchives     int64
	BooksInArchives int64
	TotalFiles      int64
	ProcessedFiles  int64
}

// ProgressInfo contains current scan progress information
type ProgressInfo struct {
	TotalFiles     int64
	ProcessedFiles int64
	BooksAdded     int64
	BooksSkipped   int64
	Elapsed        time.Duration
	Rate           float64 // files per second
	ETA            time.Duration
	Phase          string // "counting", "scanning", "cleanup"
}

// ProgressCallback is called periodically during scan with progress updates
type ProgressCallback func(info ProgressInfo)

// New creates a new scanner
func New(cfg *config.Config, svc *persistence.Service) *Scanner {
	// Build extension set
	extSet := make(map[string]bool)
	for _, ext := range cfg.Library.Formats {
		extSet[strings.ToLower(ext)] = true
	}

	// Build audio extension set
	audioExtSet := map[string]bool{
		".mp3": true, ".m4b": true, ".m4a": true,
		".flac": true, ".ogg": true, ".opus": true,
		".awb": true, // Nokia audiobook format (AMR-WB)
	}

	audioParser := NewAudioParser(cfg.Server.Port > 0)

	return &Scanner{
		config:       cfg,
		svc:          svc,
		parser:       NewFB2Parser(cfg.Server.Port > 0), // Read covers if server is enabled
		audioParser:  audioParser,
		audioGrouper: NewAudiobookGrouper(audioParser),
		extSet:       extSet,
		audioExtSet:  audioExtSet,
	}
}

// SetProgressCallback sets a callback function for progress updates
func (s *Scanner) SetProgressCallback(cb ProgressCallback) {
	s.progressCallback = cb
}

// SetConfirmCallback sets a callback function for confirming destructive operations
func (s *Scanner) SetConfirmCallback(cb ConfirmCallback) {
	s.confirmCallback = cb
}

// reportProgress sends progress update if callback is set and enough time has passed
func (s *Scanner) reportProgress(phase string) {
	if s.progressCallback == nil {
		return
	}

	now := time.Now()
	// Always report phase changes, only throttle "scanning" phase
	if phase == "scanning" && now.Sub(s.lastProgress) < 500*time.Millisecond {
		return // Rate limit scanning progress updates
	}
	s.lastProgress = now

	processed := atomic.LoadInt64(&s.stats.ProcessedFiles)
	total := atomic.LoadInt64(&s.stats.TotalFiles)
	elapsed := now.Sub(s.stats.StartTime)

	var rate float64
	var eta time.Duration
	if elapsed.Seconds() > 0 && processed > 0 {
		rate = float64(processed) / elapsed.Seconds()
		if rate > 0 && total > processed {
			remaining := total - processed
			eta = time.Duration(float64(remaining)/rate) * time.Second
		}
	}

	s.progressCallback(ProgressInfo{
		TotalFiles:     total,
		ProcessedFiles: processed,
		BooksAdded:     atomic.LoadInt64(&s.stats.BooksAdded),
		BooksSkipped:   atomic.LoadInt64(&s.stats.BooksSkipped),
		Elapsed:        elapsed,
		Rate:           rate,
		ETA:            eta,
		Phase:          phase,
	})
}

// Run starts the scanner with scheduling
func (s *Scanner) Run(ctx context.Context) {
	// Run initial scan if configured
	if s.config.Scanner.OnStart {
		go s.ScanAll(ctx)
	}

	// Set up scheduled scanning
	if s.config.Scanner.Schedule != "" {
		s.cron = cron.New()
		_, err := s.cron.AddFunc(s.config.Scanner.Schedule, func() {
			s.ScanAll(context.Background())
		})
		if err != nil {
			log.Printf("Failed to schedule scanner: %v", err)
			return
		}
		s.cron.Start()
		log.Printf("Scanner scheduled: %s", s.config.Scanner.Schedule)
	}

	<-ctx.Done()
	if s.cron != nil {
		s.cron.Stop()
	}
}

// countFiles counts total files to process
func (s *Scanner) countFiles(ctx context.Context) (int64, error) {
	var count int64

	err := filepath.WalkDir(s.config.Library.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Continue on errors
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if (ext == ".zip" || ext == ".7z") && s.config.Library.ScanZip {
			count++
		} else if s.extSet[ext] || s.audioExtSet[ext] {
			count++
		}
		return nil
	})

	return count, err
}

// ScanAll performs a full library scan
func (s *Scanner) ScanAll(ctx context.Context) error {
	if !s.isRunning.CompareAndSwap(false, true) {
		log.Println("Scan already in progress, skipping")
		return nil
	}
	defer s.isRunning.Store(false)

	log.Println("Starting library scan...")
	s.resetStats()
	s.stats.StartTime = time.Now()
	s.lastProgress = time.Now()

	// Enable async commits for performance (data recoverable by re-scan if crash)
	if err := s.svc.SetAsyncCommit(); err != nil {
		log.Printf("Warning: could not enable async commit: %v", err)
	}
	defer s.svc.SetSyncCommit()

	// Count total files first
	s.reportProgress("counting")
	log.Println("Counting files...")
	totalFiles, err := s.countFiles(ctx)
	if err != nil {
		log.Printf("Error counting files: %v", err)
	}
	atomic.StoreInt64(&s.stats.TotalFiles, totalFiles)
	log.Printf("Found %d files to process", totalFiles)

	// Load all known ZIP catalogs into memory for fast lookup
	s.reportProgress("loading")
	s.knownZips = make(map[string]int64)
	if !s.config.Library.RescanZip {
		var err error
		s.knownZips, err = s.svc.GetAllZipCatalogs(ctx)
		if err != nil {
			log.Printf("Failed to load ZIP catalogs: %v", err)
		} else {
			// Check for removed archives and delete their books
			var removedCatIDs []int64
			var removedPaths []string
			for path, catID := range s.knownZips {
				fullPath := filepath.Join(s.config.Library.Root, path)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					removedCatIDs = append(removedCatIDs, catID)
					removedPaths = append(removedPaths, path)
					delete(s.knownZips, path)
				}
			}
			if len(removedCatIDs) > 0 {
				// Build confirmation message with examples
				msg := fmt.Sprintf(
					"Found %d ZIP archives in the database that no longer exist on disk.\n"+
						"Library root: %s\n"+
						"This can happen if:\n"+
						"  - Archives were deleted or moved\n"+
						"  - The library root path changed\n"+
						"  - The drive/mount point is not available\n\n"+
						"Examples of missing archives:\n",
					len(removedCatIDs), s.config.Library.Root)

				// Show up to 5 examples
				examples := removedPaths
				if len(examples) > 5 {
					examples = examples[:5]
				}
				for _, p := range examples {
					msg += fmt.Sprintf("  - %s\n", p)
				}
				if len(removedPaths) > 5 {
					msg += fmt.Sprintf("  ... and %d more\n", len(removedPaths)-5)
				}
				msg += "\nDelete these archives and their books from the database?"

				// Check auto_clean config: ask (default), yes, no
				autoClean := strings.ToLower(s.config.Scanner.AutoClean)
				shouldDelete := true

				if autoClean == "no" {
					log.Printf("Skipping removal of %d archives (auto_clean=no)", len(removedCatIDs))
					shouldDelete = false
				} else if autoClean == "yes" {
					log.Printf("Auto-cleaning %d missing archives (auto_clean=yes)", len(removedCatIDs))
				} else if s.confirmCallback != nil {
					// Default: ask for confirmation
					if !s.confirmCallback(msg) {
						log.Printf("Skipping removal of %d archives (user cancelled)", len(removedCatIDs))
						shouldDelete = false
					}
				}

				if !shouldDelete {
					// Restore the paths to knownZips so they are not re-scanned
					for i, path := range removedPaths {
						s.knownZips[path] = removedCatIDs[i]
					}
					removedCatIDs = nil
				}

				if len(removedCatIDs) > 0 {
					log.Printf("Removing %d deleted archives from database...", len(removedCatIDs))
					deletedCount, err := s.svc.DeleteBooksInCatalogs(ctx, removedCatIDs)
					if err != nil {
						log.Printf("Failed to delete books from removed archives: %v", err)
					} else {
						atomic.AddInt64(&s.stats.BooksDeleted, deletedCount)
						log.Printf("Marked %d books as deleted", deletedCount)
					}
				}
			}
		}
	}

	// Reset start time after confirmations to exclude wait time from ETA calculations
	s.stats.StartTime = time.Now()

	// Walk the library directory
	workers := s.config.Scanner.Workers
	if workers <= 0 {
		workers = 4
	}

	fileChan := make(chan string, workers*2)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileChan {
				select {
				case <-ctx.Done():
					return
				default:
					s.processPath(ctx, path)
					atomic.AddInt64(&s.stats.ProcessedFiles, 1)
					s.reportProgress("scanning")
				}
			}
		}()
	}

	// Collect audio files for grouping, send others to workers
	var audioFiles []string
	var audioFilesMu sync.Mutex

	err = filepath.WalkDir(s.config.Library.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("Error accessing %s: %v", path, err)
			return nil // Continue on errors
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip @eaDir directories
		if d.IsDir() {
			if d.Name() == "@eaDir" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))

		// Archives go directly to workers
		if (ext == ".zip" || ext == ".7z") && s.config.Library.ScanZip {
			fileChan <- path
			return nil
		}

		// Audio files are collected for grouping
		// GroupByFolder will handle: single file = individual, multiple files = grouped
		if s.audioExtSet[ext] {
			audioFilesMu.Lock()
			audioFiles = append(audioFiles, path)
			audioFilesMu.Unlock()
			return nil
		}

		// Other files (fb2, epub, etc.) go to workers
		if s.extSet[ext] {
			fileChan <- path
		}
		return nil
	})

	close(fileChan)
	wg.Wait()

	// Process grouped audio folders
	if len(audioFiles) > 0 {
		s.processAudioFolders(ctx, audioFiles)
	}

	if err != nil {
		log.Printf("Scan walk error: %v", err)
	}

	// Check for missing regular files (cat_type=0)
	s.checkMissingRegularFiles(ctx)

	// Mark duplicates (skip if no new books added)
	if s.config.Scanner.Duplicates != "none" && atomic.LoadInt64(&s.stats.BooksAdded) > 0 {
		mode := database.DupNormal
		switch s.config.Scanner.Duplicates {
		case "strong":
			mode = database.DupStrong
		case "clear":
			mode = database.DupClear
		}
		s.reportProgress("duplicates")

		// Use incremental duplicate detection for new books
		s.newBookIDsMu.Lock()
		newIDs := s.newBookIDs
		s.newBookIDsMu.Unlock()

		if len(newIDs) > 0 {
			log.Printf("Checking %d new books for duplicates...", len(newIDs))
			progressFn := func(processed, total int) {
				if total > 0 && processed%500 == 0 {
					log.Printf("Duplicate check progress: %d/%d (%.1f%%)", processed, total, float64(processed)/float64(total)*100)
				}
			}
			if err := s.svc.MarkDuplicatesIncremental(ctx, mode, newIDs, progressFn); err != nil {
				log.Printf("Failed to mark duplicates: %v", err)
			}
		} else {
			log.Println("No new books to check for duplicates")
		}
	}

	s.stats.EndTime = time.Now()
	s.logStats()

	// Final progress report
	if s.progressCallback != nil {
		s.lastProgress = time.Time{} // Force update
		s.reportProgress("done")
	}

	return nil
}

func (s *Scanner) resetStats() {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	s.stats = ScanStats{}

	s.newBookIDsMu.Lock()
	s.newBookIDs = nil
	s.newBookIDsMu.Unlock()
}

// trackNewBook records a newly added book ID for incremental duplicate detection
func (s *Scanner) trackNewBook(id int64) {
	s.newBookIDsMu.Lock()
	s.newBookIDs = append(s.newBookIDs, id)
	s.newBookIDsMu.Unlock()
}

// checkMissingRegularFiles finds regular files (cat_type=0) that no longer exist and marks them unavailable
func (s *Scanner) checkMissingRegularFiles(ctx context.Context) {
	files, err := s.svc.GetRegularFileBooks(ctx)
	if err != nil {
		log.Printf("Failed to get regular files for cleanup: %v", err)
		return
	}

	if len(files) == 0 {
		return
	}

	// Check which files are missing
	var missingIDs []int64
	var missingPaths []string
	for _, f := range files {
		fullPath := filepath.Join(s.config.Library.Root, f.Path, f.Filename)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			missingIDs = append(missingIDs, f.ID)
			missingPaths = append(missingPaths, filepath.Join(f.Path, f.Filename))
		}
	}

	if len(missingIDs) == 0 {
		return
	}

	// Build confirmation message
	msg := fmt.Sprintf(
		"Found %d regular files in the database that no longer exist on disk.\n"+
			"Library root: %s\n\n"+
			"Examples of missing files:\n",
		len(missingIDs), s.config.Library.Root)

	examples := missingPaths
	if len(examples) > 5 {
		examples = examples[:5]
	}
	for _, p := range examples {
		msg += fmt.Sprintf("  - %s\n", p)
	}
	if len(missingPaths) > 5 {
		msg += fmt.Sprintf("  ... and %d more\n", len(missingPaths)-5)
	}
	msg += "\nMark these files as unavailable in the database?"

	// Check auto_clean config
	autoClean := strings.ToLower(s.config.Scanner.AutoClean)
	shouldMark := true

	if autoClean == "no" {
		log.Printf("Skipping marking %d missing files (auto_clean=no)", len(missingIDs))
		shouldMark = false
	} else if autoClean == "yes" {
		log.Printf("Auto-marking %d missing files as unavailable (auto_clean=yes)", len(missingIDs))
	} else if s.confirmCallback != nil {
		if !s.confirmCallback(msg) {
			log.Printf("Skipping marking %d missing files (user cancelled)", len(missingIDs))
			shouldMark = false
		}
	}

	if shouldMark {
		count, err := s.svc.MarkBooksUnavailable(ctx, missingIDs)
		if err != nil {
			log.Printf("Failed to mark missing files as unavailable: %v", err)
		} else {
			atomic.AddInt64(&s.stats.BooksDeleted, count)
			log.Printf("Marked %d missing files as unavailable", count)
		}
	}
}

func (s *Scanner) logStats() {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	duration := s.stats.EndTime.Sub(s.stats.StartTime)
	log.Printf("Scan completed in %v", duration)
	log.Printf("Books added: %d", s.stats.BooksAdded)
	log.Printf("Books skipped: %d", s.stats.BooksSkipped)
	log.Printf("Books deleted: %d", s.stats.BooksDeleted)
	log.Printf("Archives scanned: %d", s.stats.ArchivesScanned)
	log.Printf("Archives skipped: %d", s.stats.ArchivesSkipped)
	log.Printf("Bad archives: %d", s.stats.BadArchives)
	log.Printf("Books in archives: %d", s.stats.BooksInArchives)
}

// processAudioFolders groups audio files by folder and creates single audiobook entries
func (s *Scanner) processAudioFolders(ctx context.Context, audioFiles []string) {
	if len(audioFiles) == 0 {
		return
	}

	log.Printf("Grouping %d audio files by folder...", len(audioFiles))

	groups, singles := s.audioGrouper.GroupByFolder(audioFiles)

	log.Printf("Found %d audiobook folders, %d single files", len(groups), len(singles))

	// Process each audiobook group
	for _, group := range groups {
		s.processAudioGroup(ctx, &group)
	}

	// Process single files individually
	for _, path := range singles {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		s.processFile(ctx, path, info.Size(), database.CatNormal, 0)
	}
}

// processAudioGroup creates a single audiobook entry for a folder of audio files
func (s *Scanner) processAudioGroup(ctx context.Context, group *AudiobookGroup) {
	if len(group.Tracks) == 0 {
		return
	}

	// Check for INX file (Nokia audiobooks) to get accurate durations
	var inxDurations map[string]int // filename -> duration in seconds
	if inxPath, _ := FindINXFile(group.FolderPath); inxPath != "" {
		if nokia, err := ParseINX(inxPath); err == nil {
			inxDurations = make(map[string]int)
			for _, t := range nokia.Tracks {
				inxDurations[t.Filename] = t.Duration
			}
		}
	}

	// Get relative path to PARENT directory (folder name becomes filename)
	parentDir := filepath.Dir(group.FolderPath)
	relPath, err := filepath.Rel(s.config.Library.Root, parentDir)
	if err != nil {
		relPath = parentDir
	}
	// Handle case where folder is directly in library root
	if relPath == "." {
		relPath = ""
	}

	// Use folder name as filename for the "virtual" audiobook
	filename := group.FolderName

	// Check if audiobook already exists
	book, err := s.svc.FindBook(ctx, filename, relPath)
	if err == nil && book != nil {
		// Book exists, mark as verified
		s.svc.UpdateBookAvail(ctx, book.ID, database.AvailVerified)
		atomic.AddInt64(&s.stats.BooksSkipped, 1)
		return
	}

	// Clean up any individual audio files that may exist in this folder
	// (from scans before grouping was implemented)
	folderRelPath, _ := filepath.Rel(s.config.Library.Root, group.FolderPath)
	if deletedCount, err := s.svc.MarkAudioFilesInFolderDeleted(ctx, folderRelPath); err != nil {
		log.Printf("Failed to clean up individual files in %s: %v", folderRelPath, err)
	} else if deletedCount > 0 {
		log.Printf("Cleaned up %d individual audio files in folder %s", deletedCount, group.FolderName)
		atomic.AddInt64(&s.stats.BooksDeleted, deletedCount)
	}

	// Get or create catalog tree (use parent path for catalog)
	var catID int64
	if relPath != "" {
		pathParts := strings.Split(relPath, string(filepath.Separator))
		catID, _ = s.svc.GetOrCreateCatalogTree(ctx, pathParts, database.CatNormal)
	}

	// Build tracks structure (similar to archive processing)
	var tracks []AudiobookTrack
	var totalDuration int
	var totalSize int64

	for _, t := range group.Tracks {
		// Get duration - check INX first (for AWB files), then metadata, then extract
		duration := 0
		if inxDurations != nil {
			if d, ok := inxDurations[t.Filename]; ok {
				duration = d
			}
		}
		if duration == 0 {
			duration = int(t.Duration.Seconds())
		}
		if duration == 0 {
			// Try to extract actual duration
			if dur, err := GetAudioDuration(t.Path); err == nil && dur > 0 {
				duration = int(dur.Seconds())
			}
		}

		tracks = append(tracks, AudiobookTrack{
			Name:     t.Filename,
			Path:     t.Path, // Full path for playback
			Duration: duration,
			Size:     t.Size,
		})
		totalDuration += duration
		totalSize += t.Size
	}

	// Create structure JSON
	structure := AudiobookStructure{
		Type:   "book",
		Tracks: tracks,
	}
	chaptersJSON, _ := json.Marshal(structure)

	// Create book record
	newBook := &database.Book{
		Filename:        filename,
		Path:            relPath,
		Format:          "folder", // Special format for folder-based audiobooks
		Filesize:        totalSize,
		CatID:           catID,
		CatType:         database.CatNormal,
		Avail:           database.AvailVerified,
		Title:           group.Title,
		IsAudiobook:     true,
		DurationSeconds: totalDuration,
		TrackCount:      len(tracks),
		Chapters:        string(chaptersJSON),
	}

	// Set cover if available
	if len(group.Cover) > 0 {
		newBook.Cover = "embedded"
		newBook.CoverType = group.CoverType
	} else if hasFolderCover(group.FolderPath) {
		newBook.Cover = "folder"
		newBook.CoverType = "image/jpeg"
	} else if hasEaDirCover(filepath.Join(group.FolderPath, group.Tracks[0].Filename)) {
		newBook.Cover = "eadir"
		newBook.CoverType = "image/jpeg"
	}

	// Insert book
	bookID, err := s.svc.AddBook(ctx, newBook)
	if err != nil {
		log.Printf("Failed to add audiobook folder %s: %v", group.FolderPath, err)
		return
	}

	// Track for duplicate detection
	s.trackNewBook(bookID)

	// Link authors - for folder audiobooks, ALWAYS prefer folder name parsing
	// because metadata "artist" is often the narrator, not the author
	author, _ := parseAudioFolderName(group.FolderName)
	if author.FirstName != "" || author.LastName != "" {
		a, err := s.svc.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
		if err == nil && a != nil {
			s.svc.AddBookAuthor(ctx, bookID, a.ID)
		}
		// Treat metadata authors as narrators (they're usually the voice actors)
		for _, metaAuthor := range group.Authors {
			// Don't add as narrator if same as author
			if metaAuthor.FirstName == author.FirstName && metaAuthor.LastName == author.LastName {
				continue
			}
			n, err := s.svc.GetOrCreateAuthor(ctx, metaAuthor.FirstName, metaAuthor.LastName)
			if err == nil && n != nil {
				s.svc.AddBookAuthorWithRole(ctx, bookID, n.ID, "narrator")
			}
		}
	} else if len(group.Authors) > 0 {
		// Fallback to metadata authors only if folder name parsing failed
		for _, metaAuthor := range group.Authors {
			a, err := s.svc.GetOrCreateAuthor(ctx, metaAuthor.FirstName, metaAuthor.LastName)
			if err == nil && a != nil {
				s.svc.AddBookAuthor(ctx, bookID, a.ID)
			}
		}
	} else {
		s.svc.AddBookAuthor(ctx, bookID, 1) // Unknown author
	}

	// Link additional narrators from metadata
	for _, narrator := range group.Narrators {
		n, err := s.svc.GetOrCreateAuthor(ctx, narrator.FirstName, narrator.LastName)
		if err == nil && n != nil {
			s.svc.AddBookAuthorWithRole(ctx, bookID, n.ID, "narrator")
		}
	}

	// Link genre if available
	if group.Genre != "" {
		g, err := s.svc.GetOrCreateGenre(ctx, group.Genre, group.Genre, "")
		if err == nil && g != nil {
			s.svc.AddBookGenre(ctx, bookID, g.ID)
		}
	}

	atomic.AddInt64(&s.stats.BooksAdded, 1)
	log.Printf("Added audiobook folder: %s (%d tracks, %s)", group.Title, len(tracks), formatDurationSeconds(totalDuration))
}

// formatDurationSeconds formats seconds as "Xh Ym"
func formatDurationSeconds(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func (s *Scanner) processPath(ctx context.Context, path string) {
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".zip" && s.config.Library.ScanZip {
		s.processZip(ctx, path)
		return
	}

	if ext == ".7z" && s.config.Library.ScanZip {
		s.process7z(ctx, path)
		return
	}

	if !s.extSet[ext] {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	s.processFile(ctx, path, info.Size(), database.CatNormal, 0)
}

func (s *Scanner) processFile(ctx context.Context, path string, size int64, catType database.CatType, catID int64) {
	filename := filepath.Base(path)
	dir := filepath.Dir(path)

	// Get relative path from library root
	relPath, err := filepath.Rel(s.config.Library.Root, dir)
	if err != nil {
		relPath = dir
	}

	// Check if book already exists
	book, err := s.svc.FindBook(ctx, filename, relPath)
	if err == nil && book != nil {
		// Book exists, mark as verified
		s.svc.UpdateBookAvail(ctx, book.ID, database.AvailVerified)
		atomic.AddInt64(&s.stats.BooksSkipped, 1)
		return
	}

	// Get or create catalog tree
	if catID == 0 && relPath != "." && relPath != "" {
		pathParts := strings.Split(relPath, string(filepath.Separator))
		catID, err = s.svc.GetOrCreateCatalogTree(ctx, pathParts, catType)
		if err != nil {
			log.Printf("Failed to create catalog tree for %s: %v", path, err)
		}
	}

	// Create new book record
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	newBook := &database.Book{
		Filename: filename,
		Path:     relPath,
		Format:   ext,
		Filesize: size,
		CatID:    catID,
		CatType:  catType,
		Avail:    database.AvailVerified,
	}

	// Parse metadata based on format
	var fb2Meta *FB2Metadata
	var audioMeta *AudioMeta
	if ext == "fb2" {
		fb2Meta = s.parseFB2MetadataFull(ctx, path, newBook, catType)
	} else if s.audioExtSet["."+ext] {
		audioMeta = s.parseAudioMetadata(ctx, path, newBook)
	} else {
		// Use filename as title for other files
		newBook.Title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	// Insert book
	bookID, err := s.svc.AddBook(ctx, newBook)
	if err != nil {
		log.Printf("Failed to add book %s: %v", path, err)
		return
	}

	// Link authors, genres, series
	if ext == "fb2" && fb2Meta != nil {
		// Link authors from FB2 metadata
		if len(fb2Meta.Authors) > 0 {
			for _, author := range fb2Meta.Authors {
				a, err := s.svc.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
				if err == nil && a != nil {
					s.svc.AddBookAuthor(ctx, bookID, a.ID)
				}
			}
		} else {
			s.svc.AddBookAuthor(ctx, bookID, 1) // Unknown author
		}
		// Link genres
		for _, genreCode := range fb2Meta.Genres {
			g, err := s.svc.GetOrCreateGenre(ctx, genreCode, "", "")
			if err == nil && g != nil {
				s.svc.AddBookGenre(ctx, bookID, g.ID)
			}
		}
		// Link series
		for _, ser := range fb2Meta.Series {
			se, err := s.svc.GetOrCreateSeries(ctx, ser.Name)
			if err == nil && se != nil {
				s.svc.AddBookSeries(ctx, bookID, se.ID, ser.Number)
			}
		}
	} else if audioMeta != nil {
		// Link author from audio folder name
		if audioMeta.Author.FirstName != "" || audioMeta.Author.LastName != "" {
			a, err := s.svc.GetOrCreateAuthor(ctx, audioMeta.Author.FirstName, audioMeta.Author.LastName)
			if err == nil && a != nil {
				s.svc.AddBookAuthor(ctx, bookID, a.ID)
			}
		} else {
			s.svc.AddBookAuthor(ctx, bookID, 1) // Unknown author
		}
	} else if ext != "fb2" {
		// Add unknown author for non-FB2
		s.svc.AddBookAuthor(ctx, bookID, 1)
	}

	atomic.AddInt64(&s.stats.BooksAdded, 1)
	s.trackNewBook(bookID)
}

// parseFB2MetadataFull parses FB2 file and returns full metadata including authors/genres/series
func (s *Scanner) parseFB2MetadataFull(ctx context.Context, path string, book *database.Book, catType database.CatType) *FB2Metadata {
	if catType != database.CatNormal {
		// For ZIP files, this function shouldn't be called
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
		return nil
	}
	defer f.Close()

	meta, err := s.parser.Parse(f)
	if err != nil {
		log.Printf("Failed to parse FB2 %s: %v", path, err)
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
		return nil
	}

	book.Title = meta.Title
	if book.Title == "" {
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
	}
	book.Annotation = meta.Annotation
	book.Lang = meta.Lang
	book.DocDate = meta.DocDate

	if len(meta.Cover) > 0 {
		book.Cover = "embedded"
		book.CoverType = meta.CoverType
	}

	return meta
}

// AudioMeta holds parsed audio metadata including author info
type AudioMeta struct {
	Author database.Author
}

func (s *Scanner) parseAudioMetadata(ctx context.Context, path string, book *database.Book) *AudioMeta {
	// Get relative path from library root to find topmost folder
	relPath, err := filepath.Rel(s.config.Library.Root, path)
	if err != nil {
		relPath = path
	}

	// Get topmost folder name (e.g., "Author - Title [Year]")
	parts := strings.Split(relPath, string(filepath.Separator))
	topmostFolder := ""
	if len(parts) > 1 {
		topmostFolder = parts[0]
	} else {
		topmostFolder = filepath.Base(filepath.Dir(path))
	}

	// Parse author and title from folder name (format: "Author - Title" or "Author_-_Title")
	author, title := parseAudioFolderName(topmostFolder)

	meta, err := s.audioParser.ParseFile(path)
	if err != nil {
		log.Printf("Failed to parse audio %s: %v", path, err)
		book.Title = title
		if book.Title == "" {
			book.Title = topmostFolder
		}
		book.IsAudiobook = true
		return &AudioMeta{Author: author}
	}

	// Set title from metadata, then parsed folder title, then folder name
	book.Title = meta.GetTitle()
	if book.Title == "" {
		book.Title = title
	}
	if book.Title == "" {
		book.Title = topmostFolder
	}

	// Set audiobook-specific fields
	book.IsAudiobook = true
	book.DurationSeconds = int(meta.EstimateDuration().Seconds())
	book.Bitrate = meta.Bitrate
	book.TrackCount = 1 // Single file

	// Set cover if available (check embedded first, then @eaDir, then folder)
	if len(meta.Cover) > 0 {
		book.Cover = "embedded"
		book.CoverType = meta.CoverType
	} else if hasEaDirCover(path) {
		book.Cover = "eadir"
		book.CoverType = "image/jpeg"
	} else if hasFolderCover(filepath.Dir(path)) {
		book.Cover = "folder"
		book.CoverType = "image/jpeg"
	}

	return &AudioMeta{Author: author}
}

// hasEaDirCover checks if Synology @eaDir has a cover for this file
func hasEaDirCover(audioPath string) bool {
	dir := filepath.Dir(audioPath)
	filename := filepath.Base(audioPath)
	eaDir := filepath.Join(dir, "@eaDir")

	patterns := []string{
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_XL.jpg"),
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_L.jpg"),
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_M.jpg"),
		filepath.Join(eaDir, filename+".jpg"),
	}

	for _, pattern := range patterns {
		if _, err := os.Stat(pattern); err == nil {
			return true
		}
	}
	return false
}

// hasFolderCover checks if the folder has a cover image file
func hasFolderCover(dir string) bool {
	patterns := []string{
		filepath.Join(dir, "cover.jpg"),
		filepath.Join(dir, "Cover.jpg"),
		filepath.Join(dir, "folder.jpg"),
		filepath.Join(dir, "Folder.jpg"),
		filepath.Join(dir, "cover.png"),
		filepath.Join(dir, "folder.png"),
		filepath.Join(dir, "@eaDir", "cover.jpg"),
		filepath.Join(dir, "@eaDir", "folder.jpg"),
	}

	for _, pattern := range patterns {
		if _, err := os.Stat(pattern); err == nil {
			return true
		}
	}
	return false
}

// parseAudioFolderName parses "Author - Title" or "Author_-_Title" format
// Returns author (first/last name) and title
func parseAudioFolderName(folderName string) (database.Author, string) {
	// Try " - " separator first
	sep := " - "
	idx := strings.Index(folderName, sep)
	if idx == -1 {
		// Try "_-_" separator
		sep = "_-_"
		idx = strings.Index(folderName, sep)
	}
	if idx == -1 {
		// Try " – " (en-dash)
		sep = " – "
		idx = strings.Index(folderName, sep)
	}

	if idx == -1 {
		// No separator found, use whole name as title
		return database.Author{}, folderName
	}

	authorPart := strings.TrimSpace(folderName[:idx])
	titlePart := strings.TrimSpace(folderName[idx+len(sep):])

	// Clean up title (remove year in brackets like "[2007]" or "(2007)")
	titlePart = strings.TrimSpace(regexp.MustCompile(`\s*[\[\(]\d{4}[\]\)]$`).ReplaceAllString(titlePart, ""))

	// Parse author name into first/last
	author := parseAuthorFullName(authorPart)

	return author, titlePart
}

// parseAuthorFullName splits "First Last" or "First_Last" into first/last name
func parseAuthorFullName(fullName string) database.Author {
	// Replace underscores with spaces
	fullName = strings.ReplaceAll(fullName, "_", " ")
	fullName = strings.TrimSpace(fullName)

	if fullName == "" {
		return database.Author{}
	}

	parts := strings.Fields(fullName)
	if len(parts) == 1 {
		return database.Author{LastName: parts[0]}
	}

	// Last word is last name, rest is first name
	return database.Author{
		FirstName: strings.Join(parts[:len(parts)-1], " "),
		LastName:  parts[len(parts)-1],
	}
}

func (s *Scanner) processZip(ctx context.Context, path string) {
	// Get relative path
	relPath, err := filepath.Rel(s.config.Library.Root, path)
	if err != nil {
		relPath = path
	}

	// Check if already scanned (using in-memory cache)
	if !s.config.Library.RescanZip {
		if _, ok := s.knownZips[relPath]; ok {
			atomic.AddInt64(&s.stats.ArchivesSkipped, 1)
			return
		}
	}

	// Open ZIP file
	zr, err := zip.OpenReader(path)
	if err != nil {
		log.Printf("Failed to open ZIP %s: %v", path, err)
		atomic.AddInt64(&s.stats.BadArchives, 1)
		return
	}
	defer zr.Close()

	// Check if this ZIP contains audio files - if so, treat as single audiobook
	if s.isAudioZip(zr) {
		s.processAudioZip(ctx, path, relPath, zr)
		return
	}

	// Create catalog entry for ZIP
	pathParts := strings.Split(relPath, string(filepath.Separator))
	catID, err := s.svc.GetOrCreateCatalogTree(ctx, pathParts, database.CatZip)
	if err != nil {
		log.Printf("Failed to create catalog for ZIP %s: %v", path, err)
		return
	}

	// Get all existing books in this catalog (1 query instead of N)
	existingBooks, err := s.svc.GetBookMapByCatalog(ctx, catID)
	if err != nil {
		log.Printf("Failed to get existing books for ZIP %s: %v", path, err)
		existingBooks = make(map[string]int64) // Continue with empty map
	}

	// Collect books for batch insert and IDs for batch availability update
	var booksToAdd []bookWithMeta
	var booksToVerify []int64
	var skippedCount int64

	// Process files in ZIP
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if !s.extSet[ext] {
			continue
		}

		// Check if file already exists (in-memory lookup)
		zipPath := relPath + "/" + f.Name
		if bookID, exists := existingBooks[zipPath]; exists {
			booksToVerify = append(booksToVerify, bookID)
			skippedCount++
			continue
		}

		// Create book record with metadata
		bwm := bookWithMeta{
			Book: &database.Book{
				Filename: f.Name,
				Path:     zipPath,
				Format:   strings.TrimPrefix(ext, "."),
				Filesize: int64(f.UncompressedSize64),
				CatID:    catID,
				CatType:  database.CatZip,
				Avail:    database.AvailVerified,
			},
		}

		// Parse FB2 if applicable
		if ext == ".fb2" {
			rc, err := f.Open()
			if err == nil {
				meta, err := s.parser.Parse(rc)
				rc.Close()
				if err == nil {
					bwm.Book.Title = meta.Title
					bwm.Book.Annotation = meta.Annotation
					bwm.Book.Lang = meta.Lang
					bwm.Book.DocDate = meta.DocDate
					if len(meta.Cover) > 0 {
						bwm.Book.Cover = "embedded"
						bwm.Book.CoverType = meta.CoverType
					}
					// Store authors/genres/series for linking after insert
					bwm.Authors = meta.Authors
					bwm.Genres = meta.Genres
					bwm.Series = meta.Series
				}
			}
		}

		if bwm.Book.Title == "" {
			bwm.Book.Title = strings.TrimSuffix(f.Name, ext)
		}

		booksToAdd = append(booksToAdd, bwm)
	}

	// Execute all database operations in a single transaction for performance
	// (reduces fsync overhead from N operations to 1)
	var addedCount int64
	var addedIDs []int64 // Track IDs for incremental duplicate detection
	if len(booksToAdd) > 0 || len(booksToVerify) > 0 {
		// Extract books for batch insert
		books := make([]*database.Book, len(booksToAdd))
		for i := range booksToAdd {
			books[i] = booksToAdd[i].Book
		}

		err = s.svc.Transaction(func(tx *persistence.Service) error {
			// Batch insert books
			if len(books) > 0 {
				bookIDs, err := tx.AddBooksBatch(ctx, books)
				if err != nil {
					return err
				}
				addedIDs = bookIDs // Capture for tracking outside transaction

				// Link authors, genres, series for each book
				for i, id := range bookIDs {
					if id <= 0 {
						continue
					}
					bwm := booksToAdd[i]

					// Link authors (or Unknown Author if none)
					if len(bwm.Authors) > 0 {
						for _, author := range bwm.Authors {
							a, err := tx.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
							if err == nil && a != nil {
								tx.AddBookAuthor(ctx, id, a.ID)
							}
						}
					} else {
						tx.AddBookAuthor(ctx, id, 1) // Unknown author
					}

					// Link genres
					for _, genreCode := range bwm.Genres {
						g, err := tx.GetOrCreateGenre(ctx, genreCode, "", "")
						if err == nil && g != nil {
							tx.AddBookGenre(ctx, id, g.ID)
						}
					}

					// Link series
					for _, ser := range bwm.Series {
						se, err := tx.GetOrCreateSeries(ctx, ser.Name)
						if err == nil && se != nil {
							tx.AddBookSeries(ctx, id, se.ID, ser.Number)
						}
					}
				}
				addedCount = int64(len(bookIDs))
			}

			// Batch update availability for existing books
			if len(booksToVerify) > 0 {
				if err := tx.UpdateBookAvailBatch(ctx, booksToVerify, database.AvailVerified); err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			log.Printf("Failed to process ZIP %s: %v", path, err)
			// Fallback to individual inserts without transaction
			for _, bwm := range booksToAdd {
				bookID, err := s.svc.AddBook(ctx, bwm.Book)
				if err != nil {
					log.Printf("Failed to add book %s: %v", bwm.Book.Filename, err)
					continue
				}
				// Link authors
				if len(bwm.Authors) > 0 {
					for _, author := range bwm.Authors {
						a, err := s.svc.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
						if err == nil && a != nil {
							s.svc.AddBookAuthor(ctx, bookID, a.ID)
						}
					}
				} else {
					s.svc.AddBookAuthor(ctx, bookID, 1)
				}
				// Link genres
				for _, genreCode := range bwm.Genres {
					g, err := s.svc.GetOrCreateGenre(ctx, genreCode, "", "")
					if err == nil && g != nil {
						s.svc.AddBookGenre(ctx, bookID, g.ID)
					}
				}
				// Link series
				for _, ser := range bwm.Series {
					se, err := s.svc.GetOrCreateSeries(ctx, ser.Name)
					if err == nil && se != nil {
						s.svc.AddBookSeries(ctx, bookID, se.ID, ser.Number)
					}
				}
				atomic.AddInt64(&s.stats.BooksAdded, 1)
				atomic.AddInt64(&s.stats.BooksInArchives, 1)
				s.trackNewBook(bookID)
			}
		} else {
			atomic.AddInt64(&s.stats.BooksAdded, addedCount)
			atomic.AddInt64(&s.stats.BooksInArchives, addedCount)
			// Track all added IDs from successful batch
			for _, id := range addedIDs {
				if id > 0 {
					s.trackNewBook(id)
				}
			}
		}
	}

	atomic.AddInt64(&s.stats.BooksSkipped, skippedCount)
	atomic.AddInt64(&s.stats.ArchivesScanned, 1)
}

// isAudioZip checks if a ZIP contains audio files
func (s *Scanner) isAudioZip(zr *zip.ReadCloser) bool {
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if s.audioExtSet[ext] {
			return true
		}
	}
	return false
}

// process7z processes a 7z archive
func (s *Scanner) process7z(ctx context.Context, path string) {
	relPath, err := filepath.Rel(s.config.Library.Root, path)
	if err != nil {
		relPath = path
	}

	// Check if already scanned (using in-memory cache)
	if !s.config.Library.RescanZip {
		if _, ok := s.knownZips[relPath]; ok {
			atomic.AddInt64(&s.stats.ArchivesSkipped, 1)
			return
		}
	}

	szr, err := sevenzip.OpenReader(path)
	if err != nil {
		log.Printf("Failed to open 7z %s: %v", path, err)
		atomic.AddInt64(&s.stats.BadArchives, 1)
		return
	}
	defer szr.Close()

	// Check if this 7z contains audio files - if so, treat as single audiobook
	if s.isAudio7z(szr) {
		s.processAudio7z(ctx, path, relPath, szr)
		return
	}

	// Create catalog entry for 7z
	pathParts := strings.Split(relPath, string(filepath.Separator))
	catID, err := s.svc.GetOrCreateCatalogTree(ctx, pathParts, database.CatZip)
	if err != nil {
		log.Printf("Failed to create catalog for 7z %s: %v", path, err)
		return
	}

	// Get all existing books in this catalog (1 query instead of N)
	existingBooks, err := s.svc.GetBookMapByCatalog(ctx, catID)
	if err != nil {
		log.Printf("Failed to get existing books for 7z %s: %v", path, err)
		existingBooks = make(map[string]int64)
	}

	// Collect books for batch insert
	var booksToAdd []bookWithMeta
	var booksToVerify []int64
	var skippedCount int64

	for _, f := range szr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if !s.extSet[ext] {
			continue
		}

		// Check if file already exists
		szPath := relPath + "/" + f.Name
		if bookID, exists := existingBooks[szPath]; exists {
			booksToVerify = append(booksToVerify, bookID)
			skippedCount++
			continue
		}

		// Create book record
		bwm := bookWithMeta{
			Book: &database.Book{
				Filename: f.Name,
				Path:     szPath,
				Format:   strings.TrimPrefix(ext, "."),
				Filesize: int64(f.UncompressedSize),
				CatID:    catID,
				CatType:  database.CatZip,
				Avail:    database.AvailVerified,
			},
		}

		// Parse FB2 if applicable
		if ext == ".fb2" {
			rc, err := f.Open()
			if err == nil {
				meta, err := s.parser.Parse(rc)
				rc.Close()
				if err == nil {
					bwm.Book.Title = meta.Title
					bwm.Book.Annotation = meta.Annotation
					bwm.Book.Lang = meta.Lang
					bwm.Book.DocDate = meta.DocDate
					if len(meta.Cover) > 0 {
						bwm.Book.Cover = "embedded"
						bwm.Book.CoverType = meta.CoverType
					}
					bwm.Authors = meta.Authors
					bwm.Genres = meta.Genres
					bwm.Series = meta.Series
				}
			}
		}

		if bwm.Book.Title == "" {
			bwm.Book.Title = strings.TrimSuffix(f.Name, ext)
		}

		booksToAdd = append(booksToAdd, bwm)
	}

	// Execute all database operations in a single transaction
	var addedCount int64
	var addedIDs []int64 // Track IDs for incremental duplicate detection
	if len(booksToAdd) > 0 || len(booksToVerify) > 0 {
		books := make([]*database.Book, len(booksToAdd))
		for i := range booksToAdd {
			books[i] = booksToAdd[i].Book
		}

		err = s.svc.Transaction(func(tx *persistence.Service) error {
			if len(books) > 0 {
				bookIDs, err := tx.AddBooksBatch(ctx, books)
				if err != nil {
					return fmt.Errorf("batch insert: %w", err)
				}
				addedIDs = bookIDs // Capture for tracking outside transaction

				// Link authors, genres, series
				for i, bwm := range booksToAdd {
					if i >= len(bookIDs) {
						break
					}
					bookID := bookIDs[i]
					if bookID == 0 {
						continue
					}

					// Link authors
					if len(bwm.Authors) > 0 {
						for _, a := range bwm.Authors {
							author, err := tx.GetOrCreateAuthor(ctx, a.FirstName, a.LastName)
							if err == nil && author != nil && author.ID != 0 {
								tx.AddBookAuthor(ctx, bookID, author.ID)
							}
						}
					} else {
						tx.AddBookAuthor(ctx, bookID, 1) // Unknown author
					}

					// Link genres
					for _, genreCode := range bwm.Genres {
						g, err := tx.GetOrCreateGenre(ctx, genreCode, "", "")
						if err == nil && g != nil {
							tx.AddBookGenre(ctx, bookID, g.ID)
						}
					}

					// Link series
					for _, si := range bwm.Series {
						series, err := tx.GetOrCreateSeries(ctx, si.Name)
						if err == nil && series != nil {
							tx.AddBookSeries(ctx, bookID, series.ID, si.Number)
						}
					}
				}
				addedCount = int64(len(bookIDs))
			}

			// Update availability for existing books
			if len(booksToVerify) > 0 {
				if err := tx.UpdateBookAvailBatch(ctx, booksToVerify, database.AvailVerified); err != nil {
					log.Printf("Failed to update availability for 7z %s: %v", path, err)
				}
			}

			return nil
		})

		if err != nil {
			log.Printf("Transaction failed for 7z %s: %v", path, err)
		} else {
			atomic.AddInt64(&s.stats.BooksAdded, addedCount)
			atomic.AddInt64(&s.stats.BooksInArchives, addedCount)
			// Track all added IDs for incremental duplicate detection
			for _, id := range addedIDs {
				if id > 0 {
					s.trackNewBook(id)
				}
			}
		}
	}

	atomic.AddInt64(&s.stats.BooksSkipped, skippedCount)
	atomic.AddInt64(&s.stats.ArchivesScanned, 1)
}

// isAudio7z checks if a 7z contains audio files
func (s *Scanner) isAudio7z(szr *sevenzip.ReadCloser) bool {
	for _, f := range szr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if s.audioExtSet[ext] {
			return true
		}
	}
	return false
}

// processAudio7z processes a 7z containing audio files as a single audiobook
func (s *Scanner) processAudio7z(ctx context.Context, path, relPath string, szr *sevenzip.ReadCloser) {
	// Check if this audiobook already exists
	existingBook, err := s.svc.FindBook(ctx, filepath.Base(path), filepath.Dir(relPath))
	if err == nil && existingBook != nil {
		s.svc.UpdateBookAvail(ctx, existingBook.ID, database.AvailVerified)
		atomic.AddInt64(&s.stats.BooksSkipped, 1)
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Create catalog entry for the 7z's parent directory
	dir := filepath.Dir(relPath)
	var catID int64
	if dir != "." && dir != "" {
		pathParts := strings.Split(dir, string(filepath.Separator))
		catID, err = s.svc.GetOrCreateCatalogTree(ctx, pathParts, database.CatNormal)
		if err != nil {
			log.Printf("Failed to create catalog for audio 7z %s: %v", path, err)
		}
	}

	// First pass: find top-level folder and collect audio files
	type trackInfo struct {
		name     string
		relPath  string // path relative to top-level folder (for structure)
		fullPath string // full path inside archive (for download)
		duration int
		size     int64
	}
	var tracks []trackInfo
	var totalSize int64
	var totalDuration int
	var topLevelFolder string

	for _, f := range szr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if !s.audioExtSet[ext] {
			continue
		}

		// Find top-level folder from first audio file
		if topLevelFolder == "" {
			parts := strings.Split(f.Name, "/")
			if len(parts) > 1 {
				topLevelFolder = parts[0]
			}
		}

		// Get path relative to top-level folder
		relFilePath := f.Name
		if topLevelFolder != "" && strings.HasPrefix(f.Name, topLevelFolder+"/") {
			relFilePath = strings.TrimPrefix(f.Name, topLevelFolder+"/")
		}

		// Extract actual duration from audio file
		format := strings.TrimPrefix(ext, ".")
		duration := 0
		fileSize := int64(f.UncompressedSize)

		// For M4B/M4A, only read first 10MB to find moov atom (if faststart)
		// For smaller files, read entire file
		maxRead := fileSize
		if (format == "m4b" || format == "m4a" || format == "aac" || format == "mp4") && fileSize > 10*1024*1024 {
			maxRead = 10 * 1024 * 1024 // 10MB should be enough for moov at beginning
		}

		rc, err := f.Open()
		if err == nil {
			data, err := io.ReadAll(io.LimitReader(rc, maxRead))
			rc.Close()
			if err == nil && len(data) > 0 {
				r := bytes.NewReader(data)
				if dur, err := GetAudioDurationFromReader(r, fileSize, format); err == nil && dur > 0 {
					duration = int(dur.Seconds())
				}
			}
		}

		// Fallback to estimation if extraction failed
		if duration == 0 {
			// Use realistic bitrates for estimation
			bitrate := 128 // default for MP3
			switch format {
			case "m4b", "m4a", "aac", "mp4":
				bitrate = 96 // AAC audiobooks typically 64-128kbps, use middle
			case "flac":
				bitrate = 800
			case "ogg", "opus":
				bitrate = 96
			}
			duration = int(float64(fileSize) * 8 / float64(bitrate*1000))
		}

		tracks = append(tracks, trackInfo{
			name:     filepath.Base(f.Name),
			relPath:  relFilePath,
			fullPath: f.Name,
			duration: duration,
			size:     int64(f.UncompressedSize),
		})
		totalSize += int64(f.UncompressedSize)
		totalDuration += duration
	}

	if len(tracks) == 0 {
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Parse author and title from top-level folder name
	var author database.Author
	var title string
	if topLevelFolder != "" {
		author, title = parseAudioFolderName(topLevelFolder)
	}
	if title == "" {
		// Fallback to 7z filename
		szName := strings.TrimSuffix(filepath.Base(path), ".7z")
		author, title = parseAudioFolderName(szName)
		if title == "" {
			title = szName
		}
	}

	// Build audiobook structure
	structure := AudiobookStructure{}

	hasSubdirs := false
	for _, t := range tracks {
		if strings.Contains(t.relPath, "/") {
			hasSubdirs = true
			break
		}
	}

	if hasSubdirs {
		structure.Type = "collection"
		partMap := make(map[string]*AudiobookPart)
		var partOrder []string

		for _, t := range tracks {
			partName := filepath.Dir(t.relPath)
			if idx := strings.Index(partName, "/"); idx > 0 {
				partName = partName[:idx]
			}

			if _, exists := partMap[partName]; !exists {
				partMap[partName] = &AudiobookPart{Name: partName}
				partOrder = append(partOrder, partName)
			}
			part := partMap[partName]
			part.Tracks = append(part.Tracks, AudiobookTrack{
				Name:     t.name,
				Path:     t.fullPath,
				Duration: t.duration,
				Size:     t.size,
			})
			part.Duration += t.duration
		}

		for _, name := range partOrder {
			structure.Parts = append(structure.Parts, *partMap[name])
		}
	} else {
		structure.Type = "book"
		for _, t := range tracks {
			structure.Tracks = append(structure.Tracks, AudiobookTrack{
				Name:     t.name,
				Path:     t.fullPath,
				Duration: t.duration,
				Size:     t.size,
			})
		}
	}

	// Serialize structure to JSON
	chaptersJSON, err := json.Marshal(structure)
	if err != nil {
		log.Printf("Failed to marshal audiobook structure for %s: %v", path, err)
		chaptersJSON = nil
	}
	chaptersStr := string(chaptersJSON)

	// Create the audiobook entry
	book := &database.Book{
		Filename:        filepath.Base(path),
		Path:            filepath.Dir(relPath),
		Format:          "7z",
		Filesize:        totalSize,
		CatID:           catID,
		CatType:         database.CatNormal,
		Avail:           database.AvailVerified,
		Title:           title,
		IsAudiobook:     true,
		DurationSeconds: totalDuration,
		TrackCount:      len(tracks),
		Chapters:        chaptersStr,
	}

	// Insert book
	bookID, err := s.svc.AddBook(ctx, book)
	if err != nil {
		log.Printf("Failed to add audiobook %s: %v", path, err)
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Link author
	if author.FirstName != "" || author.LastName != "" {
		a, err := s.svc.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
		if err == nil && a != nil {
			s.svc.AddBookAuthor(ctx, bookID, a.ID)
		}
	} else {
		s.svc.AddBookAuthor(ctx, bookID, 1) // Unknown author
	}

	atomic.AddInt64(&s.stats.BooksAdded, 1)
	atomic.AddInt64(&s.stats.ArchivesScanned, 1)
	s.trackNewBook(bookID)
}

// AudiobookStructure represents the structure of an audiobook ZIP
type AudiobookStructure struct {
	Type  string           `json:"type"` // "book" or "collection"
	Parts []AudiobookPart  `json:"parts,omitempty"`
	Tracks []AudiobookTrack `json:"tracks,omitempty"`
}

// AudiobookPart represents a part/section of a collection
type AudiobookPart struct {
	Name     string           `json:"name"`
	Duration int              `json:"duration"` // seconds
	Tracks   []AudiobookTrack `json:"tracks"`
}

// AudiobookTrack represents a single audio track
type AudiobookTrack struct {
	Name     string `json:"name"`
	Path     string `json:"path"`     // full path inside archive
	Duration int    `json:"duration"` // seconds
	Size     int64  `json:"size"`     // bytes
}

// processAudioZip processes a ZIP containing audio files as a single audiobook
// Expected structure: TopFolder (Author - Title) / [SubFolders (parts)] / audio files
func (s *Scanner) processAudioZip(ctx context.Context, path, relPath string, zr *zip.ReadCloser) {
	// Check if this audiobook already exists
	existingBook, err := s.svc.FindBook(ctx, filepath.Base(path), filepath.Dir(relPath))
	if err == nil && existingBook != nil {
		s.svc.UpdateBookAvail(ctx, existingBook.ID, database.AvailVerified)
		atomic.AddInt64(&s.stats.BooksSkipped, 1)
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Create catalog entry for the ZIP's parent directory
	dir := filepath.Dir(relPath)
	var catID int64
	if dir != "." && dir != "" {
		pathParts := strings.Split(dir, string(filepath.Separator))
		catID, err = s.svc.GetOrCreateCatalogTree(ctx, pathParts, database.CatNormal)
		if err != nil {
			log.Printf("Failed to create catalog for audio ZIP %s: %v", path, err)
		}
	}

	// First pass: find top-level folder and collect audio files
	type trackInfo struct {
		name     string
		relPath  string // path relative to top-level folder (for structure)
		fullPath string // full path inside archive (for download)
		duration int
		size     int64
	}
	var tracks []trackInfo
	var totalSize int64
	var totalDuration int
	var topLevelFolder string

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if !s.audioExtSet[ext] {
			continue
		}

		// Find top-level folder from first audio file
		if topLevelFolder == "" {
			parts := strings.Split(f.Name, "/")
			if len(parts) > 1 {
				topLevelFolder = parts[0]
			}
		}

		// Get path relative to top-level folder
		relFilePath := f.Name
		if topLevelFolder != "" && strings.HasPrefix(f.Name, topLevelFolder+"/") {
			relFilePath = strings.TrimPrefix(f.Name, topLevelFolder+"/")
		}

		// Extract actual duration from audio file
		format := strings.TrimPrefix(ext, ".")
		duration := 0
		fileSize := int64(f.UncompressedSize64)

		// For M4B/M4A, only read first 10MB to find moov atom (if faststart)
		// For smaller files, read entire file
		maxRead := fileSize
		if (format == "m4b" || format == "m4a" || format == "aac" || format == "mp4") && fileSize > 10*1024*1024 {
			maxRead = 10 * 1024 * 1024 // 10MB should be enough for moov at beginning
		}

		rc, err := f.Open()
		if err == nil {
			data, err := io.ReadAll(io.LimitReader(rc, maxRead))
			rc.Close()
			if err == nil && len(data) > 0 {
				r := bytes.NewReader(data)
				if dur, err := GetAudioDurationFromReader(r, fileSize, format); err == nil && dur > 0 {
					duration = int(dur.Seconds())
				}
			}
		}

		// Fallback to estimation if extraction failed
		if duration == 0 {
			// Use realistic bitrates for estimation
			bitrate := 128 // default for MP3
			switch format {
			case "m4b", "m4a", "aac", "mp4":
				bitrate = 96 // AAC audiobooks typically 64-128kbps, use middle
			case "flac":
				bitrate = 800
			case "ogg", "opus":
				bitrate = 96
			}
			duration = int(float64(fileSize) * 8 / float64(bitrate*1000))
		}

		tracks = append(tracks, trackInfo{
			name:     filepath.Base(f.Name),
			relPath:  relFilePath,
			fullPath: f.Name,
			duration: duration,
			size:     fileSize,
		})
		totalSize += fileSize
		totalDuration += duration
	}

	if len(tracks) == 0 {
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Parse author and title from top-level folder name
	var author database.Author
	var title string
	if topLevelFolder != "" {
		author, title = parseAudioFolderName(topLevelFolder)
	}
	if title == "" {
		// Fallback to ZIP filename
		zipName := strings.TrimSuffix(filepath.Base(path), ".zip")
		author, title = parseAudioFolderName(zipName)
		if title == "" {
			title = zipName
		}
	}

	// Build audiobook structure
	structure := AudiobookStructure{}

	// Check if files are in subdirectories (under top-level folder) = collection
	// or directly in top-level folder = simple book
	hasSubdirs := false
	for _, t := range tracks {
		if strings.Contains(t.relPath, "/") {
			hasSubdirs = true
			break
		}
	}

	if hasSubdirs {
		// Collection: group by first subdirectory (part/chapter name)
		structure.Type = "collection"
		partMap := make(map[string]*AudiobookPart)
		var partOrder []string

		for _, t := range tracks {
			// Get first directory component as part name
			partName := filepath.Dir(t.relPath)
			if idx := strings.Index(partName, "/"); idx > 0 {
				partName = partName[:idx]
			}

			if _, exists := partMap[partName]; !exists {
				partMap[partName] = &AudiobookPart{Name: partName}
				partOrder = append(partOrder, partName)
			}
			part := partMap[partName]
			part.Tracks = append(part.Tracks, AudiobookTrack{
				Name:     t.name,
				Path:     t.fullPath,
				Duration: t.duration,
				Size:     t.size,
			})
			part.Duration += t.duration
		}

		for _, name := range partOrder {
			structure.Parts = append(structure.Parts, *partMap[name])
		}
	} else {
		// Simple book: flat list of tracks
		structure.Type = "book"
		for _, t := range tracks {
			structure.Tracks = append(structure.Tracks, AudiobookTrack{
				Name:     t.name,
				Path:     t.fullPath,
				Duration: t.duration,
				Size:     t.size,
			})
		}
	}

	// Serialize structure to JSON
	chaptersJSON, err := json.Marshal(structure)
	if err != nil {
		log.Printf("Failed to marshal audiobook structure for %s: %v", path, err)
		chaptersJSON = nil
	}
	chaptersStr := string(chaptersJSON)

	// Create the audiobook entry
	book := &database.Book{
		Filename:        filepath.Base(path),
		Path:            filepath.Dir(relPath),
		Format:          "zip",
		Filesize:        totalSize,
		CatID:           catID,
		CatType:         database.CatNormal,
		Avail:           database.AvailVerified,
		Title:           title,
		IsAudiobook:     true,
		DurationSeconds: totalDuration,
		TrackCount:      len(tracks),
		Chapters:        chaptersStr,
	}

	// Insert book
	bookID, err := s.svc.AddBook(ctx, book)
	if err != nil {
		log.Printf("Failed to add audiobook %s: %v", path, err)
		atomic.AddInt64(&s.stats.ArchivesScanned, 1)
		return
	}

	// Link author
	if author.FirstName != "" || author.LastName != "" {
		a, err := s.svc.GetOrCreateAuthor(ctx, author.FirstName, author.LastName)
		if err == nil && a != nil {
			s.svc.AddBookAuthor(ctx, bookID, a.ID)
		}
	} else {
		s.svc.AddBookAuthor(ctx, bookID, 1) // Unknown author
	}

	atomic.AddInt64(&s.stats.BooksAdded, 1)
	atomic.AddInt64(&s.stats.ArchivesScanned, 1)
	s.trackNewBook(bookID)
}
