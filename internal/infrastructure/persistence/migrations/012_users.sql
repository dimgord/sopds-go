-- Users table for authentication
CREATE TABLE IF NOT EXISTS users (
    user_id SERIAL PRIMARY KEY,
    username VARCHAR(30) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verify_token VARCHAR(64),
    verify_token_expires TIMESTAMP,
    reset_token VARCHAR(64),
    reset_token_expires TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_login TIMESTAMP
);

-- Index for faster lookups
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_verify_token ON users(verify_token) WHERE verify_token IS NOT NULL;
CREATE INDEX idx_users_reset_token ON users(reset_token) WHERE reset_token IS NOT NULL;

-- Add user_id to bookshelf (nullable for anonymous users)
ALTER TABLE bookshelf ADD COLUMN IF NOT EXISTS user_id INTEGER REFERENCES users(user_id) ON DELETE CASCADE;

-- Index for user's bookshelf
CREATE INDEX IF NOT EXISTS idx_bookshelf_user_id ON bookshelf(user_id) WHERE user_id IS NOT NULL;
