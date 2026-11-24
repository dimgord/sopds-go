package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dimgord/sopds-go/internal/config"
)

// With Basic auth on, a browser page load without credentials goes to the landing page;
// everything else keeps the 401 challenge (Rev 136).
func TestAuthMiddlewareRedirectsBrowserToLanding(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.WebPrefix = "/web"
	cfg.Server.OPDSPrefix = "/opds"
	cfg.Server.Auth.Enabled = true
	cfg.Server.Auth.Users = []config.UserAuth{{Username: "guest", Password: "pw"}}
	s := &Server{config: cfg}
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const html = "text/html,application/xhtml+xml,*/*;q=0.8"
	cases := []struct {
		name, method, path, accept, user, pass string
		want                                   int
	}{
		{"browser web root", "GET", "/web/", html, "", "", http.StatusFound},
		{"browser web no slash", "GET", "/web", html, "", "", http.StatusFound},
		{"browser deep link", "GET", "/web/book/1", html, "", "", http.StatusFound},
		{"browser on opds", "GET", "/opds/", html, "", "", http.StatusUnauthorized},
		{"opds client on web", "GET", "/web/", "application/atom+xml", "", "", http.StatusUnauthorized},
		{"no accept header", "GET", "/web/", "", "", "", http.StatusUnauthorized},
		{"post without auth", "POST", "/web/x", html, "", "", http.StatusUnauthorized},
		{"wrong password", "GET", "/web/", html, "guest", "bad", http.StatusUnauthorized},
		{"right password", "GET", "/web/", html, "guest", "pw", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.accept != "" {
				req.Header.Set("Accept", c.accept)
			}
			if c.user != "" {
				req.SetBasicAuth(c.user, c.pass)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
			if c.want == http.StatusFound {
				if loc := rec.Header().Get("Location"); loc != "/web/landing" {
					t.Fatalf("Location %q, want /web/landing", loc)
				}
			}
			if c.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("401 without WWW-Authenticate")
			}
		})
	}

	// Auth off: no challenge and no redirect.
	cfg.Server.Auth.Enabled = false
	req := httptest.NewRequest("GET", "/web/", nil)
	req.Header.Set("Accept", html)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth disabled: status %d, want 200", rec.Code)
	}
}
