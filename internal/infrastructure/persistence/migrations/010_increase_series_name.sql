-- Increase series name column from 64 to 128 characters
ALTER TABLE series ALTER COLUMN ser TYPE VARCHAR(128);
