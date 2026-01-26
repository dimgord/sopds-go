package server

import (
	"net/http"
)

// faviconSVG is an embedded SVG favicon (book icon)
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <defs>
    <linearGradient id="grad" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" style="stop-color:#6366f1"/>
      <stop offset="100%" style="stop-color:#4f46e5"/>
    </linearGradient>
  </defs>
  <rect width="32" height="32" rx="6" fill="url(#grad)"/>
  <g fill="white">
    <path d="M8 7h7v18H8a1 1 0 01-1-1V8a1 1 0 011-1z" opacity="0.9"/>
    <path d="M17 7h7a1 1 0 011 1v16a1 1 0 01-1 1h-7V7z" opacity="0.7"/>
    <path d="M15 7h2v18h-2z" fill="#4f46e5"/>
    <circle cx="11" cy="11" r="1.5"/>
    <rect x="9" y="14" width="4" height="1" rx="0.5"/>
    <rect x="9" y="16" width="4" height="1" rx="0.5"/>
    <rect x="9" y="18" width="3" height="1" rx="0.5"/>
  </g>
</svg>`

// handleFavicon serves the favicon
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800") // 1 week
	w.Write([]byte(faviconSVG))
}
