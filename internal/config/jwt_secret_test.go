package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The old config.yaml.example put jwt_secret at the top level, where nothing read it (Rev 137).
func TestJWTSecretPlacement(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"under server", "server:\n  jwt_secret: s1\n", "s1"},
		{"top level (old example)", "jwt_secret: s2\n", "s2"},
		{"server wins over top level", "server:\n  jwt_secret: s1\njwt_secret: s2\n", "s1"},
		{"example placeholder ignored", "jwt_secret: \"change-me-to-a-long-random-string\"\n", ""},
		{"old code default ignored", "server:\n  jwt_secret: change-this-secret-in-production\n", ""},
		{"absent", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte("library:\n  root: /tmp\n"+c.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(p)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.JWTSecret != c.want {
				t.Fatalf("Server.JWTSecret = %q, want %q", cfg.Server.JWTSecret, c.want)
			}
		})
	}
}
