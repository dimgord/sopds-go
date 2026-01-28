package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseINXRealFile(t *testing.T) {
	// Skip if test file doesn't exist
	inxPath := "/home/dimgord/sopds-go/AKYNIN.inx"
	if _, err := os.Stat(inxPath); os.IsNotExist(err) {
		t.Skip("Test INX file not found, skipping real file test")
	}

	ab, err := ParseINX(inxPath)
	if err != nil {
		t.Fatalf("ParseINX error: %v", err)
	}

	t.Logf("Book: %s", ab.Name)
	t.Logf("Tracks: %d", len(ab.Tracks))
	t.Logf("Total Duration: %d seconds (%.1f hours)", ab.TotalDuration(), float64(ab.TotalDuration())/3600)
	t.Logf("Version: %s", ab.Version)
	t.Logf("Codec: %+v", ab.Codec)

	if ab.Name != "AKYNIN" {
		t.Errorf("Expected book name AKYNIN, got %s", ab.Name)
	}

	if len(ab.Tracks) != 46 {
		t.Errorf("Expected 46 tracks, got %d", len(ab.Tracks))
	}

	if len(ab.Tracks) > 0 {
		t.Logf("\nFirst 3 tracks:")
		for i, tr := range ab.Tracks[:minInt(3, len(ab.Tracks))] {
			t.Logf("  %d. %s (%d sec)", i+1, tr.Filename, tr.Duration)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestNokiaAudiobookStruct(t *testing.T) {
	na := &NokiaAudiobook{
		Name:    "Test Book",
		Version: "1.0",
		Tracks: []NokiaTrack{
			{Filename: "track01.awb", Duration: 300},
			{Filename: "track02.awb", Duration: 400},
		},
		Codec: NokiaCodecInfo{
			ManagerVersion: "2.0",
			CodecMode:      1,
			BitRate:        12650,
		},
	}

	if na.Name != "Test Book" {
		t.Errorf("Name mismatch: got %s", na.Name)
	}

	if len(na.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(na.Tracks))
	}

	totalDur := na.TotalDuration()
	if totalDur != 700 {
		t.Errorf("Expected total duration 700, got %d", totalDur)
	}
}

func TestNokiaTrackStruct(t *testing.T) {
	track := NokiaTrack{
		Filename: "chapter01.awb",
		Duration: 1800, // 30 minutes in seconds
	}

	if track.Filename != "chapter01.awb" {
		t.Errorf("Filename mismatch: got %s", track.Filename)
	}

	if track.Duration != 1800 {
		t.Errorf("Duration mismatch: got %d", track.Duration)
	}
}

func TestNokiaChapterStruct(t *testing.T) {
	chapter := NokiaChapter{
		Filename: "track01.awb",
		StartSec: 0,
		Number:   1,
		Title:    "Chapter 1: Introduction",
	}

	if chapter.Title != "Chapter 1: Introduction" {
		t.Errorf("Title mismatch: got %s", chapter.Title)
	}

	if chapter.StartSec != 0 {
		t.Errorf("StartSec mismatch: got %d", chapter.StartSec)
	}

	if chapter.Number != 1 {
		t.Errorf("Number mismatch: got %d", chapter.Number)
	}

	if chapter.Filename != "track01.awb" {
		t.Errorf("Filename mismatch: got %s", chapter.Filename)
	}
}

func TestNokiaCodecInfoStruct(t *testing.T) {
	codec := NokiaCodecInfo{
		ManagerVersion: "2.0",
		CodecMode:      1,
		BitRate:        12650,
	}

	if codec.ManagerVersion != "2.0" {
		t.Errorf("ManagerVersion mismatch: got %s", codec.ManagerVersion)
	}

	if codec.CodecMode != 1 {
		t.Errorf("CodecMode mismatch: got %d", codec.CodecMode)
	}

	if codec.BitRate != 12650 {
		t.Errorf("BitRate mismatch: got %d", codec.BitRate)
	}
}

func TestTotalDuration(t *testing.T) {
	na := &NokiaAudiobook{
		Tracks: []NokiaTrack{
			{Duration: 100},
			{Duration: 200},
			{Duration: 300},
		},
	}

	total := na.TotalDuration()
	if total != 600 {
		t.Errorf("Expected total duration 600, got %d", total)
	}

	// Empty tracks
	na2 := &NokiaAudiobook{Tracks: []NokiaTrack{}}
	if na2.TotalDuration() != 0 {
		t.Error("Expected 0 for empty tracks")
	}
}

func TestFindINXFile(t *testing.T) {
	// Create temp directory with test INX file
	tmpDir, err := os.MkdirTemp("", "inx_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test when no INX file exists
	path, err := FindINXFile(tmpDir)
	if err == nil && path != "" {
		t.Error("Expected no INX file found in empty dir")
	}

	// Create a test INX file
	inxPath := filepath.Join(tmpDir, "test.inx")
	if err := os.WriteFile(inxPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test INX: %v", err)
	}

	// Now it should find it
	foundPath, err := FindINXFile(tmpDir)
	if err != nil {
		t.Errorf("FindINXFile error: %v", err)
	}
	if foundPath != inxPath {
		t.Errorf("Expected %s, got %s", inxPath, foundPath)
	}
}

func TestIsNokiaAudiobookDir(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "nokia_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Empty dir - not Nokia audiobook
	if IsNokiaAudiobookDir(tmpDir) {
		t.Error("Empty dir should not be Nokia audiobook")
	}

	// Create AWB file
	awbPath := filepath.Join(tmpDir, "track01.awb")
	if err := os.WriteFile(awbPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create AWB file: %v", err)
	}

	// AWB without INX - not complete Nokia audiobook dir
	if IsNokiaAudiobookDir(tmpDir) {
		t.Error("Dir with AWB but no INX should not be Nokia audiobook")
	}

	// Create INX file
	inxPath := filepath.Join(tmpDir, "book.inx")
	if err := os.WriteFile(inxPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create INX file: %v", err)
	}

	// Now should be Nokia audiobook dir
	if !IsNokiaAudiobookDir(tmpDir) {
		t.Error("Dir with AWB and INX should be Nokia audiobook")
	}
}

func TestParseINXNonExistent(t *testing.T) {
	_, err := ParseINX("/non/existent/path.inx")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}
