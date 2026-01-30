package tts

import (
	"testing"
	"time"
)

// --- Extractor tests ---

func TestExtractPlainTextFromContent(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "simple paragraph",
			input:    []byte("<p>Hello world</p>"),
			expected: "Hello world",
		},
		{
			name:     "multiple paragraphs",
			input:    []byte("<p>First paragraph.</p><p>Second paragraph.</p>"),
			expected: "First paragraph.\nSecond paragraph.",
		},
		{
			name:     "with emphasis",
			input:    []byte("<p>This is <emphasis>important</emphasis> text.</p>"),
			expected: "This is important text.",
		},
		{
			name:     "empty line",
			input:    []byte("<p>Before</p><empty-line/><p>After</p>"),
			expected: "Before\nAfter",
		},
		{
			name:     "verse",
			input:    []byte("<v>Line one</v><v>Line two</v>"),
			expected: "Line one\nLine two",
		},
		{
			name:     "empty input",
			input:    []byte{},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractPlainTextFromContent(tc.input)
			if result != tc.expected {
				t.Errorf("extractPlainTextFromContent() = %q, expected %q", result, tc.expected)
			}
		})
	}
}

func TestFindSplitPoint(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		minSplit int // minimum expected split point
	}{
		{
			name:     "split at paragraph",
			text:     "First paragraph.\n\nSecond paragraph.",
			maxLen:   25,
			minSplit: 18, // After "\n\n"
		},
		{
			name:     "split at sentence",
			text:     "First sentence. Second sentence.",
			maxLen:   20,
			minSplit: 16, // After ". "
		},
		{
			name:     "split at space",
			text:     "Word1 Word2 Word3 Word4",
			maxLen:   15,
			minSplit: 6, // After "Word1 "
		},
		{
			name:     "short text",
			text:     "Short",
			maxLen:   100,
			minSplit: 5, // Full length
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := findSplitPoint(tc.text, tc.maxLen)
			if result < tc.minSplit {
				t.Errorf("findSplitPoint() = %d, expected at least %d", result, tc.minSplit)
			}
		})
	}
}

// --- Cache tests ---

func TestCache(t *testing.T) {
	// Use temp directory
	cache := NewCache(t.TempDir())

	// Test EnsureDir
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}

	// Test HasBook (should be false initially)
	if cache.HasBook(123) {
		t.Error("HasBook(123) should be false initially")
	}

	// Test GetBookDir
	dir := cache.GetBookDir(123)
	if dir == "" {
		t.Error("GetBookDir() should return non-empty path")
	}

	// Test SaveMetadata and GetMetadata
	meta := &CacheMetadata{
		BookID:     123,
		Voice:      "en_US-lessac-medium",
		Lang:       "en",
		ChunkCount: 5,
		TotalChars: 25000,
		CreatedAt:  time.Now(),
		Status:     "completed",
	}

	if err := cache.SaveMetadata(123, meta); err != nil {
		t.Fatalf("SaveMetadata() error: %v", err)
	}

	// Now HasBook should be true
	if !cache.HasBook(123) {
		t.Error("HasBook(123) should be true after SaveMetadata")
	}

	// GetMetadata
	loaded, err := cache.GetMetadata(123)
	if err != nil {
		t.Fatalf("GetMetadata() error: %v", err)
	}
	if loaded.BookID != 123 {
		t.Errorf("Loaded BookID = %d, expected 123", loaded.BookID)
	}
	if loaded.Voice != "en_US-lessac-medium" {
		t.Errorf("Loaded Voice = %q, expected en_US-lessac-medium", loaded.Voice)
	}
	if loaded.ChunkCount != 5 {
		t.Errorf("Loaded ChunkCount = %d, expected 5", loaded.ChunkCount)
	}

	// Test Clear
	if err := cache.Clear(123); err != nil {
		t.Fatalf("Clear() error: %v", err)
	}
	if cache.HasBook(123) {
		t.Error("HasBook(123) should be false after Clear")
	}
}

func TestCacheGetChunkPath(t *testing.T) {
	cache := NewCache("/tmp/tts_cache")

	path := cache.GetChunkPath(123, 5)
	if path != "/tmp/tts_cache/123/chunk_005.wav" {
		t.Errorf("GetChunkPath() = %q, expected /tmp/tts_cache/123/chunk_005.wav", path)
	}
}

func TestCacheIsComplete(t *testing.T) {
	cache := NewCache(t.TempDir())
	cache.EnsureDir()

	// Not complete initially
	if cache.IsComplete(456) {
		t.Error("IsComplete() should be false for non-existent book")
	}

	// Save incomplete metadata
	meta := &CacheMetadata{
		BookID: 456,
		Status: "generating",
	}
	cache.SaveMetadata(456, meta)
	if cache.IsComplete(456) {
		t.Error("IsComplete() should be false for generating status")
	}

	// Mark as complete
	meta.Status = "completed"
	cache.SaveMetadata(456, meta)
	if !cache.IsComplete(456) {
		t.Error("IsComplete() should be true for completed status")
	}
}

// --- Queue tests ---

func TestJobQueue(t *testing.T) {
	q := NewJobQueue(10)

	// Add a job
	job, err := q.Add(123, "Test Book", "en_US-lessac-medium", "en")
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	if job.BookID != 123 {
		t.Errorf("Job.BookID = %d, expected 123", job.BookID)
	}
	if job.Status != JobStatusQueued {
		t.Errorf("Job.Status = %s, expected queued", job.Status)
	}

	// Get by ID
	retrieved := q.Get(job.ID)
	if retrieved == nil {
		t.Fatal("Get() returned nil")
	}
	if retrieved.ID != job.ID {
		t.Errorf("Get().ID = %s, expected %s", retrieved.ID, job.ID)
	}

	// Get by book
	byBook := q.GetByBook(123)
	if byBook == nil {
		t.Fatal("GetByBook() returned nil")
	}
	if byBook.ID != job.ID {
		t.Errorf("GetByBook().ID = %s, expected %s", byBook.ID, job.ID)
	}

	// Adding same book should return existing job
	job2, _ := q.Add(123, "Test Book", "en_US-lessac-medium", "en")
	if job2.ID != job.ID {
		t.Error("Adding same book should return existing job")
	}
}

func TestJobQueueUpdateStatus(t *testing.T) {
	q := NewJobQueue(10)
	job, _ := q.Add(123, "Test", "voice", "en")

	// Update to processing
	q.UpdateStatus(job.ID, JobStatusProcessing, "")
	retrieved := q.Get(job.ID)
	if retrieved.Status != JobStatusProcessing {
		t.Errorf("Status = %s, expected processing", retrieved.Status)
	}
	if retrieved.StartedAt.IsZero() {
		t.Error("StartedAt should be set when processing")
	}

	// Update to completed
	q.UpdateStatus(job.ID, JobStatusCompleted, "")
	retrieved = q.Get(job.ID)
	if retrieved.Status != JobStatusCompleted {
		t.Errorf("Status = %s, expected completed", retrieved.Status)
	}
	if retrieved.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set when completed")
	}
	if retrieved.Progress != 1.0 {
		t.Errorf("Progress = %f, expected 1.0", retrieved.Progress)
	}
}

func TestJobQueueUpdateProgress(t *testing.T) {
	q := NewJobQueue(10)
	job, _ := q.Add(123, "Test", "voice", "en")

	q.UpdateProgress(job.ID, 3, 10)
	retrieved := q.Get(job.ID)

	if retrieved.ChunksDone != 3 {
		t.Errorf("ChunksDone = %d, expected 3", retrieved.ChunksDone)
	}
	if retrieved.ChunksTotal != 10 {
		t.Errorf("ChunksTotal = %d, expected 10", retrieved.ChunksTotal)
	}
	if retrieved.Progress != 0.3 {
		t.Errorf("Progress = %f, expected 0.3", retrieved.Progress)
	}
}

func TestJobQueueListActive(t *testing.T) {
	q := NewJobQueue(10)

	q.Add(1, "Book 1", "voice", "en")
	q.Add(2, "Book 2", "voice", "en")
	job3, _ := q.Add(3, "Book 3", "voice", "en")

	// Mark one as completed
	q.UpdateStatus(job3.ID, JobStatusCompleted, "")

	active := q.ListActive()
	if len(active) != 2 {
		t.Errorf("ListActive() returned %d jobs, expected 2", len(active))
	}
}

func TestJobQueueRemove(t *testing.T) {
	q := NewJobQueue(10)
	job, _ := q.Add(123, "Test", "voice", "en")

	q.Remove(job.ID)

	if q.Get(job.ID) != nil {
		t.Error("Get() should return nil after Remove")
	}
	if q.GetByBook(123) != nil {
		t.Error("GetByBook() should return nil after Remove")
	}
}

// --- Generator tests ---

func TestVoiceSelection(t *testing.T) {
	// Test the voice selection logic used by Generator.GetVoice
	voices := map[string]string{
		"en": "en_US-lessac-medium",
		"uk": "uk_UA-lada-x_low",
	}
	defaultVoice := "en_US-lessac-medium"

	getVoice := func(lang string) string {
		if voice, ok := voices[lang]; ok {
			return voice
		}
		if defaultVoice != "" {
			return defaultVoice
		}
		return "en_US-lessac-medium"
	}

	// Test existing language
	if voice := getVoice("en"); voice != "en_US-lessac-medium" {
		t.Errorf("Voice for 'en' = %q, expected en_US-lessac-medium", voice)
	}

	if voice := getVoice("uk"); voice != "uk_UA-lada-x_low" {
		t.Errorf("Voice for 'uk' = %q, expected uk_UA-lada-x_low", voice)
	}

	// Test missing language falls back to default
	if voice := getVoice("de"); voice != "en_US-lessac-medium" {
		t.Errorf("Voice for 'de' should fall back to default, got %q", voice)
	}
}

func TestTextChunkStruct(t *testing.T) {
	chunk := TextChunk{
		Index: 0,
		Title: "Chapter 1",
		Text:  "This is the chapter content.",
	}

	if chunk.Index != 0 {
		t.Errorf("Index = %d, expected 0", chunk.Index)
	}
	if chunk.Title != "Chapter 1" {
		t.Errorf("Title = %q, expected Chapter 1", chunk.Title)
	}
}

func TestChunkInfoStruct(t *testing.T) {
	info := ChunkInfo{
		Index:    5,
		Title:    "Section 5",
		Chars:    1500,
		Duration: 45.5,
		File:     "chunk_005.wav",
	}

	if info.Index != 5 {
		t.Errorf("Index = %d, expected 5", info.Index)
	}
	if info.Duration != 45.5 {
		t.Errorf("Duration = %f, expected 45.5", info.Duration)
	}
}

func TestJobStatusConstants(t *testing.T) {
	if JobStatusQueued != "queued" {
		t.Errorf("JobStatusQueued = %q, expected queued", JobStatusQueued)
	}
	if JobStatusProcessing != "processing" {
		t.Errorf("JobStatusProcessing = %q, expected processing", JobStatusProcessing)
	}
	if JobStatusCompleted != "completed" {
		t.Errorf("JobStatusCompleted = %q, expected completed", JobStatusCompleted)
	}
	if JobStatusFailed != "failed" {
		t.Errorf("JobStatusFailed = %q, expected failed", JobStatusFailed)
	}
}
