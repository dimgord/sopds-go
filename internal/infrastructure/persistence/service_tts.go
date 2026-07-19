package persistence

import (
	"context"
	"fmt"
)

// TTSState is the on-demand-audio state of one book, for the "Listen" button.
type TTSState struct {
	Requests int    // count of unique requesters (0 until someone asks)
	AudioID  *int64 // nil until fulfilled; then the book_id of the generated audiobook (→ /audio/{id})
}

// TTSPending is a book that was requested but not yet fulfilled (for the post-scan report).
type TTSPending struct {
	BookID   int64
	Title    string
	Requests int
}

// RecordTTSRequest records a unique (book, requester) audio request. requester is "u:<username>" for a
// logged-in user or "a:<anon-id>" for a guest. It returns the book's new request count and whether this
// was a fresh request (created=false means that requester had already asked — the count is unchanged).
func (s *Service) RecordTTSRequest(ctx context.Context, bookID int64, requester string) (count int, created bool, err error) {
	db := s.GORM().WithContext(ctx)
	res := db.Exec(
		`INSERT INTO tts_request_log (book_id, requester) VALUES (?, ?) ON CONFLICT (book_id, requester) DO NOTHING`,
		bookID, requester)
	if res.Error != nil {
		return 0, false, fmt.Errorf("record tts request: %w", res.Error)
	}
	created = res.RowsAffected > 0
	if created {
		if err := db.Exec(`UPDATE books SET tts_requests = tts_requests + 1 WHERE book_id = ?`, bookID).Error; err != nil {
			return 0, false, fmt.Errorf("bump tts_requests: %w", err)
		}
	}
	if err := db.Raw(`SELECT tts_requests FROM books WHERE book_id = ?`, bookID).Scan(&count).Error; err != nil {
		return count, created, fmt.Errorf("read tts_requests: %w", err)
	}
	return count, created, nil
}

// TTSStatesFor returns the audio state for those of the given books that have any (requested or fulfilled),
// keyed by book_id. Books with no requests and no audio are omitted (so the map is small).
func (s *Service) TTSStatesFor(ctx context.Context, bookIDs []int64) (map[int64]TTSState, error) {
	out := make(map[int64]TTSState)
	if len(bookIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		BookID   int64
		Requests int
		AudioID  *int64
	}
	if err := s.GORM().WithContext(ctx).Raw(
		`SELECT book_id, tts_requests AS requests, tts_audio_id AS audio_id FROM books
		 WHERE book_id IN ? AND (tts_requests > 0 OR tts_audio_id IS NOT NULL)`, bookIDs).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("tts states: %w", err)
	}
	for _, r := range rows {
		out[r.BookID] = TTSState{Requests: r.Requests, AudioID: r.AudioID}
	}
	return out, nil
}

// TTSBookRow is one book's full on-demand-audio state (for the `tts-list` overview).
type TTSBookRow struct {
	BookID     int64
	Title      string
	Requests   int
	AudioID    *int64
	AudioTitle *string // title of the linked audiobook (nil when not fulfilled or missing)
}

// ListTTSBooks returns every book that has any audio requests or a linked audiobook (fulfilled first),
// joining in the audiobook's title so a link can be eyeballed.
func (s *Service) ListTTSBooks(ctx context.Context) ([]TTSBookRow, error) {
	var out []TTSBookRow
	if err := s.GORM().WithContext(ctx).Raw(
		`SELECT b.book_id, b.title, b.tts_requests AS requests, b.tts_audio_id AS audio_id, a.title AS audio_title
		 FROM books b LEFT JOIN books a ON a.book_id = b.tts_audio_id
		 WHERE b.tts_requests > 0 OR b.tts_audio_id IS NOT NULL
		 ORDER BY (b.tts_audio_id IS NOT NULL) DESC, b.tts_requests DESC, b.book_id`).
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list tts books: %w", err)
	}
	return out, nil
}

// AudiobookRow is one audiobook, for the `audio-list` overview (find a freshly-scanned book's id to link).
type AudiobookRow struct {
	BookID       int64
	Title        string
	TrackCount   int
	RegisterDate string
}

// ListAudiobooks returns audiobooks newest-first (limit<=0 ⇒ all), so a just-added one's book_id can be
// found to pass to `tts-link`.
func (s *Service) ListAudiobooks(ctx context.Context, limit int) ([]AudiobookRow, error) {
	var out []AudiobookRow
	q := s.GORM().WithContext(ctx).Table("books").
		Select("book_id, title, track_count, to_char(registerdate, 'YYYY-MM-DD HH24:MI') AS register_date").
		Where("is_audiobook = true").
		Order("registerdate DESC, book_id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list audiobooks: %w", err)
	}
	return out, nil
}

// SetTTSAudioID links a text book to its generated audiobook (fulfillment), or clears the link when
// audioID is nil (back to request mode). The Listen button then points at /audio/<audioID>.
func (s *Service) SetTTSAudioID(ctx context.Context, bookID int64, audioID *int64) error {
	if err := s.GORM().WithContext(ctx).Exec(
		`UPDATE books SET tts_audio_id = ? WHERE book_id = ?`, audioID, bookID).Error; err != nil {
		return fmt.Errorf("set tts_audio_id: %w", err)
	}
	return nil
}

// PendingTTSRequests lists books requested but not yet fulfilled, most-requested first (for the scan report).
func (s *Service) PendingTTSRequests(ctx context.Context) ([]TTSPending, error) {
	var out []TTSPending
	if err := s.GORM().WithContext(ctx).Raw(
		`SELECT book_id, title, tts_requests AS requests FROM books
		 WHERE tts_audio_id IS NULL AND tts_requests > 0 ORDER BY tts_requests DESC, book_id`).
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("pending tts requests: %w", err)
	}
	return out, nil
}
