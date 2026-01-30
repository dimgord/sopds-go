package tts

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sopds/sopds-go/internal/config"
)

// Generator manages TTS audio generation using piper
type Generator struct {
	config   *config.TTSConfig
	queue    *JobQueue
	cache    *Cache
	getBook  BookGetter // callback to get book data
	running  bool
	mu       sync.RWMutex
	cancelFn context.CancelFunc
}

// BookGetter is a function that retrieves book data for TTS
type BookGetter func(bookID int64) (data []byte, title string, err error)

// NewGenerator creates a new TTS generator
func NewGenerator(cfg *config.TTSConfig, getBook BookGetter) *Generator {
	return &Generator{
		config:  cfg,
		queue:   NewJobQueue(100),
		cache:   NewCache(cfg.CacheDir),
		getBook: getBook,
	}
}

// Queue returns the job queue for external access
func (g *Generator) Queue() *JobQueue {
	return g.queue
}

// Cache returns the cache for external access
func (g *Generator) Cache() *Cache {
	return g.cache
}

// IsAvailable checks if piper is available
func (g *Generator) IsAvailable() bool {
	if g.config.PiperPath == "" {
		return false
	}
	_, err := exec.LookPath(g.config.PiperPath)
	return err == nil
}

// GetVoice returns the voice model for a language
func (g *Generator) GetVoice(lang string) string {
	// Check configured voices
	if voice, ok := g.config.Voices[lang]; ok {
		return voice
	}
	// Fall back to default
	if g.config.DefaultVoice != "" {
		return g.config.DefaultVoice
	}
	// Last resort
	return "en_US-lessac-medium"
}

// GetModelPath returns the full path to a voice model
func (g *Generator) GetModelPath(voice string) string {
	// If voice is already a full path, use it
	if filepath.IsAbs(voice) {
		return voice
	}
	// Check if it already has .onnx extension
	if !strings.HasSuffix(voice, ".onnx") {
		voice = voice + ".onnx"
	}
	return filepath.Join(g.config.ModelsDir, voice)
}

// QueueBook queues a book for TTS generation
func (g *Generator) QueueBook(bookID int64, bookTitle, lang string) (*Job, error) {
	voice := g.GetVoice(lang)
	return g.queue.Add(bookID, bookTitle, voice, lang)
}

// GetJob returns job status
func (g *Generator) GetJob(jobID string) *Job {
	return g.queue.Get(jobID)
}

// GetJobByBook returns job for a book
func (g *Generator) GetJobByBook(bookID int64) *Job {
	return g.queue.GetByBook(bookID)
}

// Start starts the worker goroutines
func (g *Generator) Start(ctx context.Context) {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return
	}
	g.running = true

	ctx, g.cancelFn = context.WithCancel(ctx)
	g.mu.Unlock()

	// Ensure cache directory exists
	if err := g.cache.EnsureDir(); err != nil {
		log.Printf("TTS: Failed to create cache directory: %v", err)
	}

	workers := g.config.Workers
	if workers <= 0 {
		workers = 2
	}

	log.Printf("TTS: Starting %d workers", workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			g.worker(ctx, workerID)
		}(i)
	}

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		log.Printf("TTS: All workers stopped")
	}()
}

// Stop stops all workers
func (g *Generator) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancelFn != nil {
		g.cancelFn()
	}
}

// worker processes jobs from the queue
func (g *Generator) worker(ctx context.Context, id int) {
	log.Printf("TTS worker %d: Started", id)

	for {
		select {
		case <-ctx.Done():
			log.Printf("TTS worker %d: Stopping", id)
			return
		default:
		}

		// Try to get a job (non-blocking to allow context check)
		job := g.queue.NextNonBlocking()
		if job == nil {
			// No job available, wait a bit
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}

		log.Printf("TTS worker %d: Processing job %s for book %d", id, job.ID, job.BookID)
		g.processJob(ctx, job)
	}
}

// processJob processes a single TTS job
func (g *Generator) processJob(ctx context.Context, job *Job) {
	g.queue.UpdateStatus(job.ID, JobStatusProcessing, "")

	// Get book data
	data, title, err := g.getBook(job.BookID)
	if err != nil {
		g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Failed to get book: %v", err))
		return
	}

	if title != "" && job.BookTitle == "" {
		job.BookTitle = title
	}

	// Extract text from FB2
	chunks, lang, err := ExtractTextFromFB2(data, g.config.ChunkSize)
	if err != nil {
		g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Failed to extract text: %v", err))
		return
	}

	if len(chunks) == 0 {
		g.queue.UpdateStatus(job.ID, JobStatusFailed, "No text content found in book")
		return
	}

	// Use detected language if not specified
	if job.Lang == "" {
		job.Lang = lang
	}

	// Get voice for language
	voice := g.GetVoice(job.Lang)
	modelPath := g.GetModelPath(voice)

	// Check if model exists
	if _, err := os.Stat(modelPath); err != nil {
		g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Voice model not found: %s", modelPath))
		return
	}

	// Initialize cache metadata
	meta := &CacheMetadata{
		BookID:     job.BookID,
		Voice:      voice,
		Lang:       job.Lang,
		ChunkCount: len(chunks),
		CreatedAt:  time.Now(),
		Status:     "generating",
		Chunks:     make([]ChunkInfo, len(chunks)),
	}

	totalChars := 0
	for i, chunk := range chunks {
		meta.Chunks[i] = ChunkInfo{
			Index: i,
			Title: chunk.Title,
			Chars: len(chunk.Text),
			File:  fmt.Sprintf("chunk_%03d.wav", i),
		}
		totalChars += len(chunk.Text)
	}
	meta.TotalChars = totalChars

	if err := g.cache.SaveMetadata(job.BookID, meta); err != nil {
		g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Failed to save metadata: %v", err))
		return
	}

	g.queue.UpdateProgress(job.ID, 0, len(chunks))

	// Process each chunk
	for i, chunk := range chunks {
		select {
		case <-ctx.Done():
			g.queue.UpdateStatus(job.ID, JobStatusFailed, "Cancelled")
			meta.Status = "failed"
			meta.Error = "Cancelled"
			g.cache.SaveMetadata(job.BookID, meta)
			return
		default:
		}

		outputPath := g.cache.GetChunkPath(job.BookID, i)

		if err := g.generateChunk(chunk.Text, modelPath, outputPath); err != nil {
			g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Failed to generate chunk %d: %v", i, err))
			meta.Status = "failed"
			meta.Error = fmt.Sprintf("Failed at chunk %d: %v", i, err)
			g.cache.SaveMetadata(job.BookID, meta)
			return
		}

		g.queue.UpdateProgress(job.ID, i+1, len(chunks))
		log.Printf("TTS: Book %d chunk %d/%d completed", job.BookID, i+1, len(chunks))
	}

	// Mark as completed
	meta.Status = "completed"
	meta.CompletedAt = time.Now()
	g.cache.SaveMetadata(job.BookID, meta)

	g.queue.UpdateStatus(job.ID, JobStatusCompleted, "")
	log.Printf("TTS: Book %d completed (%d chunks)", job.BookID, len(chunks))
}

// generateChunk generates audio for a single text chunk using piper
func (g *Generator) generateChunk(text, modelPath, outputPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build piper command
	// piper --model <model> --output_file <output> < text
	cmd := exec.Command(g.config.PiperPath,
		"--model", modelPath,
		"--output_file", outputPath,
	)

	// Pass text via stdin
	cmd.Stdin = strings.NewReader(text)

	// Capture stderr for error messages
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("piper failed: %v, output: %s", err, string(output))
	}

	// Verify output file was created
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created: %w", err)
	}

	return nil
}
