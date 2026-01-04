-- SOPDS Initial Schema for PostgreSQL
-- Migrated from MySQL tables.sql

-- Books table
CREATE TABLE IF NOT EXISTS books (
    book_id BIGSERIAL PRIMARY KEY,
    filename VARCHAR(256) NOT NULL,
    path VARCHAR(1024) NOT NULL,
    filesize BIGINT DEFAULT 0,
    format VARCHAR(8),
    cat_id BIGINT,
    cat_type INTEGER DEFAULT 0,
    registerdate TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    docdate VARCHAR(20),
    favorite INTEGER DEFAULT 0,
    lang VARCHAR(16),
    title VARCHAR(256),
    annotation TEXT,
    cover VARCHAR(32),
    cover_type VARCHAR(32),
    doublicat BIGINT DEFAULT 0,
    avail INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_books_filename ON books(filename);
CREATE INDEX IF NOT EXISTS idx_books_title_format_filesize ON books(title, format, filesize);
CREATE INDEX IF NOT EXISTS idx_books_path ON books(path);
CREATE INDEX IF NOT EXISTS idx_books_cat_id ON books(cat_id);
CREATE INDEX IF NOT EXISTS idx_books_avail_doublicat ON books(avail, doublicat);
CREATE INDEX IF NOT EXISTS idx_books_registerdate ON books(registerdate);
CREATE INDEX IF NOT EXISTS idx_books_lang ON books(lang);

-- Catalogs table (directory tree)
CREATE TABLE IF NOT EXISTS catalogs (
    cat_id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT REFERENCES catalogs(cat_id) ON DELETE CASCADE,
    cat_name VARCHAR(64) NOT NULL,
    path VARCHAR(1024) NOT NULL,
    cat_type INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_catalogs_name_path ON catalogs(cat_name, path);
CREATE INDEX IF NOT EXISTS idx_catalogs_parent ON catalogs(parent_id);

-- Authors table
CREATE TABLE IF NOT EXISTS authors (
    author_id BIGSERIAL PRIMARY KEY,
    first_name VARCHAR(64) NOT NULL DEFAULT '',
    last_name VARCHAR(64) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_authors_name ON authors(last_name, first_name);

-- Insert unknown author
INSERT INTO authors (author_id, first_name, last_name)
VALUES (1, '', 'Неизвестный Автор')
ON CONFLICT DO NOTHING;

-- Book-Author relationship
CREATE TABLE IF NOT EXISTS bauthors (
    book_id BIGINT NOT NULL REFERENCES books(book_id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES authors(author_id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, author_id)
);

CREATE INDEX IF NOT EXISTS idx_bauthors_author ON bauthors(author_id);

-- Genres table
CREATE TABLE IF NOT EXISTS genres (
    genre_id BIGSERIAL PRIMARY KEY,
    genre VARCHAR(32) NOT NULL UNIQUE,
    section VARCHAR(64) NOT NULL DEFAULT '',
    subsection VARCHAR(100) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_genres_genre ON genres(genre);

-- Book-Genre relationship
CREATE TABLE IF NOT EXISTS bgenres (
    book_id BIGINT NOT NULL REFERENCES books(book_id) ON DELETE CASCADE,
    genre_id BIGINT NOT NULL REFERENCES genres(genre_id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, genre_id)
);

CREATE INDEX IF NOT EXISTS idx_bgenres_genre ON bgenres(genre_id);

-- Series table
CREATE TABLE IF NOT EXISTS series (
    ser_id BIGSERIAL PRIMARY KEY,
    ser VARCHAR(64) NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_series_ser ON series(ser);

-- Book-Series relationship
CREATE TABLE IF NOT EXISTS bseries (
    book_id BIGINT NOT NULL REFERENCES books(book_id) ON DELETE CASCADE,
    ser_id BIGINT NOT NULL REFERENCES series(ser_id) ON DELETE CASCADE,
    ser_no SMALLINT DEFAULT 0,
    PRIMARY KEY (book_id, ser_id)
);

CREATE INDEX IF NOT EXISTS idx_bseries_ser ON bseries(ser_id);

-- User bookshelf
CREATE TABLE IF NOT EXISTS bookshelf (
    user_name VARCHAR(32) NOT NULL,
    book_id BIGINT NOT NULL REFERENCES books(book_id) ON DELETE CASCADE,
    readtime TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_name, book_id)
);

CREATE INDEX IF NOT EXISTS idx_bookshelf_user_time ON bookshelf(user_name, readtime);

-- Database version tracking
CREATE TABLE IF NOT EXISTS dbver (
    ver VARCHAR(5) PRIMARY KEY
);

INSERT INTO dbver (ver) VALUES ('1.0.0') ON CONFLICT DO NOTHING;
