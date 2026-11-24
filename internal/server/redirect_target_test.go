package server

import (
	"net/http/httptest"
	"testing"
)

// Redirect targets from the client must stay on this site (Rev 138).
func TestLocalPath(t *testing.T) {
	const fb = "/web/"
	cases := map[string]string{
		"":                      fb,
		"/web/book/1":           "/web/book/1",
		"/web/search?q=a&b=c":   "/web/search?q=a&b=c",
		"/":                     "/",
		"https://evil.example/": fb,
		"//evil.example/x":      fb,
		"/\\evil.example":       fb,
		"/\t/evil.example":      fb,
		"/\r\nSet-Cookie: x":    fb,
		"javascript:alert(1)":   fb,
		"web/book/1":            fb,
		"http:/evil.example":    fb,
	}
	for in, want := range cases {
		if got := localPath(in, fb); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameSiteReferer(t *testing.T) {
	cases := map[string]string{
		"":                                       "",
		"https://library.example/web/book/1?x=1": "/web/book/1?x=1",
		"https://library.example":                "/",
		"https://evil.example/web/book/1":        "",
		"https://library.example.evil.example/web/": "",
		"/web/book/1":   "",
		"not a url %zz": "",
	}
	for ref, want := range cases {
		req := httptest.NewRequest("POST", "https://library.example/web/bookshelf/add/1", nil)
		if ref != "" {
			req.Header.Set("Referer", ref)
		}
		if got := sameSiteReferer(req); got != want {
			t.Errorf("sameSiteReferer(%q) = %q, want %q", ref, got, want)
		}
	}
}
