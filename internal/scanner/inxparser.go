package scanner

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// NokiaAudiobook represents a parsed Nokia audiobook from INX file
type NokiaAudiobook struct {
	Name     string
	Tracks   []NokiaTrack
	Chapters []NokiaChapter
	Version  string
	Codec    NokiaCodecInfo
}

// NokiaTrack represents a single AWB track
type NokiaTrack struct {
	Filename string
	Duration int // seconds
}

// NokiaChapter represents a chapter marker
type NokiaChapter struct {
	Filename string
	StartSec int
	Number   int
	Title    string
}

// NokiaCodecInfo contains codec information from CONTENT_INFO
type NokiaCodecInfo struct {
	ManagerVersion string
	CodecMode      int
	BitRate        int
}

// ParseINX parses a Nokia audiobook INX index file
func ParseINX(path string) (*NokiaAudiobook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read INX file: %w", err)
	}

	// Decode UTF-16 LE (with or without BOM)
	text, err := decodeUTF16(data)
	if err != nil {
		return nil, fmt.Errorf("decode UTF-16: %w", err)
	}

	audiobook := &NokiaAudiobook{}
	currentSection := ""

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Check for section headers
		if strings.HasPrefix(line, "#") {
			currentSection = strings.TrimPrefix(line, "#")
			continue
		}

		// Parse based on current section
		switch currentSection {
		case "BOOK":
			audiobook.Name = strings.TrimSuffix(line, ";")

		case "TRACKS":
			track, ok := parseTrackLine(line)
			if ok {
				audiobook.Tracks = append(audiobook.Tracks, track)
			}

		case "CHAPTERS":
			chapter, ok := parseChapterLine(line)
			if ok {
				audiobook.Chapters = append(audiobook.Chapters, chapter)
			}

		case "VERSION":
			audiobook.Version = strings.TrimSuffix(line, ";")

		case "CONTENT_INFO":
			parseContentInfo(line, audiobook)
		}
	}

	return audiobook, scanner.Err()
}

// decodeUTF16 decodes UTF-16 LE data to string
func decodeUTF16(data []byte) (string, error) {
	// Check for BOM and determine encoding
	var enc unicode.Endianness = unicode.LittleEndian
	var bom unicode.BOMPolicy = unicode.IgnoreBOM

	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			// UTF-16 LE with BOM
			enc = unicode.LittleEndian
			bom = unicode.ExpectBOM
		} else if data[0] == 0xFE && data[1] == 0xFF {
			// UTF-16 BE with BOM
			enc = unicode.BigEndian
			bom = unicode.ExpectBOM
		}
	}

	decoder := unicode.UTF16(enc, bom).NewDecoder()
	reader := transform.NewReader(bytes.NewReader(data), decoder)
	buf := new(bytes.Buffer)
	buf.ReadFrom(reader)
	return buf.String(), nil
}

// parseTrackLine parses a track line like "1-01.awb:557;"
func parseTrackLine(line string) (NokiaTrack, bool) {
	line = strings.TrimSuffix(line, ";")
	parts := strings.Split(line, ":")
	if len(parts) != 2 {
		return NokiaTrack{}, false
	}

	duration, err := strconv.Atoi(parts[1])
	if err != nil {
		return NokiaTrack{}, false
	}

	return NokiaTrack{
		Filename: parts[0],
		Duration: duration,
	}, true
}

// parseChapterLine parses a chapter line like "1-01.awb:0s:1:1-01;"
func parseChapterLine(line string) (NokiaChapter, bool) {
	line = strings.TrimSuffix(line, ";")
	parts := strings.Split(line, ":")
	if len(parts) < 4 {
		return NokiaChapter{}, false
	}

	// Parse start time (e.g., "0s" or "120s")
	startStr := strings.TrimSuffix(parts[1], "s")
	startSec, _ := strconv.Atoi(startStr)

	// Parse chapter number
	chapterNum, _ := strconv.Atoi(parts[2])

	return NokiaChapter{
		Filename: parts[0],
		StartSec: startSec,
		Number:   chapterNum,
		Title:    parts[3],
	}, true
}

// parseContentInfo parses CONTENT_INFO lines
func parseContentInfo(line string, ab *NokiaAudiobook) {
	line = strings.TrimSuffix(line, ";")
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}

	key, value := parts[0], parts[1]
	switch key {
	case "NokiaAudiobookManagerVersion":
		ab.Codec.ManagerVersion = value
	case "CodecMode":
		ab.Codec.CodecMode, _ = strconv.Atoi(value)
	case "CodecBitRate":
		ab.Codec.BitRate, _ = strconv.Atoi(value)
	}
}

// TotalDuration returns total duration in seconds
func (ab *NokiaAudiobook) TotalDuration() int {
	total := 0
	for _, t := range ab.Tracks {
		total += t.Duration
	}
	return total
}

// FindINXFile looks for an INX file in the given directory
func FindINXFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".inx" {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", nil
}

// IsNokiaAudiobookDir checks if a directory contains Nokia audiobook files
func IsNokiaAudiobookDir(dir string) bool {
	inxPath, _ := FindINXFile(dir)
	if inxPath == "" {
		return false
	}

	// Check if there are AWB files too
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".awb" {
			return true
		}
	}

	return false
}
