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
	"sync/atomic"
	"time"

	"github.com/dimgord/sopds-go/internal/config"
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

// IsAvailable checks if TTS is available (sopds-tts binary exists)
func (g *Generator) IsAvailable() bool {
	// Check if sopds-tts binary exists
	ttsBinary := getTTSBinaryPath()
	if _, err := os.Stat(ttsBinary); err != nil {
		return false
	}
	// Check if models directory exists
	if _, err := os.Stat(g.config.ModelsDir); err != nil {
		return false
	}
	return true
}

// GetVoice returns the voice model for a language
func (g *Generator) GetVoice(lang string) string {
	// Check configured voices (exact match)
	if voice, ok := g.config.Voices[lang]; ok {
		return voice
	}
	// Try base language code (e.g., "uk-UA" -> "uk", "en-US" -> "en")
	if idx := strings.IndexAny(lang, "-_"); idx > 0 {
		base := lang[:idx]
		if voice, ok := g.config.Voices[base]; ok {
			return voice
		}
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

	// Process chunks in parallel using subprocess workers
	numWorkers := g.config.Workers
	if numWorkers <= 0 {
		numWorkers = 2
	}
	if numWorkers > len(chunks) {
		numWorkers = len(chunks)
	}

	type chunkTask struct {
		index int
		chunk TextChunk
	}

	taskChan := make(chan chunkTask, len(chunks))
	errChan := make(chan error, 1) // buffered to avoid blocking
	var completed int64
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				// Check for cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				outputPath := g.cache.GetChunkPath(job.BookID, task.index)
				if err := g.generateChunk(task.chunk.Text, modelPath, outputPath); err != nil {
					select {
					case errChan <- fmt.Errorf("chunk %d: %w", task.index, err):
					default:
					}
					return
				}

				done := atomic.AddInt64(&completed, 1)
				g.queue.UpdateProgress(job.ID, int(done), len(chunks))
				log.Printf("TTS: Book %d chunk %d/%d completed", job.BookID, done, len(chunks))
			}
		}()
	}

	// Send tasks
	for i, chunk := range chunks {
		taskChan <- chunkTask{index: i, chunk: chunk}
	}
	close(taskChan)

	// Wait for completion
	wg.Wait()

	// Check for errors
	select {
	case err := <-errChan:
		g.queue.UpdateStatus(job.ID, JobStatusFailed, fmt.Sprintf("Failed to generate: %v", err))
		meta.Status = "failed"
		meta.Error = err.Error()
		g.cache.SaveMetadata(job.BookID, meta)
		return
	default:
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		g.queue.UpdateStatus(job.ID, JobStatusFailed, "Cancelled")
		meta.Status = "failed"
		meta.Error = "Cancelled"
		g.cache.SaveMetadata(job.BookID, meta)
		return
	default:
	}

	// Mark as completed
	meta.Status = "completed"
	meta.CompletedAt = time.Now()
	g.cache.SaveMetadata(job.BookID, meta)

	g.queue.UpdateStatus(job.ID, JobStatusCompleted, "")
	log.Printf("TTS: Book %d completed (%d chunks)", job.BookID, len(chunks))
}

// getTTSBinaryPath finds the sopds-tts binary
func getTTSBinaryPath() string {
	// First, try next to the main executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "sopds-tts")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fall back to PATH
	if path, err := exec.LookPath("sopds-tts"); err == nil {
		return path
	}
	// Default - assume it's in current directory or PATH
	return "sopds-tts"
}

// generateChunk generates audio for a single text chunk using sopds-tts subprocess
// Each chunk runs in a separate process, guaranteeing complete memory release
func (g *Generator) generateChunk(text, modelPath, outputPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	// Run sopds-tts as subprocess
	// Usage: sopds-tts <model_path> <output_path> (text via stdin)
	ttsBinary := getTTSBinaryPath()
	log.Printf("TTS: Spawning subprocess: %s %s %s (text len: %d)", ttsBinary, modelPath, outputPath, len(text))

	cmd := exec.Command(ttsBinary, modelPath, outputPath)
	cmd.Stdin = strings.NewReader(text)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sopds-tts failed: %w, output: %s", err, string(output))
	}
	log.Printf("TTS: Subprocess completed: %s", outputPath)

	// Verify output file was created
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created: %w", err)
	}

	return nil
}
