-- Schema cleanup: rename columns, fix types

-- 1. Rename doublicat -> duplicate_of and add FK constraint
ALTER TABLE books RENAME COLUMN doublicat TO duplicate_of;

-- Add FK constraint (self-reference to original book)
-- Note: 0 means no duplicate, so we need to handle that
UPDATE books SET duplicate_of = NULL WHERE duplicate_of = 0;
ALTER TABLE books
    ALTER COLUMN duplicate_of DROP DEFAULT,
    ADD CONSTRAINT books_duplicate_of_fkey
    FOREIGN KEY (duplicate_of) REFERENCES books(book_id) ON DELETE SET NULL;

-- Update index name
DROP INDEX IF EXISTS idx_books_avail_doublicat;
CREATE INDEX idx_books_avail_duplicate ON books(avail, duplicate_of);

-- 2. Keep docdate as VARCHAR - conversion handled at application level
-- Some FB2 files have invalid dates (like Feb 29 in non-leap years)
-- The domain layer parses dates safely with error handling

-- 3. Convert favorite from INT to BOOLEAN
ALTER TABLE books ADD COLUMN is_favorite BOOLEAN DEFAULT false;
UPDATE books SET is_favorite = (favorite > 0);
ALTER TABLE books DROP COLUMN favorite;
ALTER TABLE books RENAME COLUMN is_favorite TO favorite;
