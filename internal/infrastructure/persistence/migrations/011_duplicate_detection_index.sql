-- Add functional index for faster duplicate detection
-- The LOWER(title) is used because duplicate detection is case-insensitive

-- Index for "strong" mode: title + format + filesize
CREATE INDEX IF NOT EXISTS idx_books_dup_strong
ON books (LOWER(title), format, filesize)
WHERE avail != 0;

-- Index for "normal" mode: just title (authors are joined separately)
CREATE INDEX IF NOT EXISTS idx_books_dup_normal
ON books (LOWER(title))
WHERE avail != 0;
