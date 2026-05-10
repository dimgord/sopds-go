package server

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/dimgord/sopds-go/internal/database"
)

// BookPathInfo contains parsed path information for a book
type BookPathInfo struct {
	FullPath     string // Full filesystem path to the file or archive
	ArchivePath  string // Path to the archive file (empty if not in archive)
	InternalPath string // Path inside the archive (empty if not in archive)
	IsInArchive  bool   // True if book is inside an archive
	ArchiveType  string // "zip", "7z", or "" if not in archive
}

// getBookPath returns the full filesystem path for a book's file or archive
func (s *Server) getBookPath(book *database.Book) string {
	if book.Path == "" {
		return filepath.Join(s.config.Library.Root, book.Filename)
	}
	return filepath.Join(s.config.Library.Root, book.Path, book.Filename)
}

// getBookDir returns the directory containing the book
func (s *Server) getBookDir(book *database.Book) string {
	return filepath.Join(s.config.Library.Root, book.Path)
}

// parseBookPath parses book path information, determining if it's in an archive
func (s *Server) parseBookPath(book *database.Book) BookPathInfo {
	info := BookPathInfo{}

	// Check if the book itself is an archive file (standalone audiobook archive)
	ext := strings.ToLower(filepath.Ext(book.Filename))
	if ext == ".zip" || ext == ".7z" {
		info.FullPath = s.getBookPath(book)
		info.ArchivePath = info.FullPath
		info.ArchiveType = strings.TrimPrefix(ext, ".")
		info.IsInArchive = false // It's an archive, but not *inside* an archive
		return info
	}

	// Check if book is inside an archive (path contains .zip or .7z)
	parts := strings.Split(book.Path, string(filepath.Separator))
	for i, part := range parts {
		partLower := strings.ToLower(part)
		if strings.HasSuffix(partLower, ".zip") {
			info.IsInArchive = true
			info.ArchiveType = "zip"
			info.ArchivePath = filepath.Join(s.config.Library.Root, filepath.Join(parts[:i+1]...))
			if i+1 < len(parts) {
				info.InternalPath = filepath.Join(append(parts[i+1:], book.Filename)...)
			} else {
				info.InternalPath = book.Filename
			}
			info.FullPath = info.ArchivePath
			return info
		}
		if strings.HasSuffix(partLower, ".7z") {
			info.IsInArchive = true
			info.ArchiveType = "7z"
			info.ArchivePath = filepath.Join(s.config.Library.Root, filepath.Join(parts[:i+1]...))
			if i+1 < len(parts) {
				info.InternalPath = filepath.Join(append(parts[i+1:], book.Filename)...)
			} else {
				info.InternalPath = book.Filename
			}
			info.FullPath = info.ArchivePath
			return info
		}
	}

	// Regular file, not in archive
	info.FullPath = s.getBookPath(book)
	info.IsInArchive = false
	return info
}

// getTrackPath returns the full filesystem path for an audiobook track
// For folder audiobooks, trackPath may be relative to book.Path or just a filename
func (s *Server) getTrackPath(book *database.Book, trackPath string) string {
	// If trackPath is already absolute, return it
	if filepath.IsAbs(trackPath) {
		return trackPath
	}

	// For folder-based audiobooks (format="folder"), construct path from book.Path + trackPath
	// book.Path is the parent directory, book.Filename is the folder name
	if book.Format == "folder" {
		// Try: library/book.Path/trackPath (if trackPath includes folder)
		// Or: library/book.Path/book.Filename/trackPath
		fullPath := filepath.Join(s.config.Library.Root, book.Path, trackPath)
		return fullPath
	}

	// For archive audiobooks, this shouldn't be called (use archive methods instead)
	return filepath.Join(s.config.Library.Root, book.Path, book.Filename, trackPath)
}

// isAudioExtension returns true if the extension is an audio format
func isAudioExtension(ext string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	switch ext {
	case "mp3", "m4b", "m4a", "flac", "ogg", "opus", "wav", "aac", "awb":
		return true
	}
	return false
}

// isArchiveExtension returns true if the extension is an archive format
func isArchiveExtension(ext string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	return ext == "zip" || ext == "7z"
}

// readFromArchive reads a book file from a ZIP or 7z archive
func (s *Server) readFromArchive(book *database.Book) ([]byte, error) {
	pathInfo := s.parseBookPath(book)

	if !pathInfo.IsInArchive && pathInfo.ArchiveType == "" {
		return nil, fmt.Errorf("book not in archive")
	}

	switch pathInfo.ArchiveType {
	case "zip":
		return s.readFileFromZip(pathInfo.ArchivePath, pathInfo.InternalPath, book.Filename)
	case "7z":
		return s.readFileFrom7z(pathInfo.ArchivePath, pathInfo.InternalPath, book.Filename)
	default:
		return nil, fmt.Errorf("unsupported archive type: %s", pathInfo.ArchiveType)
	}
}

// readFileFromZip reads a file from a ZIP archive
func (s *Server) readFileFromZip(archivePath, internalPath, filename string) ([]byte, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == internalPath || f.Name == filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("file not found in ZIP")
}

// readFileFrom7z reads a file from a 7z archive
func (s *Server) readFileFrom7z(archivePath, internalPath, filename string) ([]byte, error) {
	sz, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer sz.Close()

	for _, f := range sz.File {
		if f.Name == internalPath || f.Name == filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("file not found in 7z")
}

// BookLinks contains related entities for a book (authors, genres, series)
type BookLinks struct {
	Authors []database.Author
	Genres  []database.Genre
	Series  []database.BookSeries
}

// getBookLinks fetches all related entities for a book
func (s *Server) getBookLinks(ctx context.Context, bookID int64) BookLinks {
	authors, _ := s.svc.GetBookAuthors(ctx, bookID)
	genres, _ := s.svc.GetBookGenres(ctx, bookID)
	series, _ := s.svc.GetBookSeries(ctx, bookID)
	return BookLinks{
		Authors: authors,
		Genres:  genres,
		Series:  series,
	}
}

// getTotalPages calculates total pages from pagination
func getTotalPages(pagination *database.Pagination) int {
	return int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
}
