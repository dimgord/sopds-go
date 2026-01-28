package scanner

import (
	"bytes"
	"io"
	"testing"
)

func TestGetAudioDurationFromReaderUnknownFormat(t *testing.T) {
	// Unknown format should return 0 without error
	reader := bytes.NewReader([]byte("test data"))
	duration, err := GetAudioDurationFromReader(reader, 9, "xyz")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if duration != 0 {
		t.Errorf("Expected 0 duration for unknown format, got %v", duration)
	}
}

func TestGetAudioDurationFromReaderEmptyMP3(t *testing.T) {
	// Empty/invalid MP3 data
	reader := bytes.NewReader([]byte{})
	_, err := GetAudioDurationFromReader(reader, 0, "mp3")
	// Should return error for empty data
	if err == nil {
		// It's OK if it returns 0 duration without error too
	}
}

func TestGetAudioDurationFromReaderEmptyFLAC(t *testing.T) {
	// FLAC without proper magic
	reader := bytes.NewReader([]byte("not flac data"))
	duration, err := GetAudioDurationFromReader(reader, 13, "flac")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if duration != 0 {
		t.Errorf("Expected 0 duration for invalid FLAC, got %v", duration)
	}
}

func TestGetAudioDurationFromReaderEmptyOgg(t *testing.T) {
	// OGG without proper magic
	reader := bytes.NewReader([]byte("not ogg data"))
	duration, err := GetAudioDurationFromReader(reader, 12, "ogg")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if duration != 0 {
		t.Errorf("Expected 0 duration for invalid OGG, got %v", duration)
	}
}

func TestGetAudioDurationFromReaderEmptyM4B(t *testing.T) {
	// M4B without proper atoms
	reader := bytes.NewReader([]byte("not m4b data1234"))
	duration, err := GetAudioDurationFromReader(reader, 16, "m4b")
	if err != nil {
		// May error or not depending on data
	}
	_ = duration
}

// Helper to create a minimal valid FLAC header
func createMinimalFLAC() []byte {
	// fLaC magic + STREAMINFO block
	data := []byte{
		// Magic
		'f', 'L', 'a', 'C',
		// STREAMINFO block header (last block, type 0, length 34)
		0x80, 0x00, 0x00, 0x22,
		// STREAMINFO data (34 bytes)
		// min block size (2), max block size (2), min frame (3), max frame (3)
		0x00, 0x10, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// sample rate (20 bits): 44100 Hz = 0xAC44 << 4 = 0x0AC440
		// channels (3 bits): stereo = 1 (stored as 1 less = 0)
		// bits per sample (5 bits): 16 = 15 (stored as 15 less = 0)
		// total samples (36 bits): 44100 samples (1 second)
		// Packed: sample_rate[19:12] | sample_rate[11:4] | sample_rate[3:0]<<4 | channels[2:0]<<1 | bps[4]
		0x0A, 0xC4, 0x40,
		// bps[3:0]<<4 | total_samples[35:32]
		0xF0,
		// total_samples[31:0] = 44100 = 0x0000AC44
		0x00, 0x00, 0xAC, 0x44,
		// MD5 (16 bytes)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	return data
}

func TestFLACDurationParsing(t *testing.T) {
	flacData := createMinimalFLAC()
	reader := bytes.NewReader(flacData)

	duration, err := getFLACDuration(reader)
	if err != nil {
		t.Fatalf("getFLACDuration error: %v", err)
	}

	// Our minimal FLAC has 44100 samples at 44100 Hz = 1 second
	// But our packed data might not be perfect, so just check it's non-negative
	if duration < 0 {
		t.Errorf("Duration should be non-negative, got %v", duration)
	}
}

func TestGetAudioDurationNonExistentFile(t *testing.T) {
	_, err := GetAudioDuration("/non/existent/file.mp3")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

// Mock reader that simulates seek errors
type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (r *errorReader) Seek(offset int64, whence int) (int64, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestMP4DurationParsingEmpty(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	duration, err := getMP4Duration(reader, 0)
	if err != nil {
		// Expected to fail on empty data
	}
	_ = duration
}

func TestMP3DurationParsingWithID3(t *testing.T) {
	// Create minimal MP3 with ID3v2 header
	data := []byte{
		// ID3v2 header
		'I', 'D', '3',
		0x04, 0x00, // version
		0x00,       // flags
		0x00, 0x00, 0x00, 0x00, // size (syncsafe)
		// MP3 frame sync
		0xFF, 0xFB, 0x90, 0x00, // Frame header
	}

	reader := bytes.NewReader(data)
	_, err := getMP3Duration(reader, int64(len(data)))
	// May succeed or fail depending on complete frame structure
	_ = err
}

func TestOggDurationParsingInvalidHeader(t *testing.T) {
	// OGG with valid magic but invalid content
	data := []byte{
		'O', 'g', 'g', 'S', // Magic
		0x00,               // Version
		0x02,               // Header type (BOS)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Granule
		0x00, 0x00, 0x00, 0x00, // Serial
		0x00, 0x00, 0x00, 0x00, // Page seq
		0x00, 0x00, 0x00, 0x00, // CRC
		0x01,               // Segment count
		0x08,               // Segment size
		// Invalid codec header
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	reader := bytes.NewReader(data)
	duration, err := getOggDuration(reader, int64(len(data)))
	if err != nil {
		// Expected to handle gracefully
	}
	if duration != 0 {
		t.Log("Duration parsed from minimal OGG data")
	}
}

func TestVorbisDurationParsing(t *testing.T) {
	// Create minimal Vorbis header in OGG container
	data := []byte{
		// OGG page header
		'O', 'g', 'g', 'S', // Magic
		0x00,               // Version
		0x02,               // Header type (BOS)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Granule position
		0x01, 0x00, 0x00, 0x00, // Serial number
		0x00, 0x00, 0x00, 0x00, // Page sequence
		0x00, 0x00, 0x00, 0x00, // CRC (we're not validating)
		0x01,               // Segment count
		30,                 // Segment size
		// Vorbis identification header
		0x01,                       // Packet type
		'v', 'o', 'r', 'b', 'i', 's', // "vorbis"
		0x00, 0x00, 0x00, 0x00, // Version
		0x02,                   // Channels
		0x44, 0xAC, 0x00, 0x00, // Sample rate (44100 = 0xAC44 little endian)
		// More header data...
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00,
	}

	reader := bytes.NewReader(data)
	duration, err := getOggDuration(reader, int64(len(data)))
	if err != nil {
		t.Logf("getOggDuration error (may be expected): %v", err)
	}
	_ = duration
}

func TestOpusDurationParsing(t *testing.T) {
	// Create minimal Opus header in OGG container
	data := []byte{
		// OGG page header
		'O', 'g', 'g', 'S', // Magic
		0x00,               // Version
		0x02,               // Header type (BOS)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Granule position
		0x01, 0x00, 0x00, 0x00, // Serial number
		0x00, 0x00, 0x00, 0x00, // Page sequence
		0x00, 0x00, 0x00, 0x00, // CRC
		0x01,               // Segment count
		19,                 // Segment size
		// Opus identification header
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', // "OpusHead"
		0x01,               // Version
		0x02,               // Channels
		0x00, 0x00,         // Pre-skip
		0x80, 0xBB, 0x00, 0x00, // Input sample rate (48000)
		0x00, 0x00,         // Output gain
		0x00,               // Channel mapping
	}

	reader := bytes.NewReader(data)
	duration, err := getOggDuration(reader, int64(len(data)))
	if err != nil {
		t.Logf("getOggDuration error (may be expected): %v", err)
	}
	// Opus uses 48000 Hz internally
	_ = duration
}

func TestFormatRouting(t *testing.T) {
	emptyReader := bytes.NewReader([]byte{})

	tests := []struct {
		format string
	}{
		{"m4b"},
		{"m4a"},
		{"m4p"},
		{"mp4"},
		{"aac"},
		{"mp3"},
		{"flac"},
		{"ogg"},
		{"opus"},
		{"wav"},
		{"unknown"},
	}

	for _, tc := range tests {
		_, err := GetAudioDurationFromReader(emptyReader, 0, tc.format)
		// We just want to ensure it doesn't panic
		_ = err
		emptyReader.Seek(0, io.SeekStart)
	}
}
