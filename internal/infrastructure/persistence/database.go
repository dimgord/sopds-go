package persistence

import (
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dimgord/sopds-go/internal/config"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
}

// NewDB creates a new GORM database connection
func NewDB(cfg *config.DatabaseConfig) (*DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
	)

	// Configure GORM logger
	gormLogger := logger.New(
		log.Default(),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormLogger,
		// Don't auto-migrate - we use explicit SQL migrations
		DisableForeignKeyConstraintWhenMigrating: true,
		// Improve performance
		PrepareStmt:                              true,
		SkipDefaultTransaction:                   true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Transaction executes a function within a transaction
func (db *DB) Transaction(fn func(tx *gorm.DB) error) error {
	return db.DB.Transaction(fn)
}

// SetAsyncCommit disables synchronous_commit for this session.
// This speeds up bulk operations by not waiting for disk fsync on each commit.
// Data can be lost on crash but is recoverable by re-scanning.
func (db *DB) SetAsyncCommit() error {
	return db.Exec("SET synchronous_commit = off").Error
}

// SetSyncCommit re-enables synchronous_commit for this session.
func (db *DB) SetSyncCommit() error {
	return db.Exec("SET synchronous_commit = on").Error
}

// SetLogLevel changes the log level dynamically
func (db *DB) SetLogLevel(level logger.LogLevel) {
	db.DB.Logger = db.DB.Logger.LogMode(level)
}

// EnableDebug enables detailed query logging
func (db *DB) EnableDebug() {
	db.SetLogLevel(logger.Info)
}

// DisableDebug disables detailed query logging
func (db *DB) DisableDebug() {
	db.SetLogLevel(logger.Warn)
}

// Migrate runs all pending database migrations
func (db *DB) Migrate() error {
	// Create migrations table if not exists
	err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Error
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get list of migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrations = append(migrations, entry.Name())
		}
	}
	sort.Strings(migrations)

	// Apply each migration
	for _, migration := range migrations {
		// Check if already applied
		var count int64
		err := db.Raw("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration).Scan(&count).Error
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", migration, err)
		}

		if count > 0 {
			continue // Already applied
		}

		// Read and execute migration
		content, err := migrationsFS.ReadFile("migrations/" + migration)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", migration, err)
		}

		// Use raw sql.DB to execute multi-statement migrations
		sqlDB, err := db.DB.DB()
		if err != nil {
			return fmt.Errorf("failed to get sql.DB: %w", err)
		}
		_, err = sqlDB.Exec(string(content))
		if err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration, err)
		}

		// Record migration
		err = db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migration).Error
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", migration, err)
		}

		fmt.Printf("Applied migration: %s\n", migration)
	}

	return nil
}
