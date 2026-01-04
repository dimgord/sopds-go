package scanner

import (
	"archive/zip"
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/sopds/sopds-go/internal/config"
	"github.com/sopds/sopds-go/internal/database"
)

// Scanner scans library directory for books
type Scanner struct {
	config           *config.Config
	db               *database.DB
	parser           *FB2Parser
	cron             *cron.Cron
	extSet           map[string]bool
	stats            ScanStats
	statsMu          sync.Mutex
	isRunning        atomic.Bool
	progressCallback ProgressCallback
	confirmCallback  ConfirmCallback
	lastProgress     time.Time
	knownZips        map[string]int64 // path -> cat_id cache
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
func New(cfg *config.Config, db *database.DB) *Scanner {
	// Build extension set
	extSet := make(map[string]bool)
	for _, ext := range cfg.Library.Formats {
		extSet[strings.ToLower(ext)] = true
	}

	return &Scanner{
		config: cfg,
		db:     db,
		parser: NewFB2Parser(cfg.Server.Port > 0), // Read covers if server is enabled
		extSet: extSet,
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
	if now.Sub(s.lastProgress) < 500*time.Millisecond {
		return // Rate limit progress updates
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
		if ext == ".zip" && s.config.Library.ScanZip {
			count++
		} else if s.extSet[ext] {
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
	s.knownZips = make(map[string]int64)
	if !s.config.Library.RescanZip {
		var err error
		s.knownZips, err = s.db.GetAllZipCatalogs(ctx)
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

				// Ask for confirmation if callback is set
				if s.confirmCallback != nil {
					if !s.confirmCallback(msg) {
						log.Printf("Skipping removal of %d archives (user cancelled)", len(removedCatIDs))
						// Restore the paths to knownZips so they are not re-scanned
						for i, path := range removedPaths {
							s.knownZips[path] = removedCatIDs[i]
						}
						removedCatIDs = nil
					}
				}

				if len(removedCatIDs) > 0 {
					log.Printf("Removing %d deleted archives from database...", len(removedCatIDs))
					deletedCount, err := s.db.DeleteBooksInCatalogs(ctx, removedCatIDs)
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

	// Walk directory and send files to workers
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

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".zip" && s.config.Library.ScanZip {
			fileChan <- path
		} else if s.extSet[ext] {
			fileChan <- path
		}
		return nil
	})

	close(fileChan)
	wg.Wait()

	if err != nil {
		log.Printf("Scan walk error: %v", err)
	}

	// Mark duplicates
	if s.config.Scanner.Duplicates != "none" {
		mode := database.DupNormal
		switch s.config.Scanner.Duplicates {
		case "strong":
			mode = database.DupStrong
		case "clear":
			mode = database.DupClear
		}
		if err := s.db.MarkDuplicates(ctx, mode); err != nil {
			log.Printf("Failed to mark duplicates: %v", err)
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

func (s *Scanner) processPath(ctx context.Context, path string) {
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".zip" && s.config.Library.ScanZip {
		s.processZip(ctx, path)
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
	book, err := s.db.FindBook(ctx, filename, relPath)
	if err == nil && book != nil {
		// Book exists, mark as verified
		s.db.UpdateBookAvail(ctx, book.ID, database.AvailVerified)
		atomic.AddInt64(&s.stats.BooksSkipped, 1)
		return
	}

	// Get or create catalog tree
	if catID == 0 && relPath != "." && relPath != "" {
		pathParts := strings.Split(relPath, string(filepath.Separator))
		catID, err = s.db.GetOrCreateCatalogTree(ctx, pathParts, catType)
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

	// Parse FB2 metadata if applicable
	if ext == "fb2" {
		s.parseFB2Metadata(ctx, path, newBook, catType)
	} else {
		// Use filename as title for non-FB2 files
		newBook.Title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	// Insert book
	bookID, err := s.db.AddBook(ctx, newBook)
	if err != nil {
		log.Printf("Failed to add book %s: %v", path, err)
		return
	}

	// Add author if no authors were found during parsing
	if newBook.Title != "" && ext != "fb2" {
		// Add unknown author for non-FB2
		s.db.AddBookAuthor(ctx, bookID, 1) // Unknown author ID = 1
	}

	atomic.AddInt64(&s.stats.BooksAdded, 1)
}

func (s *Scanner) parseFB2Metadata(ctx context.Context, path string, book *database.Book, catType database.CatType) {
	var reader interface {
		Read([]byte) (int, error)
		Close() error
	}

	if catType == database.CatNormal {
		f, err := os.Open(path)
		if err != nil {
			book.Title = strings.TrimSuffix(book.Filename, ".fb2")
			return
		}
		defer f.Close()
		reader = f
	}

	// For ZIP files, the reader is passed from processZip

	if reader == nil {
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
		return
	}

	meta, err := s.parser.Parse(reader)
	if err != nil {
		log.Printf("Failed to parse FB2 %s: %v", path, err)
		book.Title = strings.TrimSuffix(book.Filename, ".fb2")
		return
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

	// Store metadata for later linking (after book is inserted)
	// This is handled in processFile after AddBook
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

	// Create catalog entry for ZIP
	pathParts := strings.Split(relPath, string(filepath.Separator))
	catID, err := s.db.GetOrCreateCatalogTree(ctx, pathParts, database.CatZip)
	if err != nil {
		log.Printf("Failed to create catalog for ZIP %s: %v", path, err)
		return
	}

	// Collect books for batch insert
	var booksToAdd []*database.Book
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

		// Check if file already exists
		zipPath := relPath + "/" + f.Name
		book, err := s.db.FindBook(ctx, f.Name, zipPath)
		if err == nil && book != nil {
			s.db.UpdateBookAvail(ctx, book.ID, database.AvailVerified)
			skippedCount++
			continue
		}

		// Create book record
		newBook := &database.Book{
			Filename: f.Name,
			Path:     zipPath,
			Format:   strings.TrimPrefix(ext, "."),
			Filesize: int64(f.UncompressedSize64),
			CatID:    catID,
			CatType:  database.CatZip,
			Avail:    database.AvailVerified,
		}

		// Parse FB2 if applicable
		if ext == ".fb2" {
			rc, err := f.Open()
			if err == nil {
				meta, err := s.parser.Parse(rc)
				rc.Close()
				if err == nil {
					newBook.Title = meta.Title
					newBook.Annotation = meta.Annotation
					newBook.Lang = meta.Lang
					newBook.DocDate = meta.DocDate
					if len(meta.Cover) > 0 {
						newBook.Cover = "embedded"
						newBook.CoverType = meta.CoverType
					}
				}
			}
		}

		if newBook.Title == "" {
			newBook.Title = strings.TrimSuffix(f.Name, ext)
		}

		booksToAdd = append(booksToAdd, newBook)
	}

	// Batch insert books
	if len(booksToAdd) > 0 {
		bookIDs, err := s.db.AddBooksBatch(ctx, booksToAdd)
		if err != nil {
			log.Printf("Failed to batch insert books from ZIP %s: %v", path, err)
			// Fallback to individual inserts
			for _, book := range booksToAdd {
				bookID, err := s.db.AddBook(ctx, book)
				if err != nil {
					log.Printf("Failed to add book %s: %v", book.Filename, err)
					continue
				}
				s.db.AddBookAuthor(ctx, bookID, 1)
				atomic.AddInt64(&s.stats.BooksAdded, 1)
				atomic.AddInt64(&s.stats.BooksInArchives, 1)
			}
		} else {
			// Batch insert author links
			authorPairs := make([][2]int64, len(bookIDs))
			for i, id := range bookIDs {
				authorPairs[i] = [2]int64{id, 1} // Unknown author ID = 1
			}
			if err := s.db.AddBookAuthorsBatch(ctx, authorPairs); err != nil {
				log.Printf("Failed to batch insert authors: %v", err)
			}
			atomic.AddInt64(&s.stats.BooksAdded, int64(len(bookIDs)))
			atomic.AddInt64(&s.stats.BooksInArchives, int64(len(bookIDs)))
		}
	}

	atomic.AddInt64(&s.stats.BooksSkipped, skippedCount)
	atomic.AddInt64(&s.stats.ArchivesScanned, 1)
}
