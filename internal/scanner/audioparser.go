package scanner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dhowden/tag"
	"golang.org/x/text/encoding/charmap"

	"github.com/dimgord/sopds-go/internal/database"
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
	// AWB (AMR-WB) files don't have standard tags - return basic metadata
	// Duration will be read from INX file by processAudioGroup
	if format == "awb" {
		return &AudioMetadata{
			Filesize: filesize,
			Format:   format,
			Bitrate:  12, // AMR-WB typically 12.65 kbps
		}, nil
	}

	m, err := tag.ReadFrom(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio tags: %w", err)
	}

	meta := &AudioMetadata{
		Title:       fixCyrillicEncoding(strings.TrimSpace(m.Title())),
		Artist:      fixCyrillicEncoding(strings.TrimSpace(m.Artist())),
		Album:       fixCyrillicEncoding(strings.TrimSpace(m.Album())),
		AlbumArtist: fixCyrillicEncoding(strings.TrimSpace(m.AlbumArtist())),
		Composer:    fixCyrillicEncoding(strings.TrimSpace(m.Composer())),
		Genre:       fixCyrillicEncoding(strings.TrimSpace(m.Genre())),
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

// fixCyrillicEncoding detects and fixes encoding issues in audio metadata
// - Removes null bytes (from UTF-16 misread as UTF-8)
// - Fixes Windows-1251 text incorrectly read as Latin-1
func fixCyrillicEncoding(s string) string {
	if s == "" {
		return s
	}

	// First, remove null bytes (common issue with UTF-16 misread as UTF-8)
	// UTF-16 chars like "А" are stored as 0x10 0x04, which when read as UTF-8 stream
	// can appear as character + null alternating
	if strings.ContainsRune(s, 0) {
		cleaned := strings.Map(func(r rune) rune {
			if r == 0 {
				return -1 // Remove null bytes
			}
			return r
		}, s)
		if cleaned != "" {
			s = cleaned
		}
	}

	// Check for UTF-16 pattern (alternating char + space/null pattern)
	// This manifests as "А б в г" instead of "Абвг"
	if looksLikeUTF16AsUTF8(s) {
		s = fixUTF16AsUTF8(s)
	}

	// If string is valid UTF-8 and doesn't look like mojibake, keep it
	if utf8.ValidString(s) && !looksLikeMojibake(s) {
		return s
	}

	// Try to decode as Windows-1251 (Latin-1 bytes -> Windows-1251)
	// Convert string to bytes (Latin-1 codepoints map directly to byte values)
	bytes := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			bytes = append(bytes, byte(r))
		} else {
			// Non-Latin-1 character, probably already UTF-8
			return s
		}
	}

	// Decode as Windows-1251
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(bytes)
	if err != nil {
		return s
	}

	result := string(decoded)
	// Verify the result looks better (contains Cyrillic)
	if hasCyrillic(result) {
		return result
	}

	return s
}

// looksLikeUTF16AsUTF8 detects UTF-16 data misread as UTF-8
// Pattern: characters separated by spaces (the null high bytes in UTF-16 LE)
func looksLikeUTF16AsUTF8(s string) bool {
	// Check if string has alternating pattern: char, space, char, space
	runes := []rune(s)
	if len(runes) < 4 {
		return false
	}

	// Count how many odd positions are spaces
	spaceCount := 0
	charCount := 0
	for i, r := range runes {
		if i%2 == 1 && r == ' ' {
			spaceCount++
		}
		if i%2 == 0 && r != ' ' {
			charCount++
		}
	}

	// If more than 60% of odd positions are spaces, likely UTF-16 issue
	total := len(runes) / 2
	return total > 2 && float64(spaceCount)/float64(total) > 0.6
}

// fixUTF16AsUTF8 removes the extra spaces from UTF-16 misread as UTF-8
func fixUTF16AsUTF8(s string) string {
	runes := []rune(s)
	result := make([]rune, 0, len(runes)/2)

	for i, r := range runes {
		// Keep only even-position characters (the actual content)
		// Skip odd-position spaces (the null bytes from UTF-16)
		if i%2 == 0 {
			result = append(result, r)
		} else if r != ' ' && r != 0 {
			// If odd position is not space/null, keep it (not UTF-16 pattern)
			result = append(result, r)
		}
	}

	return strings.TrimSpace(string(result))
}

// looksLikeMojibake checks if a string looks like mojibake (Windows-1251 read as Latin-1)
// Common mojibake patterns: high-Latin characters (0x80-0xFF) that form Cyrillic ranges
func looksLikeMojibake(s string) bool {
	if s == "" {
		return false
	}

	// Count high-Latin characters (potential mojibake)
	highLatinCount := 0
	totalChars := 0

	for _, r := range s {
		totalChars++
		// Windows-1251 Cyrillic (А-я) maps to 0xC0-0xFF in Latin-1
		// Also check for common high-Latin characters
		if r >= 0x80 && r <= 0xFF {
			highLatinCount++
		}
	}

	// If more than 30% high-Latin, likely mojibake
	if totalChars > 0 && float64(highLatinCount)/float64(totalChars) > 0.3 {
		return true
	}

	return false
}

// hasCyrillic checks if string contains Cyrillic characters
func hasCyrillic(s string) bool {
	for _, r := range s {
		// Cyrillic range: U+0400 to U+04FF
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}
