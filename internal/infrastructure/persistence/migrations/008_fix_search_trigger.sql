-- Fix search vector trigger to only fire on title/annotation changes
-- This prevents slow updates when only changing avail or other fields

-- Drop existing trigger (may be missing UPDATE OF condition)
DROP TRIGGER IF EXISTS books_search_vector_trigger ON books;

-- Recreate trigger with UPDATE OF condition
CREATE TRIGGER books_search_vector_trigger
    BEFORE INSERT OR UPDATE OF title, annotation ON books
    FOR EACH ROW
    EXECUTE FUNCTION books_search_vector_update();
