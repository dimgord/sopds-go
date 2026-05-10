package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/dimgord/sopds-go/internal/config"
	"github.com/dimgord/sopds-go/internal/infrastructure/persistence"
	"github.com/dimgord/sopds-go/internal/scanner"
	"github.com/dimgord/sopds-go/internal/server"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	logFile *os.File

	// version is overridden at link time by GoReleaser via
	// `-ldflags "-X main.version=<tag>"`. "dev" is the default for
	// `go run`/`go install` from a source tree.
	version = "dev"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "sopds",
		Short: "Simple OPDS Catalog Server",
		Long:  `SOPDS is an OPDS catalog server for managing and serving e-book collections.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			// Set up file logging if configured
			if err := setupLogging(); err != nil {
				log.Printf("Warning: failed to set up file logging: %v", err)
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file path")

	// Start command
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the SOPDS server",
		RunE:  runStart,
	}

	// Stop command
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the SOPDS server",
		RunE:  runStop,
	}

	// Status command
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check server status",
		RunE:  runStatus,
	}

	// Scan command
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Run a single library scan",
		RunE:  runScan,
	}

	// Migrate command
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE:  runMigrate,
	}

	// Init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new configuration file",
		RunE:  runInit,
	}

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("SOPDS %s (Go rewrite)\n", version)
		},
	}

	// Import from MySQL command
	importCmd := &cobra.Command{
		Use:   "import-mysql",
		Short: "Import data from MySQL SOPDS database",
		RunE:  runImportMySQL,
	}
	importCmd.Flags().String("mysql-host", "localhost", "MySQL host")
	importCmd.Flags().Int("mysql-port", 3306, "MySQL port")
	importCmd.Flags().String("mysql-user", "sopds", "MySQL user")
	importCmd.Flags().String("mysql-pass", "sopds", "MySQL password")
	importCmd.Flags().String("mysql-db", "sopds", "MySQL database name")
	importCmd.Flags().Bool("clear", false, "Clear PostgreSQL tables before import")

	rootCmd.AddCommand(startCmd, stopCmd, statusCmd, scanCmd, migrateCmd, initCmd, versionCmd, importCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runStart(cmd *cobra.Command, args []string) error {
	log.Println("Starting SOPDS server...")

	// Connect to database using GORM
	gormDB, err := persistence.NewDB(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create service wrapping repositories
	svc := persistence.NewService(gormDB)
	defer svc.Close()

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Write PID file
	if cfg.Scanner.PIDFile != "" {
		if err := writePIDFile(cfg.Scanner.PIDFile); err != nil {
			log.Printf("Warning: failed to write PID file: %v", err)
		}
		defer os.Remove(cfg.Scanner.PIDFile)
	}

	// Start scanner if scheduled
	if cfg.Scanner.OnStart || cfg.Scanner.Schedule != "" {
		sc := scanner.New(cfg, svc)
		go sc.Run(ctx)
	}

	// Start HTTP server
	srv := server.New(cfg, svc)
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
			cancel()
		}
	}()

	log.Printf("SOPDS server running on %s:%d", cfg.Server.Bind, cfg.Server.Port)
	log.Printf("OPDS endpoint: http://%s:%d%s/", cfg.Server.Bind, cfg.Server.Port, cfg.Server.OPDSPrefix)
	log.Printf("Web endpoint: http://%s:%d%s/", cfg.Server.Bind, cfg.Server.Port, cfg.Server.WebPrefix)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down...")

	// Graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	pidFile := cfg.Scanner.PIDFile
	if pidFile == "" {
		pidFile = "/tmp/sopds.pid"
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	log.Printf("Sent SIGTERM to process %d", pid)
	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	pidFile := cfg.Scanner.PIDFile
	if pidFile == "" {
		pidFile = "/tmp/sopds.pid"
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Printf("SOPDS server is not running (no PID file at %s)\n", pidFile)
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fmt.Printf("SOPDS server status unknown (invalid PID file content: %q)\n", string(data))
		return nil
	}

	// On Linux/macOS, os.FindProcess always succeeds (it just constructs
	// a Process struct with the given PID). The actual liveness check is
	// the signal-0 below, whose error tells us why we can't confirm.
	process, _ := os.FindProcess(pid)
	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		fmt.Printf("SOPDS server is running (PID: %d)\n", pid)
	case errors.Is(err, syscall.ESRCH):
		fmt.Printf("SOPDS server is not running (stale PID %d in %s)\n", pid, pidFile)
	case errors.Is(err, syscall.EPERM):
		// Process exists but is owned by another user — typical when
		// status is invoked as a different user than the one that
		// started the daemon (e.g. dimgord asking about a sopds-user
		// systemd service).
		fmt.Printf("SOPDS server is running (PID: %d) — owned by another user; run as that user or root to control it\n", pid)
	default:
		fmt.Printf("SOPDS server status check failed: %v\n", err)
	}
	return nil
}

func runScan(cmd *cobra.Command, args []string) error {
	gormDB, err := persistence.NewDB(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	svc := persistence.NewService(gormDB)
	defer svc.Close()

	sc := scanner.New(cfg, svc)
	ctx := context.Background()

	// Set up progress callback
	sc.SetProgressCallback(func(info scanner.ProgressInfo) {
		printProgress(info)
	})

	// Set up confirmation callback for destructive operations
	sc.SetConfirmCallback(func(message string) bool {
		fmt.Println()
		fmt.Println(message)
		fmt.Print("\nProceed? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return false
		}
		response = strings.TrimSpace(strings.ToLower(response))
		return response == "y" || response == "yes"
	})

	if err := sc.ScanAll(ctx); err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	fmt.Println() // New line after progress
	log.Println("Scan completed")
	return nil
}

func printProgress(info scanner.ProgressInfo) {
	switch info.Phase {
	case "counting":
		fmt.Printf("\rCounting files...                                                                ")
	case "loading":
		fmt.Printf("\rLoading catalogs from database...                                                ")
	case "duplicates":
		fmt.Printf("\rDetecting duplicates...                                                          ")
	case "scanning":
		pct := 0.0
		if info.TotalFiles > 0 {
			pct = float64(info.ProcessedFiles) / float64(info.TotalFiles) * 100
		}

		// Build progress bar
		barWidth := 30
		filled := int(pct / 100 * float64(barWidth))
		bar := ""
		for i := 0; i < barWidth; i++ {
			if i < filled {
				bar += "="
			} else if i == filled {
				bar += ">"
			} else {
				bar += " "
			}
		}

		// Format ETA
		etaStr := "calculating..."
		if info.ETA > 0 {
			etaStr = formatDuration(info.ETA)
		}

		// Format rate
		rateStr := ""
		if info.Rate > 0 {
			rateStr = fmt.Sprintf("%.1f files/s", info.Rate)
		}

		fmt.Printf("\r[%s] %5.1f%% | %d/%d | +%d =%d | %s | ETA: %s   ",
			bar, pct, info.ProcessedFiles, info.TotalFiles,
			info.BooksAdded, info.BooksSkipped, rateStr, etaStr)
	case "cleanup":
		fmt.Printf("\rCleaning up...                                                                  ")
	case "done":
		pct := 100.0
		barWidth := 30
		bar := ""
		for i := 0; i < barWidth; i++ {
			bar += "="
		}
		fmt.Printf("\r[%s] %5.1f%% | %d/%d | +%d =%d | Done in %s   ",
			bar, pct, info.ProcessedFiles, info.TotalFiles,
			info.BooksAdded, info.BooksSkipped, formatDuration(info.Elapsed))
	}
	os.Stdout.Sync()
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", hours, mins)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	log.Println("Running database migrations...")

	gormDB, err := persistence.NewDB(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer gormDB.Close()

	if err := gormDB.Migrate(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	log.Println("Migrations completed")
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	path := "config.yaml"
	if cfgFile != "" {
		path = cfgFile
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists: %s", path)
	}

	cfg := config.DefaultConfig()
	if err := cfg.Save(path); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("Created config file: %s", path)
	log.Println("Please edit the configuration before starting the server.")
	return nil
}

func writePIDFile(path string) error {
	pid := os.Getpid()
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func setupLogging() error {
	if cfg.Logging.File == "" {
		return nil
	}

	maxSize := cfg.Logging.MaxSize
	if maxSize <= 0 {
		maxSize = 10 // default 10MB
	}
	maxBackups := cfg.Logging.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 3
	}

	writer := &rollingLogWriter{
		filename:   cfg.Logging.File,
		maxSize:    int64(maxSize) * 1024 * 1024, // convert MB to bytes
		maxBackups: maxBackups,
	}

	if err := writer.open(); err != nil {
		return fmt.Errorf("cannot open log file %s: %w", cfg.Logging.File, err)
	}

	log.SetOutput(writer)
	log.Printf("Logging to file: %s (max %dMB, %d backups)", cfg.Logging.File, maxSize, maxBackups)
	return nil
}

// rollingLogWriter implements io.Writer with log rotation
type rollingLogWriter struct {
	filename   string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
}

func (w *rollingLogWriter) open() error {
	f, err := os.OpenFile(w.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w.file = f

	// Get current file size
	info, err := f.Stat()
	if err != nil {
		return err
	}
	w.size = info.Size()
	return nil
}

func (w *rollingLogWriter) Write(p []byte) (n int, err error) {
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			// If rotation fails, continue writing to current file
			log.SetOutput(os.Stderr)
			log.Printf("Warning: log rotation failed: %v", err)
			log.SetOutput(w)
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rollingLogWriter) rotate() error {
	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Remove oldest backup if at max
	oldestBackup := fmt.Sprintf("%s.%d", w.filename, w.maxBackups)
	os.Remove(oldestBackup)

	// Shift existing backups
	for i := w.maxBackups - 1; i >= 1; i-- {
		oldName := fmt.Sprintf("%s.%d", w.filename, i)
		newName := fmt.Sprintf("%s.%d", w.filename, i+1)
		os.Rename(oldName, newName)
	}

	// Rename current log to .1
	if err := os.Rename(w.filename, w.filename+".1"); err != nil {
		// If rename fails, try to reopen original
		return w.open()
	}

	// Create new log file
	w.size = 0
	return w.open()
}

func runImportMySQL(cmd *cobra.Command, args []string) error {
	mysqlHost, _ := cmd.Flags().GetString("mysql-host")
	mysqlPort, _ := cmd.Flags().GetInt("mysql-port")
	mysqlUser, _ := cmd.Flags().GetString("mysql-user")
	mysqlPass, _ := cmd.Flags().GetString("mysql-pass")
	mysqlDB, _ := cmd.Flags().GetString("mysql-db")
	clearTables, _ := cmd.Flags().GetBool("clear")

	mysqlDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&readTimeout=0&writeTimeout=0",
		mysqlUser, mysqlPass, mysqlHost, mysqlPort, mysqlDB)

	log.Println("Importing from MySQL database...")
	log.Printf("MySQL: %s@%s:%d/%s", mysqlUser, mysqlHost, mysqlPort, mysqlDB)

	// Connect to PostgreSQL using GORM
	pgDB, err := persistence.NewDB(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	// Clear tables if requested
	if clearTables {
		log.Println("Clearing PostgreSQL tables...")
		tables := []string{"bookshelf", "bseries", "bgenres", "bauthors", "books", "catalogs", "series", "genres", "authors"}
		for _, t := range tables {
			if err := pgDB.Exec("DELETE FROM " + t).Error; err != nil {
				log.Printf("Warning: failed to clear %s: %v", t, err)
			}
		}
	}

	// Run migration
	if err := migrateFromMySQL(mysqlDSN, pgDB); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	log.Println("Import completed successfully!")
	return nil
}

func migrateFromMySQL(mysqlDSN string, pgDB *persistence.DB) error {
	// Import MySQL driver
	mysql, err := openMySQL(mysqlDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	defer mysql.Close()

	ctx := context.Background()

	// Drop foreign key constraints for faster import
	log.Println("Dropping foreign key constraints...")
	fkConstraints := []string{
		"ALTER TABLE bauthors DROP CONSTRAINT IF EXISTS bauthors_book_id_fkey",
		"ALTER TABLE bauthors DROP CONSTRAINT IF EXISTS bauthors_author_id_fkey",
		"ALTER TABLE bgenres DROP CONSTRAINT IF EXISTS bgenres_book_id_fkey",
		"ALTER TABLE bgenres DROP CONSTRAINT IF EXISTS bgenres_genre_id_fkey",
		"ALTER TABLE bseries DROP CONSTRAINT IF EXISTS bseries_book_id_fkey",
		"ALTER TABLE bseries DROP CONSTRAINT IF EXISTS bseries_ser_id_fkey",
		"ALTER TABLE bookshelf DROP CONSTRAINT IF EXISTS bookshelf_book_id_fkey",
	}
	for _, stmt := range fkConstraints {
		pgDB.Exec(stmt)
	}

	// Migrate tables in order (respecting foreign keys)
	tables := []struct {
		name    string
		migrate func(context.Context, *sqlDB, *persistence.DB) (int64, error)
	}{
		{"authors", migrateAuthors},
		{"genres", migrateGenres},
		{"series", migrateSeries},
		{"catalogs", migrateCatalogs},
		{"books", migrateBooks},
		{"bauthors", migrateBauthors},
		{"bgenres", migrateBgenres},
		{"bseries", migrateBseries},
		{"bookshelf", migrateBookshelf},
	}

	for _, t := range tables {
		log.Printf("Migrating %s...", t.name)
		count, err := t.migrate(ctx, mysql, pgDB)
		if err != nil {
			log.Printf("Warning: failed to migrate %s: %v", t.name, err)
		} else {
			log.Printf("  Migrated %d %s", count, t.name)
		}
	}

	// Recreate foreign key constraints
	log.Println("Recreating foreign key constraints...")
	fkRecreate := []string{
		"ALTER TABLE bauthors ADD CONSTRAINT bauthors_book_id_fkey FOREIGN KEY (book_id) REFERENCES books(book_id) ON DELETE CASCADE",
		"ALTER TABLE bauthors ADD CONSTRAINT bauthors_author_id_fkey FOREIGN KEY (author_id) REFERENCES authors(author_id) ON DELETE CASCADE",
		"ALTER TABLE bgenres ADD CONSTRAINT bgenres_book_id_fkey FOREIGN KEY (book_id) REFERENCES books(book_id) ON DELETE CASCADE",
		"ALTER TABLE bgenres ADD CONSTRAINT bgenres_genre_id_fkey FOREIGN KEY (genre_id) REFERENCES genres(genre_id) ON DELETE CASCADE",
		"ALTER TABLE bseries ADD CONSTRAINT bseries_book_id_fkey FOREIGN KEY (book_id) REFERENCES books(book_id) ON DELETE CASCADE",
		"ALTER TABLE bseries ADD CONSTRAINT bseries_ser_id_fkey FOREIGN KEY (ser_id) REFERENCES series(ser_id) ON DELETE CASCADE",
		"ALTER TABLE bookshelf ADD CONSTRAINT bookshelf_book_id_fkey FOREIGN KEY (book_id) REFERENCES books(book_id) ON DELETE CASCADE",
	}
	for _, stmt := range fkRecreate {
		if err := pgDB.Exec(stmt).Error; err != nil {
			log.Printf("Warning: %v", err)
		}
	}

	// Reset sequences to max IDs
	log.Println("Resetting sequences...")
	if err := resetSequences(pgDB); err != nil {
		log.Printf("Warning: failed to reset sequences: %v", err)
	}

	return nil
}

// sqlDB is a minimal interface for database operations
type sqlDB struct {
	db *sql.DB
}

func openMySQL(dsn string) (*sqlDB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &sqlDB{db: db}, nil
}

func (s *sqlDB) Close() error {
	return s.db.Close()
}

func migrateAuthors(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	rows, err := mysql.db.QueryContext(ctx, "SELECT author_id, COALESCE(first_name,''), COALESCE(last_name,'') FROM authors")
	if err != nil {
		return 0, err
	}

	type record struct {
		id                  int64
		firstName, lastName string
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.firstName, &r.lastName); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d authors from MySQL", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			"INSERT INTO authors (author_id, first_name, last_name) VALUES (?, ?, ?) ON CONFLICT (author_id) DO NOTHING",
			r.id, r.firstName, r.lastName).Error
		if err != nil {
			continue
		}
		count++
	}
	return count, nil
}

func migrateGenres(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	pg.Exec("ALTER TABLE genres DROP CONSTRAINT IF EXISTS genres_genre_key")

	rows, err := mysql.db.QueryContext(ctx, "SELECT genre_id, COALESCE(genre,''), COALESCE(section,''), COALESCE(subsection,'') FROM genres")
	if err != nil {
		return 0, err
	}

	type record struct {
		id                         int64
		genre, section, subsection string
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.genre, &r.section, &r.subsection); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d genres from MySQL", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			`INSERT INTO genres (genre_id, genre, section, subsection) VALUES (?, ?, ?, ?)
			 ON CONFLICT (genre_id) DO UPDATE SET genre = EXCLUDED.genre, section = EXCLUDED.section, subsection = EXCLUDED.subsection`,
			r.id, r.genre, r.section, r.subsection).Error
		if err != nil {
			continue
		}
		count++
	}

	pg.Exec("ALTER TABLE genres ADD CONSTRAINT genres_genre_key UNIQUE (genre)")
	return count, nil
}

func migrateSeries(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	pg.Exec("ALTER TABLE series DROP CONSTRAINT IF EXISTS series_ser_key")

	rows, err := mysql.db.QueryContext(ctx, "SELECT ser_id, COALESCE(ser,'') FROM series")
	if err != nil {
		return 0, err
	}

	type record struct {
		id  int64
		ser string
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.ser); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d series from MySQL", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			"INSERT INTO series (ser_id, ser) VALUES (?, ?) ON CONFLICT (ser_id) DO UPDATE SET ser = EXCLUDED.ser",
			r.id, r.ser).Error
		if err != nil {
			continue
		}
		count++
	}

	pg.Exec("ALTER TABLE series ADD CONSTRAINT series_ser_key UNIQUE (ser)")
	return count, nil
}

func migrateCatalogs(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	pg.Exec("ALTER TABLE catalogs DROP CONSTRAINT IF EXISTS catalogs_parent_id_fkey")

	rows, err := mysql.db.QueryContext(ctx, "SELECT cat_id, parent_id, COALESCE(cat_name,''), COALESCE(path,''), cat_type FROM catalogs ORDER BY cat_id")
	if err != nil {
		return 0, err
	}

	type record struct {
		id       int64
		parentID sql.NullInt64
		catName  string
		path     string
		catType  int
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.parentID, &r.catName, &r.path, &r.catType); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d catalogs from MySQL", len(records))

	var count int64
	for _, r := range records {
		var parent interface{}
		if r.parentID.Valid {
			parent = r.parentID.Int64
		}
		err := pg.Exec(
			"INSERT INTO catalogs (cat_id, parent_id, cat_name, path, cat_type) VALUES (?, ?, ?, ?, ?) ON CONFLICT (cat_id) DO UPDATE SET parent_id = EXCLUDED.parent_id, cat_name = EXCLUDED.cat_name, path = EXCLUDED.path",
			r.id, parent, r.catName, r.path, r.catType).Error
		if err != nil {
			continue
		}
		count++
	}

	pg.Exec("ALTER TABLE catalogs ADD CONSTRAINT catalogs_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES catalogs(cat_id) ON DELETE CASCADE")
	return count, nil
}

func migrateBooks(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	rows, err := mysql.db.QueryContext(ctx, `
		SELECT book_id, COALESCE(filename,''), COALESCE(path,''), filesize,
		       COALESCE(format,''), cat_id, cat_type, registerdate,
		       COALESCE(docdate,''), favorite, COALESCE(lang,''),
		       COALESCE(title,''), COALESCE(annotation,''),
		       COALESCE(cover,''), COALESCE(cover_type,''), doublicat, avail
		FROM books`)
	if err != nil {
		return 0, err
	}

	type record struct {
		id, filesize, catID, catType, favorite, doublicat, avail int64
		filename, path, format, docdate, lang, title             string
		annotation, cover, coverType                             string
		registerdate                                             time.Time
	}

	log.Printf("  Reading books from MySQL (this may take a while)...")
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.filename, &r.path, &r.filesize, &r.format, &r.catID, &r.catType,
			&r.registerdate, &r.docdate, &r.favorite, &r.lang, &r.title, &r.annotation,
			&r.cover, &r.coverType, &r.doublicat, &r.avail); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
		if len(records)%100000 == 0 {
			log.Printf("  ... read %d books", len(records))
		}
	}
	rows.Close()

	log.Printf("  Read %d books from MySQL, inserting to PostgreSQL...", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(`
			INSERT INTO books (book_id, filename, path, filesize, format, cat_id, cat_type,
			                   registerdate, docdate, favorite, lang, title, annotation,
			                   cover, cover_type, doublicat, avail)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (book_id) DO NOTHING`,
			r.id, r.filename, r.path, r.filesize, r.format, r.catID, r.catType,
			r.registerdate, r.docdate, r.favorite, r.lang, r.title, r.annotation,
			r.cover, r.coverType, r.doublicat, r.avail).Error
		if err != nil {
			continue
		}
		count++
		if count%50000 == 0 {
			log.Printf("  ... %d books inserted", count)
		}
	}
	return count, nil
}

func migrateBauthors(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	// Read all rows first to avoid MySQL timeout
	rows, err := mysql.db.QueryContext(ctx, "SELECT book_id, author_id FROM bauthors")
	if err != nil {
		return 0, err
	}

	type record struct{ bookID, authorID int64 }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.bookID, &r.authorID); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d bauthors from MySQL, inserting to PostgreSQL...", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			"INSERT INTO bauthors (book_id, author_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			r.bookID, r.authorID).Error
		if err != nil {
			continue // Skip errors
		}
		count++
		if count%50000 == 0 {
			log.Printf("  ... %d bauthors", count)
		}
	}
	return count, nil
}

func migrateBgenres(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	rows, err := mysql.db.QueryContext(ctx, "SELECT book_id, genre_id FROM bgenres")
	if err != nil {
		return 0, err
	}

	type record struct{ bookID, genreID int64 }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.bookID, &r.genreID); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d bgenres from MySQL, inserting to PostgreSQL...", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			"INSERT INTO bgenres (book_id, genre_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
			r.bookID, r.genreID).Error
		if err != nil {
			continue
		}
		count++
		if count%50000 == 0 {
			log.Printf("  ... %d bgenres", count)
		}
	}
	return count, nil
}

func migrateBseries(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	rows, err := mysql.db.QueryContext(ctx, "SELECT book_id, ser_id, ser_no FROM bseries")
	if err != nil {
		return 0, err
	}

	type record struct {
		bookID, serID int64
		serNo         int
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.bookID, &r.serID, &r.serNo); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()

	log.Printf("  Read %d bseries from MySQL, inserting to PostgreSQL...", len(records))

	var count int64
	for _, r := range records {
		err := pg.Exec(
			"INSERT INTO bseries (book_id, ser_id, ser_no) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			r.bookID, r.serID, r.serNo).Error
		if err != nil {
			continue
		}
		count++
		if count%50000 == 0 {
			log.Printf("  ... %d bseries", count)
		}
	}
	return count, nil
}

func migrateBookshelf(ctx context.Context, mysql *sqlDB, pg *persistence.DB) (int64, error) {
	rows, err := mysql.db.QueryContext(ctx, "SELECT user, book_id, readtime FROM bookshelf")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var userName string
		var bookID int64
		var readtime time.Time
		if err := rows.Scan(&userName, &bookID, &readtime); err != nil {
			return count, err
		}
		err := pg.Exec(
			"INSERT INTO bookshelf (user_name, book_id, readtime) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			userName, bookID, readtime).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func resetSequences(pg *persistence.DB) error {
	sequences := []struct {
		seq   string
		table string
		col   string
	}{
		{"books_book_id_seq", "books", "book_id"},
		{"authors_author_id_seq", "authors", "author_id"},
		{"genres_genre_id_seq", "genres", "genre_id"},
		{"series_ser_id_seq", "series", "ser_id"},
		{"catalogs_cat_id_seq", "catalogs", "cat_id"},
	}

	for _, s := range sequences {
		err := pg.Exec(fmt.Sprintf(
			"SELECT setval('%s', COALESCE((SELECT MAX(%s) FROM %s), 1))",
			s.seq, s.col, s.table)).Error
		if err != nil {
			return err
		}
	}
	return nil
}
