package tts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cache manages TTS audio file caching
type Cache struct {
	dir string
}

// CacheMetadata stores information about cached TTS audio
type CacheMetadata struct {
	BookID      int64     `json:"book_id"`
	Voice       string    `json:"voice"`
	Lang        string    `json:"lang"`
	ChunkCount  int       `json:"chunk_count"`
	TotalChars  int       `json:"total_chars"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Status      string    `json:"status"` // generating, completed, failed
	Error       string    `json:"error,omitempty"`
	Chunks      []ChunkInfo `json:"chunks"`
}

// ChunkInfo stores information about a single chunk
type ChunkInfo struct {
	Index    int    `json:"index"`
	Title    string `json:"title,omitempty"`
	Chars    int    `json:"chars"`
	Duration float64 `json:"duration,omitempty"` // seconds, filled after generation
	File     string `json:"file"`
}

// NewCache creates a new cache manager
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// EnsureDir creates the cache directory if it doesn't exist
func (c *Cache) EnsureDir() error {
	return os.MkdirAll(c.dir, 0755)
}

// GetBookDir returns the cache directory for a book
func (c *Cache) GetBookDir(bookID int64) string {
	return filepath.Join(c.dir, fmt.Sprintf("%d", bookID))
}

// HasBook checks if a book has cached audio
func (c *Cache) HasBook(bookID int64) bool {
	metaPath := filepath.Join(c.GetBookDir(bookID), "metadata.json")
	_, err := os.Stat(metaPath)
	return err == nil
}

// IsComplete checks if TTS generation is complete for a book
func (c *Cache) IsComplete(bookID int64) bool {
	meta, err := c.GetMetadata(bookID)
	if err != nil {
		return false
	}
	return meta.Status == "completed"
}

// GetChunkPath returns the path to a chunk audio file
func (c *Cache) GetChunkPath(bookID int64, index int) string {
	return filepath.Join(c.GetBookDir(bookID), fmt.Sprintf("chunk_%03d.wav", index))
}

// GetMetadataPath returns the path to the metadata file
func (c *Cache) GetMetadataPath(bookID int64) string {
	return filepath.Join(c.GetBookDir(bookID), "metadata.json")
}

// GetMetadata reads the cache metadata for a book
func (c *Cache) GetMetadata(bookID int64) (*CacheMetadata, error) {
	path := c.GetMetadataPath(bookID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta CacheMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// SaveMetadata writes the cache metadata for a book
func (c *Cache) SaveMetadata(bookID int64, meta *CacheMetadata) error {
	bookDir := c.GetBookDir(bookID)
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.GetMetadataPath(bookID), data, 0644)
}

// Clear removes all cached audio for a book
func (c *Cache) Clear(bookID int64) error {
	return os.RemoveAll(c.GetBookDir(bookID))
}

// GetProgress returns the generation progress (0.0 - 1.0)
func (c *Cache) GetProgress(bookID int64) float64 {
	meta, err := c.GetMetadata(bookID)
	if err != nil {
		return 0
	}

	if meta.ChunkCount == 0 {
		return 0
	}

	// Count existing chunk files
	completed := 0
	for i := 0; i < meta.ChunkCount; i++ {
		path := c.GetChunkPath(bookID, i)
		if _, err := os.Stat(path); err == nil {
			completed++
		}
	}

	return float64(completed) / float64(meta.ChunkCount)
}

// ListCachedBooks returns a list of book IDs with cached audio
func (c *Cache) ListCachedBooks() ([]int64, error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var bookIDs []int64
	for _, entry := range entries {
		if entry.IsDir() {
			id, err := strconv.ParseInt(entry.Name(), 10, 64)
			if err == nil {
				bookIDs = append(bookIDs, id)
			}
		}
	}

	sort.Slice(bookIDs, func(i, j int) bool {
		return bookIDs[i] < bookIDs[j]
	})

	return bookIDs, nil
}

// GetTotalSize returns the total size of cached audio in bytes
func (c *Cache) GetTotalSize() (int64, error) {
	var total int64

	err := filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore errors
		}
		if !info.IsDir() && strings.HasSuffix(path, ".wav") {
			total += info.Size()
		}
		return nil
	})

	return total, err
}

// CleanOldCache removes cache entries older than maxAge
func (c *Cache) CleanOldCache(maxAge time.Duration) error {
	bookIDs, err := c.ListCachedBooks()
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)

	for _, bookID := range bookIDs {
		meta, err := c.GetMetadata(bookID)
		if err != nil {
			continue
		}

		if meta.CreatedAt.Before(cutoff) {
			c.Clear(bookID)
		}
	}

	return nil
}
