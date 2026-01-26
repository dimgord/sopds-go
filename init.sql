-- SOPDS PostgreSQL Database Initialization Script
-- Run as postgres superuser: psql -U postgres -f init.sql

-- Create database
CREATE DATABASE sopds WITH ENCODING 'UTF8' LC_COLLATE 'en_US.UTF-8' LC_CTYPE 'en_US.UTF-8' TEMPLATE template0;

-- Create user
CREATE USER sopds WITH PASSWORD 'sopds';

-- Connect to sopds database
\c sopds

-- Grant schema permissions (required for PostgreSQL 15+)
GRANT ALL ON SCHEMA public TO sopds;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO sopds;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO sopds;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO sopds;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO sopds;

-- Grant database permissions
GRANT ALL PRIVILEGES ON DATABASE sopds TO sopds;

\echo 'Database sopds created successfully!'
\echo 'Now run: ./sopds migrate'
