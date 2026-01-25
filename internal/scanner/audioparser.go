package scanner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/sopds/sopds-go/internal/database"
)

// AudioMetadata holds extracted audio file metadata
type AudioMetadata struct {
	Title       string
	Artist      string            // Primary artist/author
	Album       string            // Album name (audiobook title for multi-file)
	AlbumArtist string            // Album artist (narrator or primary author)
	Composer    string            // Often used for author in audiobooks
	Genre       string
	Year        int
	Track       int               // Track number
	TotalTracks int               // Total tracks in album
	Disc        int               // Disc number
	TotalDiscs  int               // Total discs
	Duration    time.Duration     // Audio duration
	Bitrate     int               // Bitrate in kbps (estimated)
	Filesize    int64             // File size in bytes
	Format      string            // Audio format (mp3, m4a, flac, etc.)
	Cover       []byte            // Embedded cover art
	CoverType   string            // Cover MIME type
	Chapters    []Chapter         // Chapter markers (M4B)
}

// Chapter represents a chapter marker in audiobook
type Chapter struct {
	Title   string `json:"title"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// AudioParser parses audio file metadata
type AudioParser struct {
	readCover bool
}

// NewAudioParser creates a new audio parser
func NewAudioParser(readCover bool) *AudioParser {
	return &AudioParser{readCover: readCover}
}

// Parse extracts metadata from an io.ReadSeeker
func (p *AudioParser) Parse(r io.ReadSeeker, filesize int64, format string) (*AudioMetadata, error) {
	m, err := tag.ReadFrom(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio tags: %w", err)
	}

	meta := &AudioMetadata{
		Title:       strings.TrimSpace(m.Title()),
		Artist:      strings.TrimSpace(m.Artist()),
		Album:       strings.TrimSpace(m.Album()),
		AlbumArtist: strings.TrimSpace(m.AlbumArtist()),
		Composer:    strings.TrimSpace(m.Composer()),
		Genre:       strings.TrimSpace(m.Genre()),
		Year:        m.Year(),
		Filesize:    filesize,
		Format:      format,
	}

	// Track info
	track, totalTracks := m.Track()
	meta.Track = track
	meta.TotalTracks = totalTracks

	disc, totalDiscs := m.Disc()
	meta.Disc = disc
	meta.TotalDiscs = totalDiscs

	// Cover art
	if p.readCover {
		if pic := m.Picture(); pic != nil {
			meta.Cover = pic.Data
			meta.CoverType = pic.MIMEType
		}
	}

	// Get actual duration from audio data
	r.Seek(0, io.SeekStart)
	if duration, err := GetAudioDurationFromReader(r, filesize, format); err == nil && duration > 0 {
		meta.Duration = duration
		// Calculate actual bitrate from duration
		if duration.Seconds() > 0 {
			meta.Bitrate = int(float64(filesize*8) / duration.Seconds() / 1000)
		}
	} else {
		// Fallback: estimate bitrate from file format
		meta.Bitrate = p.estimateBitrate(format, filesize)
	}

	return meta, nil
}

// ParseFile extracts metadata from a file path
func (p *AudioParser) ParseFile(path string) (*AudioMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat audio file: %w", err)
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return p.Parse(f, stat.Size(), ext)
}

// GetAuthors extracts author information from metadata
// Audiobooks often store author in Composer or AlbumArtist fields
func (m *AudioMetadata) GetAuthors() []database.Author {
	var authors []database.Author
	seen := make(map[string]bool)

	// Priority: Composer > AlbumArtist > Artist
	candidates := []string{m.Composer, m.AlbumArtist, m.Artist}

	for _, name := range candidates {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true

		author := parseAuthorName(name)
		if author.FirstName != "" || author.LastName != "" {
			authors = append(authors, author)
		}
	}

	return authors
}

// GetNarrator extracts narrator from metadata
// Often stored in Artist field when AlbumArtist/Composer has the author
func (m *AudioMetadata) GetNarrator() *database.Author {
	// If we have both Composer (author) and Artist (narrator), use Artist as narrator
	if m.Composer != "" && m.Artist != "" && m.Composer != m.Artist {
		narrator := parseAuthorName(m.Artist)
		if narrator.FirstName != "" || narrator.LastName != "" {
			return &narrator
		}
	}
	// If AlbumArtist (author) differs from Artist, use Artist as narrator
	if m.AlbumArtist != "" && m.Artist != "" && m.AlbumArtist != m.Artist {
		narrator := parseAuthorName(m.Artist)
		if narrator.FirstName != "" || narrator.LastName != "" {
			return &narrator
		}
	}
	return nil
}

// GetTitle returns the title, falling back to album for multi-file audiobooks
func (m *AudioMetadata) GetTitle() string {
	if m.Title != "" {
		return m.Title
	}
	if m.Album != "" {
		return m.Album
	}
	return ""
}

// GetAlbumTitle returns the album/audiobook title
func (m *AudioMetadata) GetAlbumTitle() string {
	if m.Album != "" {
		return m.Album
	}
	return m.Title
}

// parseAuthorName splits a full name into first/last name
func parseAuthorName(fullName string) database.Author {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return database.Author{}
	}

	// Handle "Last, First" format
	if strings.Contains(fullName, ",") {
		parts := strings.SplitN(fullName, ",", 2)
		return database.Author{
			LastName:  strings.TrimSpace(parts[0]),
			FirstName: strings.TrimSpace(parts[1]),
		}
	}

	// Handle "First Last" format
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

// estimateBitrate estimates bitrate based on file format and size
func (p *AudioParser) estimateBitrate(format string, filesize int64) int {
	// Common audiobook bitrates
	switch format {
	case "mp3":
		return 128 // Typical for audiobooks
	case "m4b", "m4a", "aac":
		return 64 // AAC is more efficient
	case "flac":
		return 800 // Lossless
	case "ogg", "opus":
		return 96 // Efficient codecs
	default:
		return 128
	}
}

// EstimateDuration estimates duration from file size and bitrate
func (m *AudioMetadata) EstimateDuration() time.Duration {
	if m.Bitrate <= 0 || m.Filesize <= 0 {
		return 0
	}
	// Duration = FileSize(bytes) * 8 / Bitrate(kbps) / 1000
	seconds := float64(m.Filesize) * 8 / float64(m.Bitrate*1000)
	return time.Duration(seconds * float64(time.Second))
}

// IsAudioFormat returns true if the extension is a supported audio format
func IsAudioFormat(ext string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	switch ext {
	case "mp3", "m4b", "m4a", "flac", "ogg", "opus", "wav", "aac":
		return true
	}
	return false
}

// AudioFormatMIME returns the MIME type for an audio format
func AudioFormatMIME(format string) string {
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "m4b", "m4a", "aac":
		return "audio/mp4"
	case "flac":
		return "audio/flac"
	case "ogg", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	default:
		return "audio/mpeg"
	}
}
