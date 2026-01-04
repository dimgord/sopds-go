package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sopds/sopds-go/internal/config"
	"github.com/sopds/sopds-go/internal/converter"
	"github.com/sopds/sopds-go/internal/database"
	"github.com/sopds/sopds-go/internal/opds"
)

// Server represents the HTTP server
type Server struct {
	config     *config.Config
	db         *database.DB
	converter  *converter.Converter
	httpServer *http.Server
	router     chi.Router
}

// New creates a new HTTP server
func New(cfg *config.Config, db *database.DB) *Server {
	// Determine ebook-convert path for MOBI conversion
	ebookConvertPath := cfg.Converters.FB2ToMOBI
	if ebookConvertPath == "" {
		ebookConvertPath = "ebook-convert" // Use system PATH
	}

	s := &Server{
		config:    cfg,
		db:        db,
		converter: converter.New(ebookConvertPath),
	}

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

	// Basic auth if enabled
	if s.config.Server.Auth.Enabled {
		r.Use(s.basicAuth)
	}

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
		r.Get("/catalogs", s.handleWebCatalogs)
		r.Get("/catalogs/{id}", s.handleWebCatalog)
		r.Get("/bookshelf", s.handleWebBookshelf)
		r.Get("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Post("/bookshelf/add/{id}", s.handleBookshelfAdd)
		r.Get("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Post("/bookshelf/remove/{id}", s.handleBookshelfRemove)
		r.Get("/duplicates/{id}", s.handleWebDuplicates)
	})

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return r
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting HTTP server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
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
