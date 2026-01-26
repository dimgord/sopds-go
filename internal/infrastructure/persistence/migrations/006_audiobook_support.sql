-- Migration: 006_audiobook_support.sql
-- Adds support for audiobooks: duration, bitrate, chapters, narrator role

-- Audio-specific fields for books table
ALTER TABLE books ADD COLUMN IF NOT EXISTS duration_seconds INTEGER DEFAULT 0;
ALTER TABLE books ADD COLUMN IF NOT EXISTS bitrate INTEGER DEFAULT 0;
ALTER TABLE books ADD COLUMN IF NOT EXISTS is_audiobook BOOLEAN DEFAULT FALSE;
ALTER TABLE books ADD COLUMN IF NOT EXISTS track_count INTEGER DEFAULT 0;
ALTER TABLE books ADD COLUMN IF NOT EXISTS chapters JSONB DEFAULT NULL;

-- Role column for author/narrator distinction in junction table
ALTER TABLE bauthors ADD COLUMN IF NOT EXISTS role VARCHAR(16) DEFAULT 'author';

-- Index for audiobook queries
CREATE INDEX IF NOT EXISTS idx_books_is_audiobook ON books(is_audiobook) WHERE is_audiobook = TRUE;

-- Index for duration sorting/filtering
CREATE INDEX IF NOT EXISTS idx_books_duration ON books(duration_seconds) WHERE duration_seconds > 0;

-- Index for role filtering (find all narrators)
CREATE INDEX IF NOT EXISTS idx_bauthors_role ON bauthors(role);
