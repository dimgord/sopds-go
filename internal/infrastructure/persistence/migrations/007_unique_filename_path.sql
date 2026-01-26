-- Add unique constraint on (filename, path) for ON CONFLICT support
-- This is needed for upsert operations in book_repository.go

-- First, remove any duplicates (keep the one with lowest book_id)
DELETE FROM books a USING books b
WHERE a.book_id > b.book_id
  AND a.filename = b.filename
  AND a.path = b.path;

-- Create unique index (can be used by ON CONFLICT)
CREATE UNIQUE INDEX IF NOT EXISTS idx_books_filename_path ON books(filename, path);

-- Drop the separate indexes as they're now redundant
DROP INDEX IF EXISTS idx_books_filename;
DROP INDEX IF EXISTS idx_books_path;
