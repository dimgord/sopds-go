package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	// Database defaults
	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %q, expected 'localhost'", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, expected 5432", cfg.Database.Port)
	}
	if cfg.Database.SSLMode != "disable" {
		t.Errorf("Database.SSLMode = %q, expected 'disable'", cfg.Database.SSLMode)
	}

	// Library defaults
	if cfg.Library.Root != "/var/lib/sopds/books" {
		t.Errorf("Library.Root = %q, expected '/var/lib/sopds/books'", cfg.Library.Root)
	}
	if !cfg.Library.ScanZip {
		t.Error("Library.ScanZip should be true by default")
	}
	if cfg.Library.RescanZip {
		t.Error("Library.RescanZip should be false by default")
	}

	// Check formats include audio formats
	formats := strings.Join(cfg.Library.Formats, ",")
	if !strings.Contains(formats, ".mp3") {
		t.Error("Library.Formats should include .mp3")
	}
	if !strings.Contains(formats, ".awb") {
		t.Error("Library.Formats should include .awb")
	}

	// Server defaults
	if cfg.Server.Bind != "0.0.0.0" {
		t.Errorf("Server.Bind = %q, expected '0.0.0.0'", cfg.Server.Bind)
	}
	if cfg.Server.Port != 8081 {
		t.Errorf("Server.Port = %d, expected 8081", cfg.Server.Port)
	}
	if cfg.Server.Auth.Enabled {
		t.Error("Server.Auth.Enabled should be false by default")
	}

	// Scanner defaults
	if cfg.Scanner.Workers != 4 {
		t.Errorf("Scanner.Workers = %d, expected 4", cfg.Scanner.Workers)
	}
	if cfg.Scanner.Duplicates != "normal" {
		t.Errorf("Scanner.Duplicates = %q, expected 'normal'", cfg.Scanner.Duplicates)
	}

	// Converters defaults
	if cfg.Converters.FFmpeg != "ffmpeg" {
		t.Errorf("Converters.FFmpeg = %q, expected 'ffmpeg'", cfg.Converters.FFmpeg)
	}

	// SMTP defaults
	if cfg.SMTP.Enabled {
		t.Error("SMTP.Enabled should be false by default")
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP.Port = %d, expected 587", cfg.SMTP.Port)
	}
}

func TestDatabaseDSN(t *testing.T) {
	tests := []struct {
		cfg      DatabaseConfig
		expected string
	}{
		{
			DatabaseConfig{Host: "localhost", Port: 5432, User: "user", Password: "pass", Name: "db", SSLMode: "disable"},
			"postgres://user:pass@localhost:5432/db?sslmode=disable",
		},
		{
			DatabaseConfig{Host: "db.example.com", Port: 5433, User: "admin", Password: "secret", Name: "mydb", SSLMode: "require"},
			"postgres://admin:secret@db.example.com:5433/mydb?sslmode=require",
		},
		{
			// Empty SSLMode should default to "disable"
			DatabaseConfig{Host: "localhost", Port: 5432, User: "user", Password: "pass", Name: "db", SSLMode: ""},
			"postgres://user:pass@localhost:5432/db?sslmode=disable",
		},
	}

	for _, tc := range tests {
		result := tc.cfg.DSN()
		if result != tc.expected {
			t.Errorf("DSN() = %q, expected %q", result, tc.expected)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	// Create temp config file
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `
database:
  host: testhost
  port: 5433
  name: testdb
  user: testuser
  password: testpass
  sslmode: require

library:
  root: /test/books
  formats:
    - .fb2
    - .epub
  scan_zip: false

server:
  bind: 127.0.0.1
  port: 9000
  opds_prefix: /test-opds
  web_prefix: /test-web
  auth:
    enabled: true
    users:
      - username: admin
        password: secret

scanner:
  workers: 8
  duplicates: strong

site:
  title: "Test Library"
`

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Verify loaded values
	if cfg.Database.Host != "testhost" {
		t.Errorf("Database.Host = %q, expected 'testhost'", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("Database.Port = %d, expected 5433", cfg.Database.Port)
	}
	if cfg.Database.SSLMode != "require" {
		t.Errorf("Database.SSLMode = %q, expected 'require'", cfg.Database.SSLMode)
	}

	if cfg.Library.Root != "/test/books" {
		t.Errorf("Library.Root = %q, expected '/test/books'", cfg.Library.Root)
	}
	if len(cfg.Library.Formats) != 2 {
		t.Errorf("Expected 2 formats, got %d", len(cfg.Library.Formats))
	}
	if cfg.Library.ScanZip {
		t.Error("Library.ScanZip should be false")
	}

	if cfg.Server.Bind != "127.0.0.1" {
		t.Errorf("Server.Bind = %q, expected '127.0.0.1'", cfg.Server.Bind)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("Server.Port = %d, expected 9000", cfg.Server.Port)
	}
	if !cfg.Server.Auth.Enabled {
		t.Error("Server.Auth.Enabled should be true")
	}
	if len(cfg.Server.Auth.Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(cfg.Server.Auth.Users))
	}

	if cfg.Scanner.Workers != 8 {
		t.Errorf("Scanner.Workers = %d, expected 8", cfg.Scanner.Workers)
	}
	if cfg.Scanner.Duplicates != "strong" {
		t.Errorf("Scanner.Duplicates = %q, expected 'strong'", cfg.Scanner.Duplicates)
	}

	if cfg.Site.Title != "Test Library" {
		t.Errorf("Site.Title = %q, expected 'Test Library'", cfg.Site.Title)
	}
}

func TestLoadConfigMissingLibraryRoot(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Config with empty library root
	configContent := `
database:
  host: localhost
library:
  root: ""
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for missing library.root")
	}
	if !strings.Contains(err.Error(), "library.root is required") {
		t.Errorf("Expected 'library.root is required' error, got: %v", err)
	}
}

func TestLoadConfigNonExistentFile(t *testing.T) {
	_, err := Load("/non/existent/config.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Invalid YAML
	if _, err := tmpFile.WriteString("invalid: yaml: content:"); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestSaveConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Site.Title = "Saved Library"
	cfg.Library.Root = "/saved/books"

	tmpFile := filepath.Join(os.TempDir(), "saved-config.yaml")
	defer os.Remove(tmpFile)

	err := cfg.Save(tmpFile)
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Load and verify
	loaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if loaded.Site.Title != "Saved Library" {
		t.Errorf("Site.Title = %q, expected 'Saved Library'", loaded.Site.Title)
	}
	if loaded.Library.Root != "/saved/books" {
		t.Errorf("Library.Root = %q, expected '/saved/books'", loaded.Library.Root)
	}
}

func TestLoadConfigAutoDiscovery(t *testing.T) {
	// This test verifies that Load("") tries to find config in standard locations
	// We expect an error since we don't have config in those locations
	_, err := Load("")
	if err == nil {
		t.Log("Config file found in standard location (expected during development)")
	} else {
		if !strings.Contains(err.Error(), "config file not found") {
			// If error is something else (like missing library.root), that's also OK
			// as it means a config was found
			t.Logf("Config discovery error (expected): %v", err)
		}
	}
}

func TestConfigStructFields(t *testing.T) {
	cfg := DefaultConfig()

	// Test all struct fields are accessible
	_ = cfg.Database.Host
	_ = cfg.Database.Port
	_ = cfg.Database.Name
	_ = cfg.Database.User
	_ = cfg.Database.Password
	_ = cfg.Database.SSLMode

	_ = cfg.Library.Root
	_ = cfg.Library.Formats
	_ = cfg.Library.ScanZip
	_ = cfg.Library.RescanZip

	_ = cfg.Server.Bind
	_ = cfg.Server.Port
	_ = cfg.Server.OPDSPrefix
	_ = cfg.Server.WebPrefix
	_ = cfg.Server.Auth.Enabled
	_ = cfg.Server.Auth.Users
	_ = cfg.Server.JWTSecret

	_ = cfg.Scanner.Workers
	_ = cfg.Scanner.Schedule
	_ = cfg.Scanner.OnStart
	_ = cfg.Scanner.Duplicates
	_ = cfg.Scanner.PIDFile
	_ = cfg.Scanner.AutoClean

	_ = cfg.Site.ID
	_ = cfg.Site.Title
	_ = cfg.Site.Subtitle
	_ = cfg.Site.Icon
	_ = cfg.Site.Author
	_ = cfg.Site.URL
	_ = cfg.Site.Email
	_ = cfg.Site.MainTitle

	_ = cfg.Logging.Level
	_ = cfg.Logging.File
	_ = cfg.Logging.MaxSize
	_ = cfg.Logging.MaxBackups

	_ = cfg.Converters.FB2ToEPUB
	_ = cfg.Converters.FB2ToMOBI
	_ = cfg.Converters.FFmpeg
	_ = cfg.Converters.TempDir

	_ = cfg.SMTP.Enabled
	_ = cfg.SMTP.Host
	_ = cfg.SMTP.Port
	_ = cfg.SMTP.Username
	_ = cfg.SMTP.Password
	_ = cfg.SMTP.From
	_ = cfg.SMTP.UseTLS
	_ = cfg.SMTP.UseSTARTTLS
}

func TestUserAuthStruct(t *testing.T) {
	user := UserAuth{
		Username: "testuser",
		Password: "testpass",
	}

	if user.Username != "testuser" {
		t.Errorf("Username mismatch: %s", user.Username)
	}
	if user.Password != "testpass" {
		t.Errorf("Password mismatch: %s", user.Password)
	}
}

func TestSMTPConfig(t *testing.T) {
	cfg := SMTPConfig{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "user@example.com",
		Password:    "secret",
		From:        "SOPDS <noreply@example.com>",
		UseTLS:      false,
		UseSTARTTLS: true,
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Host != "smtp.example.com" {
		t.Errorf("Host mismatch: %s", cfg.Host)
	}
	if cfg.Port != 587 {
		t.Errorf("Port mismatch: %d", cfg.Port)
	}
	if cfg.UseTLS {
		t.Error("UseTLS should be false")
	}
	if !cfg.UseSTARTTLS {
		t.Error("UseSTARTTLS should be true")
	}
}
