-- Migration: 014_tts_requests.sql
-- On-demand TTS: replace piper auto-generation with a request/counter model (piper sounds mechanical even
-- on "high"; real audio is generated in batch with F5-TTS and linked back via tts_audio_id).
--
-- Two columns on books:
--   tts_requests  cached count of UNIQUE requesters (deduped via tts_request_log)
--   tts_audio_id  NULL until fulfilled, then the book_id of the generated audiobook (→ /audio/{id})

ALTER TABLE books ADD COLUMN IF NOT EXISTS tts_requests INTEGER NOT NULL DEFAULT 0;
ALTER TABLE books ADD COLUMN IF NOT EXISTS tts_audio_id BIGINT DEFAULT NULL;

-- One row per (book, requester) so each user/guest is counted once. requester is "u:<username>" for a
-- logged-in user or "a:<anon-id>" for a guest (the existing anonymous-session cookie). books.tts_requests
-- is the denormalized count for cheap display.
CREATE TABLE IF NOT EXISTS tts_request_log (
    book_id    BIGINT      NOT NULL,
    requester  TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (book_id, requester)
);
CREATE INDEX IF NOT EXISTS idx_tts_request_log_book ON tts_request_log(book_id);

-- Pending = requested but not yet fulfilled; drives the post-scan report + the counter in the button.
CREATE INDEX IF NOT EXISTS idx_books_tts_pending ON books(tts_requests) WHERE tts_audio_id IS NULL AND tts_requests > 0;
