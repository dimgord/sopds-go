package book

import (
	"errors"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	b, err := New("test.fb2", "/books/test.fb2", FormatFB2, 1024)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	if b.Filename() != "test.fb2" {
		t.Errorf("Filename() = %q, expected test.fb2", b.Filename())
	}
	if b.Path() != "/books/test.fb2" {
		t.Errorf("Path() = %q, expected /books/test.fb2", b.Path())
	}
	if b.Format() != FormatFB2 {
		t.Errorf("Format() = %q, expected %q", b.Format(), FormatFB2)
	}
	if b.Filesize() != 1024 {
		t.Errorf("Filesize() = %d, expected 1024", b.Filesize())
	}
	if b.Availability() != AvailabilityVerified {
		t.Errorf("Availability() = %d, expected %d", b.Availability(), AvailabilityVerified)
	}
}

func TestNewEmptyFilename(t *testing.T) {
	_, err := New("", "/path", FormatFB2, 100)
	if err == nil {
		t.Error("New with empty filename should return error")
	}
	if !errors.Is(err, ErrEmptyFilename) {
		t.Errorf("Expected ErrEmptyFilename, got %v", err)
	}
}

func TestNewNegativeFilesize(t *testing.T) {
	_, err := New("test.fb2", "/path", FormatFB2, -1)
	if err == nil {
		t.Error("New with negative filesize should return error")
	}
	if !errors.Is(err, ErrInvalidFilesize) {
		t.Errorf("Expected ErrInvalidFilesize, got %v", err)
	}
}

func TestReconstruct(t *testing.T) {
	now := time.Now()
	duplicateID := ID(5)
	cover := NewCover("data", "image/jpeg")

	b := Reconstruct(
		42,
		"test.fb2", "/path",
		FormatFB2,
		1024,
		"Test Book", "Annotation", "2024-01-01",
		LangEnglish,
		now,
		1,
		CatalogTypeNormal,
		AvailabilityVerified,
		&duplicateID,
		cover,
		true,
		true,
		3600, 128, 10,
		"[]",
	)

	if b.ID() != 42 {
		t.Errorf("ID() = %d, expected 42", b.ID())
	}
	if b.Title() != "Test Book" {
		t.Errorf("Title() = %q, expected Test Book", b.Title())
	}
	if b.Annotation() != "Annotation" {
		t.Errorf("Annotation() = %q, expected Annotation", b.Annotation())
	}
	if b.Language() != LangEnglish {
		t.Errorf("Language() = %q, expected en", b.Language())
	}
	if *b.DuplicateOf() != 5 {
		t.Errorf("DuplicateOf() = %d, expected 5", *b.DuplicateOf())
	}
	if !b.IsFavorite() {
		t.Error("IsFavorite() should be true")
	}
	if !b.IsAudiobook() {
		t.Error("IsAudiobook() should be true")
	}
	if b.DurationSeconds() != 3600 {
		t.Errorf("DurationSeconds() = %d, expected 3600", b.DurationSeconds())
	}
	if b.Bitrate() != 128 {
		t.Errorf("Bitrate() = %d, expected 128", b.Bitrate())
	}
	if b.TrackCount() != 10 {
		t.Errorf("TrackCount() = %d, expected 10", b.TrackCount())
	}
}

func TestMarkAsDuplicate(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.SetID(1)

	err := b.MarkAsDuplicate(5)
	if err != nil {
		t.Fatalf("MarkAsDuplicate() returned error: %v", err)
	}
	if !b.IsDuplicate() {
		t.Error("IsDuplicate() should be true after marking")
	}
	if *b.DuplicateOf() != 5 {
		t.Errorf("DuplicateOf() = %d, expected 5", *b.DuplicateOf())
	}
}

func TestMarkAsDuplicateSelf(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.SetID(1)

	err := b.MarkAsDuplicate(1)
	if err == nil {
		t.Error("MarkAsDuplicate(self) should return error")
	}
	if !errors.Is(err, ErrSelfDuplicate) {
		t.Errorf("Expected ErrSelfDuplicate, got %v", err)
	}
}

func TestClearDuplicateStatus(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.SetID(1)
	b.MarkAsDuplicate(5)

	b.ClearDuplicateStatus()
	if b.IsDuplicate() {
		t.Error("IsDuplicate() should be false after clearing")
	}
}

func TestMarkAsDeleted(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.MarkAsDeleted()
	if b.Availability() != AvailabilityDeleted {
		t.Errorf("Availability() = %d, expected %d", b.Availability(), AvailabilityDeleted)
	}
	if b.IsAvailable() {
		t.Error("IsAvailable() should be false after MarkAsDeleted")
	}
}

func TestMarkAsPending(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	// Initially verified
	b.MarkAsPending()
	if b.Availability() != AvailabilityPending {
		t.Errorf("Availability() = %d, expected %d", b.Availability(), AvailabilityPending)
	}
}

func TestMarkAsPendingIgnoresDeleted(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.MarkAsDeleted()
	b.MarkAsPending()
	// Should still be deleted, not changed to pending
	if b.Availability() != AvailabilityDeleted {
		t.Errorf("MarkAsPending should not change deleted status")
	}
}

func TestVerify(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.MarkAsPending()
	b.Verify()
	if b.Availability() != AvailabilityVerified {
		t.Errorf("Availability() = %d, expected %d after Verify", b.Availability(), AvailabilityVerified)
	}
}

func TestVerifyIgnoresNonPending(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.MarkAsDeleted()
	b.Verify()
	// Should still be deleted
	if b.Availability() != AvailabilityDeleted {
		t.Errorf("Verify should not change deleted status")
	}
}

func TestSetMetadata(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.SetMetadata("New Title", "New Annotation", "2024-06-01", LangEnglish)

	if b.Title() != "New Title" {
		t.Errorf("Title() = %q, expected New Title", b.Title())
	}
	if b.Annotation() != "New Annotation" {
		t.Errorf("Annotation() = %q, expected New Annotation", b.Annotation())
	}
	if b.Language() != LangEnglish {
		t.Errorf("Language() = %q, expected en", b.Language())
	}
}

func TestSetCover(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	cover := NewCover("base64data", "image/png")
	b.SetCover(cover)

	if !b.Cover().HasCover() {
		t.Error("Cover().HasCover() should be true")
	}
	if b.Cover().Data() != "base64data" {
		t.Errorf("Cover().Data() = %q, expected base64data", b.Cover().Data())
	}
}

func TestToggleFavorite(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	if b.IsFavorite() {
		t.Error("New book should not be favorite")
	}

	b.ToggleFavorite()
	if !b.IsFavorite() {
		t.Error("IsFavorite() should be true after toggle")
	}

	b.ToggleFavorite()
	if b.IsFavorite() {
		t.Error("IsFavorite() should be false after second toggle")
	}
}

func TestSetFavorite(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.SetFavorite(true)
	if !b.IsFavorite() {
		t.Error("IsFavorite() should be true after SetFavorite(true)")
	}
	b.SetFavorite(false)
	if b.IsFavorite() {
		t.Error("IsFavorite() should be false after SetFavorite(false)")
	}
}

func TestAssignToCatalog(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	b.AssignToCatalog(42, CatalogTypeZip)

	if b.CatalogID() != 42 {
		t.Errorf("CatalogID() = %d, expected 42", b.CatalogID())
	}
	if b.CatalogType() != CatalogTypeZip {
		t.Errorf("CatalogType() = %d, expected %d", b.CatalogType(), CatalogTypeZip)
	}
}

func TestSetID(t *testing.T) {
	b, _ := New("test.fb2", "/path", FormatFB2, 100)
	if b.ID() != 0 {
		t.Errorf("Initial ID() = %d, expected 0", b.ID())
	}
	b.SetID(123)
	if b.ID() != 123 {
		t.Errorf("ID() = %d, expected 123 after SetID", b.ID())
	}
}

// --- Format tests ---

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
	}{
		{"fb2", FormatFB2},
		{"epub", FormatEPUB},
		{"mobi", FormatMOBI},
		{"pdf", FormatPDF},
		{"djvu", FormatDJVU},
		{"mp3", FormatMP3},
		{"m4b", FormatM4B},
		{"m4a", FormatM4A},
		{"flac", FormatFLAC},
		{"ogg", FormatOGG},
		{"opus", FormatOPUS},
		{"unknown", Format("unknown")},
	}

	for _, tc := range tests {
		result := ParseFormat(tc.input)
		if result != tc.expected {
			t.Errorf("ParseFormat(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestFormatString(t *testing.T) {
	if FormatFB2.String() != "fb2" {
		t.Errorf("FormatFB2.String() = %q, expected fb2", FormatFB2.String())
	}
}

func TestFormatIsConvertible(t *testing.T) {
	tests := []struct {
		format   Format
		expected bool
	}{
		{FormatFB2, true},
		{FormatEPUB, false},
		{FormatMOBI, false},
		{FormatMP3, false},
	}

	for _, tc := range tests {
		result := tc.format.IsConvertible()
		if result != tc.expected {
			t.Errorf("Format(%q).IsConvertible() = %v, expected %v", tc.format, result, tc.expected)
		}
	}
}

func TestFormatIsAudio(t *testing.T) {
	tests := []struct {
		format   Format
		expected bool
	}{
		{FormatFB2, false},
		{FormatEPUB, false},
		{FormatMP3, true},
		{FormatM4B, true},
		{FormatM4A, true},
		{FormatFLAC, true},
		{FormatOGG, true},
		{FormatOPUS, true},
	}

	for _, tc := range tests {
		result := tc.format.IsAudio()
		if result != tc.expected {
			t.Errorf("Format(%q).IsAudio() = %v, expected %v", tc.format, result, tc.expected)
		}
	}
}

func TestFormatIsEbook(t *testing.T) {
	tests := []struct {
		format   Format
		expected bool
	}{
		{FormatFB2, true},
		{FormatEPUB, true},
		{FormatMOBI, true},
		{FormatPDF, true},
		{FormatDJVU, true},
		{FormatMP3, false},
		{FormatM4B, false},
	}

	for _, tc := range tests {
		result := tc.format.IsEbook()
		if result != tc.expected {
			t.Errorf("Format(%q).IsEbook() = %v, expected %v", tc.format, result, tc.expected)
		}
	}
}

// --- Language tests ---

func TestLanguageString(t *testing.T) {
	if LangEnglish.String() != "en" {
		t.Errorf("LangEnglish.String() = %q, expected en", LangEnglish.String())
	}
}

func TestLanguageIsUnknown(t *testing.T) {
	tests := []struct {
		lang     Language
		expected bool
	}{
		{LangEnglish, false},
		{LangRussian, false},
		{LangUnknown, true},
		{Language(""), true},
	}

	for _, tc := range tests {
		result := tc.lang.IsUnknown()
		if result != tc.expected {
			t.Errorf("Language(%q).IsUnknown() = %v, expected %v", tc.lang, result, tc.expected)
		}
	}
}

// --- Availability tests ---

func TestAvailabilityString(t *testing.T) {
	tests := []struct {
		avail    Availability
		expected string
	}{
		{AvailabilityDeleted, "deleted"},
		{AvailabilityPending, "pending"},
		{AvailabilityVerified, "available"},
		{Availability(99), "unknown"},
	}

	for _, tc := range tests {
		result := tc.avail.String()
		if result != tc.expected {
			t.Errorf("Availability(%d).String() = %q, expected %q", tc.avail, result, tc.expected)
		}
	}
}

func TestAvailabilityIsAvailable(t *testing.T) {
	tests := []struct {
		avail    Availability
		expected bool
	}{
		{AvailabilityDeleted, false},
		{AvailabilityPending, true},
		{AvailabilityVerified, true},
	}

	for _, tc := range tests {
		result := tc.avail.IsAvailable()
		if result != tc.expected {
			t.Errorf("Availability(%d).IsAvailable() = %v, expected %v", tc.avail, result, tc.expected)
		}
	}
}

// --- CatalogType tests ---

func TestCatalogTypeString(t *testing.T) {
	tests := []struct {
		catType  CatalogType
		expected string
	}{
		{CatalogTypeNormal, "directory"},
		{CatalogTypeZip, "zip"},
		{CatalogTypeGzip, "gzip"},
		{CatalogType(99), "unknown"},
	}

	for _, tc := range tests {
		result := tc.catType.String()
		if result != tc.expected {
			t.Errorf("CatalogType(%d).String() = %q, expected %q", tc.catType, result, tc.expected)
		}
	}
}

func TestCatalogTypeIsArchive(t *testing.T) {
	tests := []struct {
		catType  CatalogType
		expected bool
	}{
		{CatalogTypeNormal, false},
		{CatalogTypeZip, true},
		{CatalogTypeGzip, true},
	}

	for _, tc := range tests {
		result := tc.catType.IsArchive()
		if result != tc.expected {
			t.Errorf("CatalogType(%d).IsArchive() = %v, expected %v", tc.catType, result, tc.expected)
		}
	}
}

// --- Cover tests ---

func TestNewCover(t *testing.T) {
	cover := NewCover("base64data", "image/jpeg")
	if cover.Data() != "base64data" {
		t.Errorf("Data() = %q, expected base64data", cover.Data())
	}
	if cover.ContentType() != "image/jpeg" {
		t.Errorf("ContentType() = %q, expected image/jpeg", cover.ContentType())
	}
}

func TestEmptyCover(t *testing.T) {
	cover := EmptyCover()
	if cover.HasCover() {
		t.Error("EmptyCover().HasCover() should be false")
	}
	if cover.Data() != "" {
		t.Errorf("EmptyCover().Data() = %q, expected empty", cover.Data())
	}
}

func TestCoverHasCover(t *testing.T) {
	tests := []struct {
		data     string
		expected bool
	}{
		{"some data", true},
		{"", false},
	}

	for _, tc := range tests {
		cover := NewCover(tc.data, "image/png")
		if cover.HasCover() != tc.expected {
			t.Errorf("Cover{data: %q}.HasCover() = %v, expected %v", tc.data, cover.HasCover(), tc.expected)
		}
	}
}

// --- AuthorRef tests ---

func TestAuthorRefFullName(t *testing.T) {
	tests := []struct {
		firstName string
		lastName  string
		expected  string
	}{
		{"John", "Doe", "Doe John"},
		{"", "Doe", "Doe"},
		{"John", "", "John"},
		{"", "", ""},
	}

	for _, tc := range tests {
		ref := AuthorRef{FirstName: tc.firstName, LastName: tc.lastName}
		result := ref.FullName()
		if result != tc.expected {
			t.Errorf("AuthorRef{%q, %q}.FullName() = %q, expected %q",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

// --- GenreRef tests ---

func TestGenreRefDisplayName(t *testing.T) {
	tests := []struct {
		code       string
		subsection string
		expected   string
	}{
		{"sf", "Science Fiction", "Science Fiction"},
		{"sf", "", "sf"},
	}

	for _, tc := range tests {
		ref := GenreRef{Code: tc.code, Subsection: tc.subsection}
		result := ref.DisplayName()
		if result != tc.expected {
			t.Errorf("GenreRef{Code: %q, Subsection: %q}.DisplayName() = %q, expected %q",
				tc.code, tc.subsection, result, tc.expected)
		}
	}
}
