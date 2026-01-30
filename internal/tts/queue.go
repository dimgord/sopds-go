package tts

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the status of a TTS job
type JobStatus string

const (
	JobStatusQueued     JobStatus = "queued"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// Job represents a TTS generation job
type Job struct {
	ID          string    `json:"id"`
	BookID      int64     `json:"book_id"`
	BookTitle   string    `json:"book_title"`
	Voice       string    `json:"voice"`
	Lang        string    `json:"lang"`
	Status      JobStatus `json:"status"`
	Progress    float64   `json:"progress"` // 0.0 - 1.0
	ChunksDone  int       `json:"chunks_done"`
	ChunksTotal int       `json:"chunks_total"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// JobQueue manages TTS generation jobs
type JobQueue struct {
	jobs    map[string]*Job
	byBook  map[int64]string // bookID -> jobID for quick lookup
	pending chan *Job
	mu      sync.RWMutex
}

// NewJobQueue creates a new job queue
func NewJobQueue(bufferSize int) *JobQueue {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &JobQueue{
		jobs:    make(map[string]*Job),
		byBook:  make(map[int64]string),
		pending: make(chan *Job, bufferSize),
	}
}

// Add creates and queues a new job
func (q *JobQueue) Add(bookID int64, bookTitle, voice, lang string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if job already exists for this book
	if existingID, ok := q.byBook[bookID]; ok {
		existing := q.jobs[existingID]
		if existing != nil && (existing.Status == JobStatusQueued || existing.Status == JobStatusProcessing) {
			return existing, nil // Return existing active job
		}
	}

	job := &Job{
		ID:        uuid.New().String(),
		BookID:    bookID,
		BookTitle: bookTitle,
		Voice:     voice,
		Lang:      lang,
		Status:    JobStatusQueued,
		Progress:  0,
		CreatedAt: time.Now(),
	}

	q.jobs[job.ID] = job
	q.byBook[bookID] = job.ID

	// Non-blocking send to pending channel
	select {
	case q.pending <- job:
	default:
		// Queue is full, job will be picked up on next iteration
	}

	return job, nil
}

// Next returns the next pending job (blocking)
func (q *JobQueue) Next() *Job {
	return <-q.pending
}

// NextNonBlocking returns the next pending job or nil
func (q *JobQueue) NextNonBlocking() *Job {
	select {
	case job := <-q.pending:
		return job
	default:
		return nil
	}
}

// Get returns a job by ID
func (q *JobQueue) Get(id string) *Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.jobs[id]
}

// GetByBook returns the job for a book
func (q *JobQueue) GetByBook(bookID int64) *Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if jobID, ok := q.byBook[bookID]; ok {
		return q.jobs[jobID]
	}
	return nil
}

// UpdateStatus updates a job's status
func (q *JobQueue) UpdateStatus(id string, status JobStatus, err string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.jobs[id]
	if !ok {
		return
	}

	job.Status = status
	job.Error = err

	switch status {
	case JobStatusProcessing:
		job.StartedAt = time.Now()
	case JobStatusCompleted, JobStatusFailed:
		job.CompletedAt = time.Now()
		if status == JobStatusCompleted {
			job.Progress = 1.0
		}
	}
}

// UpdateProgress updates a job's progress
func (q *JobQueue) UpdateProgress(id string, chunksDone, chunksTotal int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.jobs[id]
	if !ok {
		return
	}

	job.ChunksDone = chunksDone
	job.ChunksTotal = chunksTotal
	if chunksTotal > 0 {
		job.Progress = float64(chunksDone) / float64(chunksTotal)
	}
}

// ListActive returns all active (queued or processing) jobs
func (q *JobQueue) ListActive() []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var active []*Job
	for _, job := range q.jobs {
		if job.Status == JobStatusQueued || job.Status == JobStatusProcessing {
			active = append(active, job)
		}
	}
	return active
}

// ListAll returns all jobs
func (q *JobQueue) ListAll() []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	all := make([]*Job, 0, len(q.jobs))
	for _, job := range q.jobs {
		all = append(all, job)
	}
	return all
}

// Remove removes a job
func (q *JobQueue) Remove(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if job, ok := q.jobs[id]; ok {
		delete(q.byBook, job.BookID)
		delete(q.jobs, id)
	}
}

// CleanOld removes completed/failed jobs older than maxAge
func (q *JobQueue) CleanOld(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	for id, job := range q.jobs {
		if (job.Status == JobStatusCompleted || job.Status == JobStatusFailed) &&
			job.CompletedAt.Before(cutoff) {
			delete(q.byBook, job.BookID)
			delete(q.jobs, id)
		}
	}
}

// PendingCount returns the number of pending jobs
func (q *JobQueue) PendingCount() int {
	return len(q.pending)
}
