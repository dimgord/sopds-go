package scanner

import (
	"testing"
)

func TestNewAudioParser(t *testing.T) {
	parser := NewAudioParser(true)
	if parser == nil {
		t.Error("Expected non-nil parser")
	}
	if !parser.readCover {
		t.Error("Expected readCover to be true")
	}

	parser2 := NewAudioParser(false)
	if parser2.readCover {
		t.Error("Expected readCover to be false")
	}
}

func TestIsAudioFormat(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{".mp3", true},
		{"mp3", true},
		{".MP3", true},
		{".m4b", true},
		{".m4a", true},
		{".flac", true},
		{".ogg", true},
		{".opus", true},
		{".wav", true},
		{".aac", true},
		{".fb2", false},
		{".epub", false},
		{".pdf", false},
		{".txt", false},
		{"", false},
	}

	for _, tc := range tests {
		result := IsAudioFormat(tc.ext)
		if result != tc.expected {
			t.Errorf("IsAudioFormat(%q) = %v, expected %v", tc.ext, result, tc.expected)
		}
	}
}

func TestAudioFormatMIME(t *testing.T) {
	tests := []struct {
		format   string
		expected string
	}{
		{"mp3", "audio/mpeg"},
		{"MP3", "audio/mpeg"},
		{"m4b", "audio/mp4"},
		{"m4a", "audio/mp4"},
		{"aac", "audio/mp4"},
		{"flac", "audio/flac"},
		{"ogg", "audio/ogg"},
		{"opus", "audio/ogg"},
		{"wav", "audio/wav"},
		{"unknown", "audio/mpeg"}, // Default
		{"", "audio/mpeg"},
	}

	for _, tc := range tests {
		result := AudioFormatMIME(tc.format)
		if result != tc.expected {
			t.Errorf("AudioFormatMIME(%q) = %q, expected %q", tc.format, result, tc.expected)
		}
	}
}

func TestAudioMetadataGetTitle(t *testing.T) {
	tests := []struct {
		meta     AudioMetadata
		expected string
	}{
		{AudioMetadata{Title: "Track Title", Album: "Album Name"}, "Track Title"},
		{AudioMetadata{Title: "", Album: "Album Name"}, "Album Name"},
		{AudioMetadata{Title: "", Album: ""}, ""},
	}

	for _, tc := range tests {
		result := tc.meta.GetTitle()
		if result != tc.expected {
			t.Errorf("GetTitle() = %q, expected %q", result, tc.expected)
		}
	}
}

func TestAudioMetadataGetAlbumTitle(t *testing.T) {
	tests := []struct {
		meta     AudioMetadata
		expected string
	}{
		{AudioMetadata{Title: "Track Title", Album: "Album Name"}, "Album Name"},
		{AudioMetadata{Title: "Track Title", Album: ""}, "Track Title"},
		{AudioMetadata{Title: "", Album: ""}, ""},
	}

	for _, tc := range tests {
		result := tc.meta.GetAlbumTitle()
		if result != tc.expected {
			t.Errorf("GetAlbumTitle() = %q, expected %q", result, tc.expected)
		}
	}
}

func TestAudioMetadataEstimateDuration(t *testing.T) {
	tests := []struct {
		meta            AudioMetadata
		expectedSeconds float64
	}{
		{AudioMetadata{Bitrate: 128, Filesize: 1280000}, 80},   // 1.28MB at 128kbps = 80s
		{AudioMetadata{Bitrate: 320, Filesize: 3200000}, 80},   // 3.2MB at 320kbps = 80s
		{AudioMetadata{Bitrate: 0, Filesize: 1000000}, 0},      // Zero bitrate
		{AudioMetadata{Bitrate: 128, Filesize: 0}, 0},          // Zero filesize
		{AudioMetadata{Bitrate: 64, Filesize: 640000}, 80},     // 640KB at 64kbps = 80s
	}

	for _, tc := range tests {
		result := tc.meta.EstimateDuration()
		actualSeconds := result.Seconds()
		// Allow small floating point differences
		diff := actualSeconds - tc.expectedSeconds
		if diff < -0.1 || diff > 0.1 {
			t.Errorf("EstimateDuration() for bitrate=%d, filesize=%d = %v seconds, expected ~%v",
				tc.meta.Bitrate, tc.meta.Filesize, actualSeconds, tc.expectedSeconds)
		}
	}
}

func TestAudioMetadataGetAuthors(t *testing.T) {
	// Test priority: Composer > AlbumArtist > Artist
	meta := AudioMetadata{
		Composer:    "John Doe",
		AlbumArtist: "Jane Smith",
		Artist:      "Bob Wilson",
	}

	authors := meta.GetAuthors()
	if len(authors) != 3 {
		t.Fatalf("Expected 3 authors, got %d", len(authors))
	}

	// First should be Composer
	if authors[0].LastName != "Doe" {
		t.Errorf("First author should be Composer 'Doe', got '%s'", authors[0].LastName)
	}

	// Test with duplicates (should be deduplicated)
	meta2 := AudioMetadata{
		Composer:    "John Doe",
		AlbumArtist: "John Doe",
		Artist:      "John Doe",
	}

	authors2 := meta2.GetAuthors()
	if len(authors2) != 1 {
		t.Errorf("Expected 1 author (deduplicated), got %d", len(authors2))
	}
}

func TestAudioMetadataGetNarrator(t *testing.T) {
	// When Composer (author) and Artist (narrator) differ
	meta := AudioMetadata{
		Composer: "Author Name",
		Artist:   "Narrator Name",
	}

	narrator := meta.GetNarrator()
	if narrator == nil {
		t.Fatal("Expected narrator, got nil")
	}
	if narrator.LastName != "Name" || narrator.FirstName != "Narrator" {
		t.Errorf("Narrator mismatch: got %+v", narrator)
	}

	// When AlbumArtist (author) and Artist (narrator) differ
	meta2 := AudioMetadata{
		AlbumArtist: "Author Name",
		Artist:      "Different Narrator",
	}

	narrator2 := meta2.GetNarrator()
	if narrator2 == nil {
		t.Fatal("Expected narrator, got nil")
	}

	// When same - no separate narrator
	meta3 := AudioMetadata{
		Composer: "Same Person",
		Artist:   "Same Person",
	}

	narrator3 := meta3.GetNarrator()
	if narrator3 != nil {
		t.Error("Expected nil narrator when Composer equals Artist")
	}
}

func TestParseAuthorName(t *testing.T) {
	tests := []struct {
		input         string
		expectedFirst string
		expectedLast  string
	}{
		// Last, First format
		{"Doe, John", "John", "Doe"},
		{"Smith, Jane Mary", "Jane Mary", "Smith"},

		// First Last format
		{"John Doe", "John", "Doe"},
		{"Jane Mary Smith", "Jane Mary", "Smith"},

		// Single name
		{"Madonna", "", "Madonna"},
		{"Prince", "", "Prince"},

		// Empty/whitespace
		{"", "", ""},
		{"   ", "", ""},
	}

	for _, tc := range tests {
		result := parseAuthorName(tc.input)
		if result.FirstName != tc.expectedFirst || result.LastName != tc.expectedLast {
			t.Errorf("parseAuthorName(%q) = {First: %q, Last: %q}, expected {First: %q, Last: %q}",
				tc.input, result.FirstName, result.LastName, tc.expectedFirst, tc.expectedLast)
		}
	}
}

func TestFixCyrillicEncoding(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Already valid UTF-8
		{"Hello World", "Hello World"},
		{"", ""},

		// String with null bytes (UTF-16 as UTF-8 issue)
		{"A\x00B\x00C\x00", "ABC"},
	}

	for _, tc := range tests {
		result := fixCyrillicEncoding(tc.input)
		if result != tc.expected {
			t.Errorf("fixCyrillicEncoding(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestLooksLikeMojibake(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Normal ASCII
		{"Hello World", false},
		{"", false},

		// Normal Cyrillic (valid UTF-8, not mojibake)
		{"Привет мир", false},

		// High-Latin chars - in Go, \xc0 etc. form multi-byte UTF-8 sequences,
		// so they don't count as individual high-Latin chars
		{"\xc0\xc1\xc2\xc3", false}, // Actually forms UTF-8 sequences
	}

	for _, tc := range tests {
		result := looksLikeMojibake(tc.input)
		if result != tc.expected {
			t.Errorf("looksLikeMojibake(%q) = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}

func TestHasCyrillic(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Привет", true},
		{"Мир", true},
		{"Hello", false},
		{"Hello Мир", true},
		{"", false},
		{"123", false},
	}

	for _, tc := range tests {
		result := hasCyrillic(tc.input)
		if result != tc.expected {
			t.Errorf("hasCyrillic(%q) = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}

func TestLooksLikeUTF16AsUTF8(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Normal text
		{"Hello", false},
		{"", false},
		{"ab", false}, // Too short

		// UTF-16 pattern: char, space, char, space
		{"A B C D E F", true},
		{"H e l l o", true},
	}

	for _, tc := range tests {
		result := looksLikeUTF16AsUTF8(tc.input)
		if result != tc.expected {
			t.Errorf("looksLikeUTF16AsUTF8(%q) = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}

func TestFixUTF16AsUTF8(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"H e l l o", "Hello"},
		{"A B C", "ABC"},
		// "normal" keeps non-space chars at odd positions, so remains unchanged
		{"normal", "normal"},
	}

	for _, tc := range tests {
		result := fixUTF16AsUTF8(tc.input)
		if result != tc.expected {
			t.Errorf("fixUTF16AsUTF8(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestAudioParserEstimateBitrate(t *testing.T) {
	parser := NewAudioParser(false)

	tests := []struct {
		format   string
		expected int
	}{
		{"mp3", 128},
		{"m4b", 64},
		{"m4a", 64},
		{"aac", 64},
		{"flac", 800},
		{"ogg", 96},
		{"opus", 96},
		{"unknown", 128},
	}

	for _, tc := range tests {
		result := parser.estimateBitrate(tc.format, 0)
		if result != tc.expected {
			t.Errorf("estimateBitrate(%q) = %d, expected %d", tc.format, result, tc.expected)
		}
	}
}

func TestAudioMetadataStruct(t *testing.T) {
	// Test struct initialization
	meta := AudioMetadata{
		Title:       "Test Title",
		Artist:      "Test Artist",
		Album:       "Test Album",
		AlbumArtist: "Test Album Artist",
		Composer:    "Test Composer",
		Genre:       "Test Genre",
		Year:        2024,
		Track:       5,
		TotalTracks: 10,
		Disc:        1,
		TotalDiscs:  2,
		Bitrate:     128,
		Filesize:    1000000,
		Format:      "mp3",
	}

	if meta.Title != "Test Title" {
		t.Error("Title mismatch")
	}
	if meta.Year != 2024 {
		t.Error("Year mismatch")
	}
	if meta.Track != 5 {
		t.Error("Track mismatch")
	}
}
