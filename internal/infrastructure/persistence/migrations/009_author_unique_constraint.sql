-- Add unique constraint on authors(first_name, last_name)
-- This enables ON CONFLICT handling for concurrent author creation

-- Drop existing index if it exists (non-unique)
DROP INDEX IF EXISTS idx_authors_name;

-- Create unique constraint (use DO block for idempotency)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'authors_name_unique'
    ) THEN
        ALTER TABLE authors ADD CONSTRAINT authors_name_unique UNIQUE (first_name, last_name);
    END IF;
END$$;

-- Fix sequence to start after the max existing author_id
-- (migration 001 inserts author_id=1 explicitly without updating sequence)
-- Using DO block to ensure setval is executed
DO $$
BEGIN
    PERFORM setval('authors_author_id_seq', (SELECT COALESCE(MAX(author_id), 0) FROM authors), true);
END$$;
