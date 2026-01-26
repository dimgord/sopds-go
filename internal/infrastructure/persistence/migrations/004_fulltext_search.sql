-- Full-text search for books
-- Uses 'simple' configuration for multi-language support

-- Add tsvector column for search
ALTER TABLE books ADD COLUMN IF NOT EXISTS search_vector tsvector;

-- Create GIN index for fast full-text search
CREATE INDEX IF NOT EXISTS idx_books_search ON books USING GIN(search_vector);

-- Function to update search vector
CREATE OR REPLACE FUNCTION books_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('simple', COALESCE(NEW.title, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(NEW.annotation, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update search vector on insert/update
DROP TRIGGER IF EXISTS books_search_vector_trigger ON books;
CREATE TRIGGER books_search_vector_trigger
    BEFORE INSERT OR UPDATE OF title, annotation ON books
    FOR EACH ROW
    EXECUTE FUNCTION books_search_vector_update();

-- Populate search_vector for existing books
UPDATE books SET search_vector =
    setweight(to_tsvector('simple', COALESCE(title, '')), 'A') ||
    setweight(to_tsvector('simple', COALESCE(annotation, '')), 'B')
WHERE search_vector IS NULL;

-- Create index on author names for author search
CREATE INDEX IF NOT EXISTS idx_authors_search ON authors
USING GIN(to_tsvector('simple', COALESCE(last_name, '') || ' ' || COALESCE(first_name, '')));
