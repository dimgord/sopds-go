-- +migrate up

-- Index for ZipIsScanned query: SELECT cat_id FROM catalogs WHERE path = $1 AND cat_type = $2
CREATE INDEX IF NOT EXISTS idx_catalogs_path_type ON catalogs(path, cat_type);

-- Composite index for MarkCatalogVerified query: UPDATE books SET avail = $1 WHERE cat_id = $2 AND avail = $3
CREATE INDEX IF NOT EXISTS idx_books_cat_avail ON books(cat_id, avail);

-- Index for FindBook query: SELECT ... FROM books WHERE filename = $1 AND path = $2
CREATE INDEX IF NOT EXISTS idx_books_filename_path ON books(filename, path);

-- +migrate down
DROP INDEX IF EXISTS idx_catalogs_path_type;
DROP INDEX IF EXISTS idx_books_cat_avail;
DROP INDEX IF EXISTS idx_books_filename_path;
