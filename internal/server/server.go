package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/dimgord/sopds-go/internal/config"
	"github.com/dimgord/sopds-go/internal/converter"
	"github.com/dimgord/sopds-go/internal/domain/repository"
	"github.com/dimgord/sopds-go/internal/i18n"
	"github.com/dimgord/sopds-go/internal/infrastructure/persistence"
	"github.com/dimgord/sopds-go/internal/opds"
	"github.com/dimgord/sopds-go/internal/tts"
)

// Server represents the HTTP server
type Server struct {
	config       *config.Config
	svc          *persistence.Service
	converter    *converter.Converter
	userRepo     repository.UserRepository
	emailService *EmailService
	authHandlers *AuthHandlers
	ttsGenerator *tts.Generator
	httpServer   *http.Server
	router       chi.Router
}

// New creates a new HTTP server
func New(cfg *config.Config, svc *persistence.Service) *Server {
	// Determine ebook-convert path for MOBI conversion
	ebookConvertPath := cfg.Converters.FB2ToMOBI
	if ebookConvertPath == "" {
		ebookConvertPath = "ebook-convert" // Use system PATH
	}

	// Set JWT secret from config
	if cfg.Server.JWTSecret != "" {
		SetJWTSecret(cfg.Server.JWTSecret)
	}

	// Determine base URL for email links
	baseURL := cfg.Site.URL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}

	s := &Server{
		config:       cfg,
		svc:          svc,
		converter:    converter.New(ebookConvertPath),
		userRepo:     svc.Repos().Users,
		emailService: NewEmailService(&cfg.SMTP, cfg.Site.Title, baseURL),
	}

	// Initialize TTS generator if enabled
	if cfg.TTS.Enabled {
		s.ttsGenerator = tts.NewGenerator(&cfg.TTS, s.getBookDataForTTS)
	}

	s.authHandlers = NewAuthHandlers(s)
	s.router = s.setupRouter()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Bind, cfg.Server.Port),
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) setupRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware - order matters!
	r.Use(headToGet) // Convert HEAD to GET first
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	// Favicon (no auth required)
	r.Get("/favicon.ico", s.handleFavicon)
	r.Get("/favicon.svg", s.handleFavicon)

	// Auth API routes (rate-limited, no auth required)
	r.Route("/api/auth", func(r chi.Router) {
		r.With(RateLimitMiddleware(checkRateLimiter)).Get("/check-username", s.authHandlers.HandleCheckUsername)
		r.With(RateLimitMiddleware(checkRateLimiter)).Get("/check-email", s.authHandlers.HandleCheckEmail)
		r.Get("/check-password", s.authHandlers.HandleCheckPassword) // No rate limit - client-side only
	})

	// Auth page routes (no auth required) - must be before authMiddleware
	r.Get(s.config.Server.WebPrefix+"/landing", s.authHandlers.HandleLanding)
	r.Get(s.config.Server.WebPrefix+"/login", s.authHandlers.HandleLogin)
	r.Post(s.config.Server.WebPrefix+"/login", s.authHandlers.HandleLogin)
	r.Get(s.config.Server.WebPrefix+"/register", s.authHandlers.HandleRegister)
	r.Post(s.config.Server.WebPrefix+"/register", s.authHandlers.HandleRegister)
	r.Get(s.config.Server.WebPrefix+"/logout", s.authHandlers.HandleLogout)
	r.Get(s.config.Server.WebPrefix+"/forgot-password", s.authHandlers.HandleForgotPassword)
	r.Post(s.config.Server.WebPrefix+"/forgot-password", s.authHandlers.HandleForgotPassword)
	r.Get(s.config.Server.WebPrefix+"/reset-password", s.authHandlers.HandleResetPassword)
	r.Post(s.config.Server.WebPrefix+"/reset-password", s.authHandlers.HandleResetPassword)
	r.Get(s.config.Server.WebPrefix+"/verify-email", s.authHandlers.HandleVerifyEmail)
	r.Get(s.config.Server.WebPrefix+"/resend-verification", s.authHandlers.HandleResendVerification)
	r.With(RateLimitMiddleware(checkRateLimiter)).Post(s.config.Server.WebPrefix+"/resend-verification", s.authHandlers.HandleResendVerification)
	r.Post(s.config.Server.WebPrefix+"/guest", s.authHandlers.HandleGuestLogin)

	// Apply auth middleware (JWT + optional basic auth) for all routes below
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

	// OPDS routes
	r.Route(s.config.Server.OPDSPrefix, func(r chi.Router) {
		r.Get("/", s.handleMainMenu)
		r.Get("/opensearch.xml", s.handleOpenSearch)
		r.Get("/catalogs", s.handleCatalogs)
		r.Get("/catalogs/{id}", s.handleCatalog)
		r.Get("/authors", s.handleAuthors)
		r.Get("/authors/{id}", s.handleAuthor)
		r.Get("/titles", s.handleTitles)
		r.Get("/genres", s.handleGenres)
		r.Get("/genres/{id}", s.handleGenre)
		r.Get("/series", s.handleSeriesList)
		r.Get("/series/{id}", s.handleSeries)
		r.Get("/new", s.handleNew)
		r.Get("/search", s.handleSearch)
		r.Get("/bookshelf", s.handleBookshelf)
		r.Get("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Post("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Get("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Post("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Get("/book/{id}/download", s.handleDownload)
		r.Get("/book/{id}/cover", s.handleCover)
		r.Get("/book/{id}/epub", s.handleConvertEPUB)
		r.Get("/book/{id}/mobi", s.handleConvertMOBI)
	})

	// Web routes (HTML version)
	r.Route(s.config.Server.WebPrefix, func(r chi.Router) {
		r.Get("/", s.handleWebHome)
		r.Get("/search", s.handleWebSearch)
		r.Get("/authors", s.handleWebAuthors)
		r.Get("/authors/{id}", s.handleWebAuthor)
		r.Get("/genres", s.handleWebGenres)
		r.Get("/genres/{id}", s.handleWebGenre)
		r.Get("/series", s.handleWebSeries)
		r.Get("/series/{id}", s.handleWebSeriesBooks)
		r.Get("/languages", s.handleWebLanguages)
		r.Get("/languages/{lang}", s.handleWebLanguage)
		r.Get("/new", s.handleWebNew)
		r.Get("/audio", s.handleWebAudio)
		r.Get("/audio/{id}", s.handleWebAudioDetail)
		r.Get("/audio/{id}/track", s.handleAudioTrackDownload)
		r.Get("/audio/{id}/cover", s.handleAudioTrackCover)
		r.Get("/catalogs", s.handleWebCatalogs)
		r.Get("/catalogs/{id}", s.handleWebCatalog)
		r.Get("/bookshelf", s.handleWebBookshelf)
		r.Get("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Post("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Get("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Post("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Get("/duplicates/{id}", s.handleWebDuplicates)
		r.Get("/help", s.handleWebHelp)
		r.Get("/read/{id}", s.handleWebReader)

		// TTS routes (text-to-speech)
		r.Post("/book/{id}/tts/generate", s.handleTTSGenerate)
		r.Get("/book/{id}/tts/status", s.handleTTSStatus)
		r.Get("/book/{id}/tts", s.handleTTSPlayer)
		r.Get("/book/{id}/tts/chunk/{idx}", s.handleTTSChunk)
	})

		// Health check (inside auth group)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})
	}) // Close auth middleware Group

	return r
}

// Start starts the HTTP server
func (s *Server) Start() error {
	// Start TTS workers if enabled
	if s.ttsGenerator != nil {
		s.ttsGenerator.Start(context.Background())
	}

	log.Printf("Starting HTTP server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop TTS workers
	if s.ttsGenerator != nil {
		s.ttsGenerator.Stop()
	}
	return s.httpServer.Shutdown(ctx)
}

// getBookDataForTTS returns book data for TTS processing
func (s *Server) getBookDataForTTS(bookID int64) ([]byte, string, error) {
	ctx := context.Background()

	book, err := s.svc.GetBook(ctx, bookID)
	if err != nil {
		return nil, "", err
	}

	// Read book content based on storage type
	var data []byte
	if strings.Contains(book.Path, ".zip") {
		data, err = s.readFromZip(book)
	} else if strings.Contains(book.Path, ".7z") {
		data, err = s.readFromArchive(book)
	} else {
		filePath := s.getBookPath(book)
		data, err = os.ReadFile(filePath)
	}

	if err != nil {
		return nil, "", err
	}

	return data, book.Title, nil
}

// basicAuth middleware for HTTP Basic Authentication
func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			s.unauthorized(w)
			return
		}

		// Check credentials
		valid := false
		for _, user := range s.config.Server.Auth.Users {
			if user.Username == username && user.Password == password {
				valid = true
				break
			}
		}

		if !valid {
			s.unauthorized(w)
			return
		}

		// Store username in context
		ctx := context.WithValue(r.Context(), "username", username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="SOPDS Library"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// getBaseURL constructs the base URL for OPDS links
func (s *Server) getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, r.Host, s.config.Server.OPDSPrefix)
}

// getFeedBuilder creates a new feed builder for the request
func (s *Server) getFeedBuilder(r *http.Request) *opds.FeedBuilder {
	return opds.NewFeedBuilder(s.config, s.getBaseURL(r))
}

// writeOPDS writes an OPDS feed response
func (s *Server) writeOPDS(w http.ResponseWriter, feed *opds.Feed) {
	data, err := feed.Render()
	if err != nil {
		http.Error(w, "Failed to render feed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;charset=utf-8")
	w.Write(data)
}

// writeError writes an error response
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

// headToGet middleware converts HEAD requests to GET
func headToGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			r.Method = http.MethodGet
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware handles JWT authentication and optional basic auth fallback
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var username string

		// First check JWT cookie
		if cookie, err := r.Cookie(JWTCookieName); err == nil && cookie.Value != "" {
			if claims, err := ValidateJWT(cookie.Value); err == nil {
				// Valid JWT - set user in context
				u, err := s.userRepo.GetByID(r.Context(), claims.UserID)
				if err == nil && u != nil {
					ctx := context.WithValue(r.Context(), ctxUserKey, u)
					ctx = context.WithValue(ctx, "username", u.Username)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		// Check for anonymous session cookie
		if anonID := GetAnonCookie(r); anonID != "" {
			ctx := context.WithValue(r.Context(), ctxAnonKey, anonID)
			ctx = context.WithValue(ctx, "username", anonID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// If basic auth is enabled and required, check it
		if s.config.Server.Auth.Enabled {
			var password string
			var ok bool
			username, password, ok = r.BasicAuth()
			if !ok {
				s.unauthorized(w)
				return
			}

			// Check credentials
			valid := false
			for _, user := range s.config.Server.Auth.Users {
				if user.Username == username && user.Password == password {
					valid = true
					break
				}
			}

			if !valid {
				s.unauthorized(w)
				return
			}

			// Create anonymous session with basic auth username
			SetAnonCookie(w, username)
			ctx := context.WithValue(r.Context(), ctxAnonKey, username)
			ctx = context.WithValue(ctx, "username", username)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// No auth required - generate anonymous session
		anonID := generateAnonID()
		SetAnonCookie(w, anonID)
		ctx := context.WithValue(r.Context(), ctxAnonKey, anonID)
		ctx = context.WithValue(ctx, "username", anonID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// renderAuthTemplate renders authentication page templates
func (s *Server) renderAuthTemplate(w http.ResponseWriter, r *http.Request, templateName string, data map[string]string) {
	// Get language from cookie/query
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		if cookie, err := r.Cookie("lang"); err == nil {
			lang = cookie.Value
		}
	}
	if lang == "" {
		lang = "en"
	}

	// Prepare template data
	tmplData := struct {
		Lang       string
		WebPrefix  string
		SiteTitle  string
		Error      string
		Success    string
		Token      string
		Title      string
		Message    string
		Registered bool
		Verified   bool
		Reset      bool
		T          map[string]string
	}{
		Lang:      lang,
		WebPrefix: s.config.Server.WebPrefix,
		SiteTitle: s.config.Site.Title,
		T:         getAuthTranslations(lang),
	}

	if data != nil {
		tmplData.Error = data["error"]
		tmplData.Success = data["success"]
		tmplData.Token = data["token"]
		tmplData.Title = data["title"]
		tmplData.Message = data["message"]
	}

	// Check query params for messages
	if r.URL.Query().Get("registered") == "1" {
		tmplData.Registered = true
	}
	if r.URL.Query().Get("verified") == "1" {
		tmplData.Verified = true
	}
	if r.URL.Query().Get("reset") == "1" {
		tmplData.Reset = true
	}

	// Render template - first parse the page template to get "content" definition,
	// then execute the "auth" base template which includes {{template "content" .}}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Clone the base template and add the specific page content
	tmpl, err := authTemplates.Clone()
	if err != nil {
		log.Printf("Template clone error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	// The page templates define "content" block - execute base which uses it
	pageContent, ok := authPageTemplates[templateName]
	if !ok {
		log.Printf("Template not found: %s", templateName)
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}

	if _, err := tmpl.Parse(pageContent); err != nil {
		log.Printf("Template parse error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "auth", tmplData); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// getAuthTranslations returns translations for auth pages from i18n package
// Auth templates use flat keys like {{.T.login}}, so we extract "auth.*" keys
// and return them without the "auth." prefix
func getAuthTranslations(lang string) map[string]string {
	allTranslations := i18n.GetTranslations(lang)
	result := make(map[string]string)

	// Auth-specific keys (stored under "auth." prefix in YAML)
	authKeys := []string{
		"login", "logout", "register", "guest", "guest_warning",
		"email", "username", "password", "confirm_password",
		"forgot_password", "reset_password", "send_reset_link",
		"login_or_username", "continue_as_guest",
		"already_have_account", "no_account",
		"register_success", "verify_success", "reset_success",
		"password_requirements", "username_requirements",
		"welcome", "library_description",
		"or", "req_length", "req_lower", "req_upper", "req_digit",
		"passwords_no_match", "username_not_available", "email_not_available",
	}

	for _, key := range authKeys {
		if val, ok := allTranslations["auth."+key]; ok {
			result[key] = val
		}
	}

	return result
}
