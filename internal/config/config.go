package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the main application configuration
type Config struct {
	Database   DatabaseConfig   `yaml:"database"`
	Library    LibraryConfig    `yaml:"library"`
	Server     ServerConfig     `yaml:"server"`
	Scanner    ScannerConfig    `yaml:"scanner"`
	Site       SiteConfig       `yaml:"site"`
	Logging    LoggingConfig    `yaml:"logging"`
	Converters ConvertersConfig `yaml:"converters"`
	SMTP       SMTPConfig       `yaml:"smtp"`
	TTS        TTSConfig        `yaml:"tts"`
}

// TTSConfig holds text-to-speech settings
type TTSConfig struct {
	Enabled      bool              `yaml:"enabled"`
	ModelsDir    string            `yaml:"models_dir"`    // Directory with .onnx voice models
	Voices       map[string]string `yaml:"voices"`        // lang code -> model name
	DefaultVoice string            `yaml:"default_voice"` // Fallback voice model
	CacheDir     string            `yaml:"cache_dir"`     // Generated audio storage
	Workers      int               `yaml:"workers"`       // Parallel generation jobs
	ChunkSize    int               `yaml:"chunk_size"`    // Bytes of text per audio chunk (keep ~1000: Piper/VITS attention is O(n²) — big chunks OOM the GPU)
	Mode         string            `yaml:"mode"`          // "request" (collect demand, no auto-gen — default) | "generate" (legacy piper auto-gen)
	Worker       WorkerConfig      `yaml:"worker"`        // auto-F5 fulfillment worker (`sopds tts-worker`)
}

// RequestMode reports whether the Listen button collects requests instead of auto-generating (default).
func (t TTSConfig) RequestMode() bool { return t.Mode != "generate" }

// WorkerConfig drives `sopds tts-worker` — a separate process (run as the F5-env user on the GPU box)
// that auto-fulfills the most-requested pending books with F5-TTS via f5-bridge/fb2-to-f5.sh.
type WorkerConfig struct {
	Threshold    int                         `yaml:"threshold"`     // min unique requests before a book is generated
	Review       string                      `yaml:"review"`        // "gate" (stress → editor review → synth) | "auto" (one-shot MODE=all)
	OutputSubdir string                      `yaml:"output_subdir"` // subfolder under library.root for generated .7z audiobooks
	F5Home       string                      `yaml:"f5_home"`       // f5-spike path (scripts + models + ref); configurable
	Script       string                      `yaml:"script"`        // fb2-to-f5.sh path (defaults to <f5_home>/fb2-to-f5.sh)
	F5Bin        string                      `yaml:"f5_bin"`        // sopds-tts-rs binary (native synth); empty ⇒ fb2-to-f5.sh default
	MaxChars     int                         `yaml:"max_chars"`     // MAXCHARS: max chars per synth chunk (fb2-to-f5.sh default 250)
	Languages    map[string]WorkerLangConfig `yaml:"languages"`     // per-language assets; a book whose lang is absent here is skipped
}

// WorkerLangConfig is one language's synthesis assets for the auto-F5 worker.
type WorkerLangConfig struct {
	F5Model    string `yaml:"f5_model"`    // model dir: 3 ONNX graphs + vocab.txt + ref.wav/ref.txt
	NotesModel string `yaml:"notes_model"` // optional 2nd voice dir for footnotes (e.g. luka)
	Stress     string `yaml:"stress"`      // "ruaccent" | "none" (en) | "uk-stress" (later); how to accent before synth
}

// ReviewGate reports whether the worker pauses for stress-editor review before synthesis (default gate).
func (w WorkerConfig) ReviewGate() bool { return w.Review != "auto" }

// SMTPConfig holds email sending settings
type SMTPConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// PasswordEnv, when set, names an env var whose value is used as the
	// SMTP password instead of `password:` above. Keeps secrets out of
	// the config file — works with sops-decrypted env, docker env, k8s
	// secrets, etc. If the env var is unset/empty, falls back to
	// `password:` (so misconfiguration doesn't silently send anon-SMTP).
	PasswordEnv string `yaml:"password_env"`
	From        string `yaml:"from"`         // From address (e.g., "SOPDS Library <noreply@example.com>")
	UseTLS      bool   `yaml:"use_tls"`      // Port 465 implicit TLS
	UseSTARTTLS bool   `yaml:"use_starttls"` // Port 587 STARTTLS upgrade
}

// ResolvedPassword returns the effective SMTP password — env-var lookup
// when `password_env:` is set, else the literal `password:` field.
func (c *SMTPConfig) ResolvedPassword() string {
	if c.PasswordEnv != "" {
		if v := os.Getenv(c.PasswordEnv); v != "" {
			return v
		}
	}
	return c.Password
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Name     string `yaml:"name"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"sslmode"`
}

// DSN returns the PostgreSQL connection string
func (d *DatabaseConfig) DSN() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, sslMode)
}

// LibraryConfig holds book library settings
type LibraryConfig struct {
	Root      string   `yaml:"root"`
	Formats   []string `yaml:"formats"`
	ScanZip   bool     `yaml:"scan_zip"`
	RescanZip bool     `yaml:"rescan_zip"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Bind       string     `yaml:"bind"`
	Port       int        `yaml:"port"`
	OPDSPrefix string     `yaml:"opds_prefix"`
	WebPrefix  string     `yaml:"web_prefix"`
	Auth       AuthConfig `yaml:"auth"`
	JWTSecret  string     `yaml:"jwt_secret"` // Secret for signing JWT tokens
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	Enabled bool       `yaml:"enabled"`
	Users   []UserAuth `yaml:"users"`
}

// UserAuth represents a user credential
type UserAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ScannerConfig holds scanner daemon settings
type ScannerConfig struct {
	Workers    int    `yaml:"workers"`
	Schedule   string `yaml:"schedule"`
	OnStart    bool   `yaml:"on_start"`
	Duplicates string `yaml:"duplicates"` // none, normal, strong, clear
	PIDFile    string `yaml:"pid_file"`
	AutoClean  string `yaml:"auto_clean"` // ask (default), yes, no - for missing archives
}

// SiteConfig holds site metadata
type SiteConfig struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	Subtitle  string `yaml:"subtitle"`
	Icon      string `yaml:"icon"`
	Author    string `yaml:"author"`
	URL       string `yaml:"url"`
	Email     string `yaml:"email"`
	MainTitle string `yaml:"main_title"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size"`    // Max size in MB before rotation (default 10)
	MaxBackups int    `yaml:"max_backups"` // Number of old log files to keep (default 3)
}

// ConvertersConfig holds external converter paths
type ConvertersConfig struct {
	FB2ToEPUB string `yaml:"fb2toepub"`
	FB2ToMOBI string `yaml:"fb2tomobi"`
	FFmpeg    string `yaml:"ffmpeg"` // Path to ffmpeg for AWB→MP3 conversion
	TempDir   string `yaml:"temp_dir"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Name:     "sopds",
			User:     "sopds",
			Password: "sopds",
			SSLMode:  "disable",
		},
		Library: LibraryConfig{
			Root:      "/var/lib/sopds/books",
			Formats:   []string{".fb2", ".epub", ".mobi", ".pdf", ".djvu", ".mp3", ".m4b", ".m4a", ".flac", ".ogg", ".opus", ".awb"},
			ScanZip:   true,
			RescanZip: false,
		},
		Server: ServerConfig{
			Bind:       "0.0.0.0",
			Port:       8081,
			OPDSPrefix: "/opds",
			WebPrefix:  "/web",
			Auth: AuthConfig{
				Enabled: false,
				Users:   []UserAuth{},
			},
		},
		Scanner: ScannerConfig{
			Workers:    4,
			Schedule:   "0 3 * * *",
			OnStart:    false,
			Duplicates: "normal",
			PIDFile:    "/tmp/sopds.pid",
		},
		Site: SiteConfig{
			ID:        "urn:sopds:catalog",
			Title:     "SOPDS Library",
			Subtitle:  "Simple OPDS Catalog",
			Author:    "Admin",
			Email:     "admin@example.com",
			MainTitle: "Library Catalog",
		},
		Logging: LoggingConfig{
			Level:      "info",
			File:       "/var/log/sopds/sopds.log",
			MaxSize:    10,
			MaxBackups: 3,
		},
		Converters: ConvertersConfig{
			FFmpeg:  "ffmpeg", // AWB→MP3 conversion
			TempDir: "/tmp",
		},
		SMTP: SMTPConfig{
			Enabled:     false,
			Host:        "smtp.example.com",
			Port:        587,
			UseSTARTTLS: true,
		},
		TTS: TTSConfig{
			Enabled:      false,
			ModelsDir:    "/var/lib/piper/models",
			Voices:       map[string]string{},
			DefaultVoice: "",
			CacheDir:     "/var/lib/sopds/tts_cache",
			Workers:      2,
			ChunkSize:    1000, // ~1000 bytes; Piper/VITS attention is O(n²) — larger chunks blow up GPU memory (a ~2.3 KB chunk needs >8 GB)
			Mode:         "request",
			Worker: WorkerConfig{
				Threshold:    3,
				Review:       "gate",
				OutputSubdir: "_tts",
				MaxChars:     250, // matches fb2-to-f5.sh MAXCHARS default
				Languages:    map[string]WorkerLangConfig{},
			},
		},
	}
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	// Expand path
	if path == "" {
		// Look for config in standard locations
		locations := []string{
			"config.yaml",
			"config.yml",
		}

		// Add config.yaml next to the executable
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			locations = append(locations,
				filepath.Join(exeDir, "config.yaml"),
				filepath.Join(exeDir, "config.yml"),
			)
		}

		// Add system-wide locations
		locations = append(locations,
			"/etc/sopds/config.yaml",
			filepath.Join(os.Getenv("HOME"), ".config", "sopds", "config.yaml"),
		)

		for _, loc := range locations {
			if _, err := os.Stat(loc); err == nil {
				path = loc
				break
			}
		}
	}

	if path == "" {
		return nil, fmt.Errorf("config file not found; tried: config.yaml, <exe-dir>/config.yaml, /etc/sopds/config.yaml, ~/.config/sopds/config.yaml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate required fields
	if cfg.Library.Root == "" {
		return nil, fmt.Errorf("library.root is required")
	}

	return cfg, nil
}

// Save writes configuration to a YAML file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
