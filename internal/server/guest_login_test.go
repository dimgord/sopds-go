package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dimgord/sopds-go/internal/config"
)

const legacyAnonID = "0123456789abcdef0123456789abcdef"

func guestServer(authOn bool) *Server {
	cfg := &config.Config{}
	cfg.Server.WebPrefix = "/web"
	cfg.Server.OPDSPrefix = "/opds"
	cfg.Server.Auth.Enabled = authOn
	cfg.Server.Auth.Users = []config.UserAuth{{Username: "guest", Password: "pw"}}
	return &Server{config: cfg}
}

func anonCookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == AnonCookieName {
			return c
		}
	}
	return nil
}

func TestAnonCookieSignature(t *testing.T) {
	SetJWTSecret("test-secret")
	rec := httptest.NewRecorder()
	SetAnonCookie(rec, "abc")
	c := anonCookieFrom(rec)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	if got := GetAnonCookie(req); got != "abc" {
		t.Fatalf("signed cookie: got %q, want abc", got)
	}
	for _, v := range []string{"abc", legacyAnonID, "abd." + strings.SplitN(c.Value, ".", 2)[1], "abc.bad", ".x"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: AnonCookieName, Value: v})
		if got := GetAnonCookie(req); got != "" {
			t.Errorf("forged cookie %q accepted as %q", v, got)
		}
	}
	if a, b := newRandomSecret(), newRandomSecret(); len(a) != 32 || bytes.Equal(a, b) {
		t.Error("newRandomSecret must return 32 fresh random bytes")
	}
}

// With auth on, a self-made anon cookie no longer skips the password (Rev 137).
func TestForgedAnonCookieRejected(t *testing.T) {
	SetJWTSecret("test-secret")
	s := guestServer(true)
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))

	signed := httptest.NewRecorder()
	SetAnonCookie(signed, "abc")
	cases := []struct {
		name, path, accept string
		cookie             *http.Cookie
		want               int
	}{
		{"forged, browser", "/web/", "text/html", &http.Cookie{Name: AnonCookieName, Value: legacyAnonID}, http.StatusFound},
		{"forged, opds", "/opds/", "application/atom+xml", &http.Cookie{Name: AnonCookieName, Value: legacyAnonID}, http.StatusUnauthorized},
		{"signed", "/web/", "text/html", anonCookieFrom(signed), http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", c.path, nil)
			req.Header.Set("Accept", c.accept)
			req.AddCookie(c.cookie)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
		})
	}
}

// With auth off, an old unsigned anon ID is kept (its bookshelf) and re-issued signed.
func TestLegacyAnonCookieAdoptedWhenAuthOff(t *testing.T) {
	SetJWTSecret("test-secret")
	s := guestServer(false)
	var seen string
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetAuthInfo(r).AnonID
	}))
	req := httptest.NewRequest("GET", "/web/", nil)
	req.AddCookie(&http.Cookie{Name: AnonCookieName, Value: legacyAnonID})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != legacyAnonID {
		t.Fatalf("anon ID %q, want the legacy one", seen)
	}
	if c := anonCookieFrom(rec); c == nil || !strings.HasPrefix(c.Value, legacyAnonID+".") {
		t.Fatalf("legacy ID not re-issued signed: %v", c)
	}
}

func postGuest(h *AuthHandlers, ip, password string) *httptest.ResponseRecorder {
	form := url.Values{}
	if password != "" {
		form.Set("password", password)
	}
	req := httptest.NewRequest("POST", "/web/guest", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	h.HandleGuestLogin(rec, req)
	return rec
}

func TestGuestLoginNeedsPassword(t *testing.T) {
	SetJWTSecret("test-secret")
	h := NewAuthHandlers(guestServer(true))

	rec := postGuest(h, "198.51.100.1", "wrong")
	if rec.Code != http.StatusUnauthorized || anonCookieFrom(rec) != nil {
		t.Fatalf("wrong password: status %d, cookie %v", rec.Code, anonCookieFrom(rec))
	}
	if !strings.Contains(rec.Body.String(), "Wrong password") || !strings.Contains(rec.Body.String(), `name="password"`) {
		t.Fatal("wrong password: landing with error and password field expected")
	}
	if rec := postGuest(h, "198.51.100.2", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no password: status %d, want 401", rec.Code)
	}

	rec = postGuest(h, "198.51.100.3", "pw")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/web/" {
		t.Fatalf("right password: status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(anonCookieFrom(rec))
	if GetAnonCookie(req) == "" {
		t.Fatal("right password: issued cookie does not verify")
	}

	for i := 0; i < 10; i++ {
		postGuest(h, "198.51.100.4", "wrong")
	}
	if rec := postGuest(h, "198.51.100.4", "pw"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th try: status %d, want 429", rec.Code)
	}
}

func TestGuestLoginAuthOff(t *testing.T) {
	SetJWTSecret("test-secret")
	h := NewAuthHandlers(guestServer(false))
	rec := postGuest(h, "198.51.100.5", "")
	if rec.Code != http.StatusSeeOther || anonCookieFrom(rec) == nil {
		t.Fatalf("auth off: status %d, cookie %v", rec.Code, anonCookieFrom(rec))
	}
	landing := httptest.NewRecorder()
	h.HandleLanding(landing, httptest.NewRequest("GET", "/web/landing", nil))
	if strings.Contains(landing.Body.String(), `name="password"`) {
		t.Fatal("auth off: landing must not ask for a password")
	}
}
