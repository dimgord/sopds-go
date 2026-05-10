package scanner

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dimgord/sopds-go/internal/database"
)

// AudioTrack represents a single audio file in a multi-file audiobook
type AudioTrack struct {
	Filename string
	Path     string
	Track    int           // Track number from metadata
	Disc     int           // Disc number from metadata
	Duration time.Duration // Estimated duration
	Size     int64         // File size
	Format   string        // Audio format
}

// AudiobookGroup represents a multi-file audiobook grouped by folder
type AudiobookGroup struct {
	FolderPath    string          // Relative path to folder
	FolderName    string          // Folder name (fallback title)
	Title         string          // Audiobook title from metadata
	Authors       []database.Author
	Narrators     []database.Author
	Tracks        []AudioTrack
	TotalDuration time.Duration
	TotalSize     int64
	Format        string          // Primary format (most common among tracks)
	Cover         []byte
	CoverType     string
	Year          int
	Genre         string
}

// AudiobookGrouper groups audio files by folder into audiobooks
type AudiobookGrouper struct {
	parser *AudioParser
}

// NewAudiobookGrouper creates a new audiobook grouper
func NewAudiobookGrouper(parser *AudioParser) *AudiobookGrouper {
	return &AudiobookGrouper{parser: parser}
}

// GroupByFolder groups audio files in the same folder into audiobook groups
// Returns grouped audiobooks and any ungrouped single files
func (g *AudiobookGrouper) GroupByFolder(files []string) ([]AudiobookGroup, []string) {
	// Group files by parent directory
	byFolder := make(map[string][]string)
	for _, f := range files {
		dir := filepath.Dir(f)
		byFolder[dir] = append(byFolder[dir], f)
	}

	var groups []AudiobookGroup
	var singles []string

	for folder, folderFiles := range byFolder {
		if len(folderFiles) == 1 {
			// Single file in folder - not a multi-file audiobook
			singles = append(singles, folderFiles[0])
			continue
		}

		// Multiple files - create audiobook group
		group := g.createGroup(folder, folderFiles)
		if group != nil {
			groups = append(groups, *group)
		} else {
			// Couldn't create group, treat as singles
			singles = append(singles, folderFiles...)
		}
	}

	return groups, singles
}

// createGroup creates an AudiobookGroup from files in a folder
func (g *AudiobookGrouper) createGroup(folder string, files []string) *AudiobookGroup {
	if len(files) == 0 {
		return nil
	}

	folderName := filepath.Base(folder)
	group := &AudiobookGroup{
		FolderPath: folder,
		FolderName: folderName,
	}

	// Parse title from folder name first (format: "Author - Title")
	// This is more reliable than metadata for folder-based audiobooks
	group.Title = parseTitleFromFolderName(folderName)

	// Track format counts to determine primary format
	formatCounts := make(map[string]int)

	// Parse each file and collect tracks
	for _, f := range files {
		meta, err := g.parser.ParseFile(f)
		if err != nil {
			continue
		}

		track := AudioTrack{
			Filename: filepath.Base(f),
			Path:     f,
			Track:    meta.Track,
			Disc:     meta.Disc,
			Duration: meta.EstimateDuration(),
			Size:     meta.Filesize,
			Format:   meta.Format,
		}

		group.Tracks = append(group.Tracks, track)
		group.TotalDuration += track.Duration
		group.TotalSize += track.Size
		formatCounts[meta.Format]++

		// For folder audiobooks, treat metadata "artist" as narrator, not author
		// Authors should come from folder name parsing in processAudioGroup
		if len(group.Narrators) == 0 {
			// First check explicit narrator field
			if narrator := meta.GetNarrator(); narrator != nil {
				group.Narrators = []database.Author{*narrator}
			} else {
				// Use artist from metadata as narrator (voice actor)
				group.Authors = meta.GetAuthors()
			}
		}
		if group.Year == 0 && meta.Year > 0 {
			group.Year = meta.Year
		}
		if group.Genre == "" && meta.Genre != "" {
			group.Genre = meta.Genre
		}
		if len(group.Cover) == 0 && len(meta.Cover) > 0 {
			group.Cover = meta.Cover
			group.CoverType = meta.CoverType
		}
	}

	// Sort tracks by disc then track number
	sort.Slice(group.Tracks, func(i, j int) bool {
		if group.Tracks[i].Disc != group.Tracks[j].Disc {
			return group.Tracks[i].Disc < group.Tracks[j].Disc
		}
		if group.Tracks[i].Track != group.Tracks[j].Track {
			return group.Tracks[i].Track < group.Tracks[j].Track
		}
		// Fall back to filename sort
		return group.Tracks[i].Filename < group.Tracks[j].Filename
	})

	// Determine primary format
	maxCount := 0
	for format, count := range formatCounts {
		if count > maxCount {
			maxCount = count
			group.Format = format
		}
	}

	return group
}

// parseTitleFromFolderName extracts title from "Author - Title" format
func parseTitleFromFolderName(name string) string {
	name = strings.TrimSpace(name)

	// Try different separators
	for _, sep := range []string{" - ", " – ", "_-_"} {
		if idx := strings.Index(name, sep); idx > 0 {
			// Return the part after separator as title
			title := strings.TrimSpace(name[idx+len(sep):])
			// Clean up year patterns like "(2020)" or "[2020]"
			title = strings.TrimSpace(removeYearSuffix(title))
			if title != "" {
				return title
			}
		}
	}

	// No separator found, return cleaned folder name
	return strings.TrimSpace(removeYearSuffix(name))
}

// removeYearSuffix removes year patterns like "(2020)" or "[2020]" from end of string
func removeYearSuffix(s string) string {
	// Simple pattern matching for [YYYY] or (YYYY) at end
	s = strings.TrimSpace(s)
	if len(s) < 6 {
		return s
	}

	// Check for [YYYY] pattern
	if s[len(s)-1] == ']' {
		for i := len(s) - 2; i >= 0; i-- {
			if s[i] == '[' {
				// Check if content is a year (4 digits)
				content := s[i+1 : len(s)-1]
				if len(content) == 4 && isDigits(content) {
					return strings.TrimSpace(s[:i])
				}
				break
			}
		}
	}

	// Check for (YYYY) pattern
	if s[len(s)-1] == ')' {
		for i := len(s) - 2; i >= 0; i-- {
			if s[i] == '(' {
				content := s[i+1 : len(s)-1]
				if len(content) == 4 && isDigits(content) {
					return strings.TrimSpace(s[:i])
				}
				break
			}
		}
	}

	return s
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// IsSingleFileAudiobook returns true if the file is a single-file audiobook format
// M4B files are typically complete audiobooks with chapters
func IsSingleFileAudiobook(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".m4b"
}

// FormatDuration formats a duration as "Xh Ym" or "Xm"
func FormatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return strings.TrimSpace(strings.Replace(
			strings.Replace("%dh %dm", "%d", string(rune('0'+hours%10)), 1),
			"%d", string(rune('0'+minutes%10)), 1))
	}
	// Simple format
	if hours > 0 {
		if minutes > 0 {
			return formatInt(hours) + "h " + formatInt(minutes) + "m"
		}
		return formatInt(hours) + "h"
	}
	if minutes > 0 {
		return formatInt(minutes) + "m"
	}
	return "0m"
}

func formatInt(n int) string {
	if n < 10 {
		return string(rune('0'+n))
	}
	return intToString(n)
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
