package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/dimgord/sopds-go/internal/domain/user"
)

// JWT constants
const (
	JWTCookieName    = "sopds_jwt"
	JWTExpiration    = 24 * time.Hour
	AnonCookieName   = "sopds_anon"
	AnonCookieExpiry = 7 * 24 * time.Hour // 7 days for anonymous session
)

// Context keys
type contextKey string

const (
	ctxUserKey contextKey = "user"
	ctxAnonKey contextKey = "anon"
)

// JWTClaims represents the claims in a JWT token
type JWTClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// AuthInfo holds current auth state for templates
type AuthInfo struct {
	IsAuthenticated bool
	IsAnonymous     bool
	UserID          int64
	Username        string
	AnonID          string // Cookie-based anonymous ID
}

// jwtSecret should be set from config or environment
var jwtSecret = []byte("change-this-secret-in-production")

// SetJWTSecret sets the JWT signing secret
func SetJWTSecret(secret string) {
	if secret != "" {
		jwtSecret = []byte(secret)
	}
}

// GenerateJWT creates a new JWT token for a user
func GenerateJWT(userID int64, username string) (string, error) {
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(JWTExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// ValidateJWT validates a JWT token and returns claims
func ValidateJWT(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}

// SetJWTCookie sets the JWT token in an HTTP-only cookie
func SetJWTCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     JWTCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(JWTExpiration.Seconds()),
	})
}

// ClearJWTCookie removes the JWT cookie
func ClearJWTCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     JWTCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// SetAnonCookie sets an anonymous session cookie
func SetAnonCookie(w http.ResponseWriter, anonID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     AnonCookieName,
		Value:    anonID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(AnonCookieExpiry.Seconds()),
	})
}

// GetAnonCookie gets the anonymous session ID from cookie
func GetAnonCookie(r *http.Request) string {
	cookie, err := r.Cookie(AnonCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// GetAuthInfo extracts auth info from request context
func GetAuthInfo(r *http.Request) AuthInfo {
	info := AuthInfo{}

	// Check for authenticated user
	if u, ok := r.Context().Value(ctxUserKey).(*user.User); ok && u != nil {
		info.IsAuthenticated = true
		info.UserID = u.ID
		info.Username = u.Username
		return info
	}

	// Check for anonymous session
	if anonID, ok := r.Context().Value(ctxAnonKey).(string); ok && anonID != "" {
		info.IsAnonymous = true
		info.AnonID = anonID
	}

	return info
}

// RateLimiter implements a simple in-memory rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// Allow checks if a request is allowed for the given key (usually IP)
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// Filter requests within window
	var recent []time.Time
	for _, t := range rl.requests[key] {
		if t.After(windowStart) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		rl.requests[key] = recent
		return false
	}

	rl.requests[key] = append(recent, now)
	return true
}

// cleanup periodically removes old entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-rl.window)
		for key, times := range rl.requests {
			var recent []time.Time
			for _, t := range times {
				if t.After(windowStart) {
					recent = append(recent, t)
				}
			}
			if len(recent) == 0 {
				delete(rl.requests, key)
			} else {
				rl.requests[key] = recent
			}
		}
		rl.mu.Unlock()
	}
}

// Rate limiters
var (
	checkRateLimiter  = NewRateLimiter(150, 1*time.Minute)  // 150 per minute for username/email check
	forgotRateLimiter = NewRateLimiter(5, 1*time.Hour)     // 5 per hour for forgot password
)

// RateLimitMiddleware creates middleware for rate limiting
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			if !limiter.Allow(ip) {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// AuthHandlers holds handlers for authentication endpoints
type AuthHandlers struct {
	server *Server
}

// NewAuthHandlers creates auth handlers
func NewAuthHandlers(s *Server) *AuthHandlers {
	return &AuthHandlers{server: s}
}

// HandleCheckUsername checks if username is available (rate limited)
func (h *AuthHandlers) HandleCheckUsername(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"available": false,
			"error":     "Username required",
		})
		return
	}

	// Validate format first
	if err := user.ValidateUsername(username); err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"error":     err.Error(),
		})
		return
	}

	exists, err := h.server.userRepo.ExistsUsername(r.Context(), username)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]interface{}{
			"available": false,
			"error":     "Server error",
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"available": !exists,
	})
}

// HandleCheckEmail checks if email is available (rate limited)
func (h *AuthHandlers) HandleCheckEmail(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("email")))
	if email == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"available": false,
			"error":     "Email required",
		})
		return
	}

	// Validate format first
	if err := user.ValidateEmail(email); err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"error":     err.Error(),
		})
		return
	}

	exists, err := h.server.userRepo.ExistsEmail(r.Context(), email)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]interface{}{
			"available": false,
			"error":     "Server error",
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"available": !exists,
	})
}

// HandleCheckPassword validates password strength (no rate limit needed - client-side data)
func (h *AuthHandlers) HandleCheckPassword(w http.ResponseWriter, r *http.Request) {
	password := r.URL.Query().Get("password")
	strength := user.CheckPasswordStrength(password)
	jsonResponse(w, http.StatusOK, strength)
}

// HandleRegister handles user registration
func (h *AuthHandlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderRegisterPage(w, r, nil)
		return
	}

	// POST - process registration
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")

	// Create user
	newUser, err := user.NewUser(username, email, password)
	if err != nil {
		h.renderRegisterPage(w, r, map[string]string{"error": err.Error()})
		return
	}

	// Save to database
	if err := h.server.userRepo.Create(r.Context(), newUser); err != nil {
		h.renderRegisterPage(w, r, map[string]string{"error": err.Error()})
		return
	}

	// Send verification email
	if err := h.server.emailService.SendVerificationEmail(email, username, newUser.VerifyToken); err != nil {
		log.Printf("Failed to send verification email to %s: %v", email, err)
		// Still redirect - user was created, they can request new verification
	}

	// Redirect to login with success message
	http.Redirect(w, r, h.server.config.Server.WebPrefix+"/login?registered=1", http.StatusSeeOther)
}

// HandleLogin handles user login
func (h *AuthHandlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderLoginPage(w, r, nil)
		return
	}

	// POST - process login
	login := strings.TrimSpace(r.FormValue("login"))
	password := r.FormValue("password")

	// Find user by email or username
	u, err := h.server.userRepo.GetByLogin(r.Context(), login)
	if err != nil {
		h.renderLoginPage(w, r, map[string]string{"error": "Invalid credentials"})
		return
	}

	// Check password
	if !u.CheckPassword(password) {
		h.renderLoginPage(w, r, map[string]string{"error": "Invalid credentials"})
		return
	}

	// Check email verification
	if !u.EmailVerified {
		h.renderLoginPage(w, r, map[string]string{"error": "Please verify your email first"})
		return
	}

	// Generate JWT
	token, err := GenerateJWT(u.ID, u.Username)
	if err != nil {
		h.renderLoginPage(w, r, map[string]string{"error": "Server error"})
		return
	}

	// Set cookie
	SetJWTCookie(w, token)

	// Update last login
	h.server.userRepo.UpdateLastLogin(r.Context(), u.ID)

	// Migrate anonymous bookshelf
	h.migrateAnonBookshelf(r.Context(), r, u.ID)

	// Redirect to home or return URL
	returnURL := r.URL.Query().Get("return")
	if returnURL == "" {
		returnURL = h.server.config.Server.WebPrefix + "/"
	}
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// HandleLogout handles user logout
func (h *AuthHandlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	ClearJWTCookie(w)
	http.Redirect(w, r, h.server.config.Server.WebPrefix+"/login", http.StatusSeeOther)
}

// HandleForgotPassword handles forgot password request
func (h *AuthHandlers) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderForgotPasswordPage(w, r, nil)
		return
	}

	// Check rate limit for forgot password
	ip := getClientIP(r)
	if !forgotRateLimiter.Allow(ip) {
		h.renderForgotPasswordPage(w, r, map[string]string{"error": "Too many requests. Please try again later."})
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))

	// Find user by email
	u, err := h.server.userRepo.GetByEmail(r.Context(), email)
	if err != nil {
		// Don't reveal if email exists
		h.renderForgotPasswordPage(w, r, map[string]string{"success": "If this email is registered, you will receive reset instructions."})
		return
	}

	// Generate reset token
	token, err := u.GenerateResetToken()
	if err != nil {
		h.renderForgotPasswordPage(w, r, map[string]string{"error": "Server error"})
		return
	}

	// Save token to database
	if err := h.server.userRepo.Update(r.Context(), u); err != nil {
		h.renderForgotPasswordPage(w, r, map[string]string{"error": "Server error"})
		return
	}

	// Send password reset email
	if err := h.server.emailService.SendPasswordResetEmail(email, u.Username, token); err != nil {
		log.Printf("Failed to send password reset email to %s: %v", email, err)
		// Still show success - don't reveal if email exists
	}

	h.renderForgotPasswordPage(w, r, map[string]string{"success": "If this email is registered, you will receive reset instructions."})
}

// HandleResetPassword handles password reset
func (h *AuthHandlers) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, h.server.config.Server.WebPrefix+"/forgot-password", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet {
		// Verify token is valid
		_, err := h.server.userRepo.GetByResetToken(r.Context(), token)
		if err != nil {
			h.renderResetPasswordPage(w, r, token, map[string]string{"error": "Invalid or expired reset link"})
			return
		}
		h.renderResetPasswordPage(w, r, token, nil)
		return
	}

	// POST - process reset
	password := r.FormValue("password")

	u, err := h.server.userRepo.GetByResetToken(r.Context(), token)
	if err != nil {
		h.renderResetPasswordPage(w, r, token, map[string]string{"error": "Invalid or expired reset link"})
		return
	}

	// Set new password
	if err := u.SetPassword(password); err != nil {
		h.renderResetPasswordPage(w, r, token, map[string]string{"error": err.Error()})
		return
	}

	// Save changes
	if err := h.server.userRepo.Update(r.Context(), u); err != nil {
		h.renderResetPasswordPage(w, r, token, map[string]string{"error": "Server error"})
		return
	}

	// Redirect to login with success
	http.Redirect(w, r, h.server.config.Server.WebPrefix+"/login?reset=1", http.StatusSeeOther)
}

// HandleVerifyEmail handles email verification
func (h *AuthHandlers) HandleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, h.server.config.Server.WebPrefix+"/login", http.StatusSeeOther)
		return
	}

	u, err := h.server.userRepo.GetByVerifyToken(r.Context(), token)
	if err != nil {
		h.renderMessagePage(w, r, "Verification Failed", "Invalid or expired verification link.")
		return
	}

	// Verify email
	u.VerifyEmail()

	// Save changes
	if err := h.server.userRepo.Update(r.Context(), u); err != nil {
		h.renderMessagePage(w, r, "Verification Failed", "Server error. Please try again.")
		return
	}

	// Redirect to login with success
	http.Redirect(w, r, h.server.config.Server.WebPrefix+"/login?verified=1", http.StatusSeeOther)
}

// HandleResendVerification handles re-sending the email-verification link.
// GET shows a form asking for the email address; POST processes it.
//
// Security / privacy posture:
//   - Doesn't disclose whether a given email is registered (always responds
//     with the same "if your account exists, we sent an email" message,
//     to avoid email enumeration via this endpoint).
//   - Per-user cooldown via domain method (user.CanResendVerification, 60s
//     default) — independent of HTTP-level per-IP rate limiting which
//     should also be wired at the route layer.
//   - Skips already-verified accounts silently (same generic response).
//   - SMTP send failures are logged but don't expose details to the
//     unauthenticated caller.
func (h *AuthHandlers) HandleResendVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.renderResendVerificationPage(w, r, nil)
		return
	}

	// POST — process resend request.
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if email == "" {
		h.renderResendVerificationPage(w, r, map[string]string{"error": "Email is required"})
		return
	}

	// Generic response shown for every outcome that isn't an outright
	// form-validation error. Keeps email-enumeration safe.
	const genericMsg = "If an account with that email exists and isn't already verified, a new verification link has been sent. Check your inbox (and spam folder)."

	u, err := h.server.userRepo.GetByEmail(r.Context(), email)
	if err != nil {
		// Account not found — fall through to generic response.
		h.renderMessagePage(w, r, "Verification email sent", genericMsg)
		return
	}

	if u.EmailVerified {
		// Already verified — silently OK, same generic message.
		h.renderMessagePage(w, r, "Verification email sent", genericMsg)
		return
	}

	// Per-user cooldown — protects against burst-resend abuse even when
	// HTTP-rate-limiter is bypassed (e.g. distributed IPs).
	allowed, remaining := u.CanResendVerification()
	if !allowed {
		secs := int(remaining.Seconds())
		if secs < 1 {
			secs = 1
		}
		h.renderResendVerificationPage(w, r, map[string]string{
			"error": fmt.Sprintf("Please wait %d second(s) before requesting another verification email.", secs),
		})
		return
	}

	// Regenerate token (invalidates the previous one) and persist.
	if err := u.RegenerateVerifyToken(); err != nil {
		log.Printf("RegenerateVerifyToken failed for %s: %v", email, err)
		h.renderMessagePage(w, r, "Verification email sent", genericMsg)
		return
	}
	if err := h.server.userRepo.Update(r.Context(), u); err != nil {
		log.Printf("user repo Update failed for resend (%s): %v", email, err)
		h.renderMessagePage(w, r, "Verification email sent", genericMsg)
		return
	}

	// Best-effort SMTP send — log failures, still show the generic
	// confirmation. Operators can grep `journalctl -u sopds` for actual
	// delivery problems.
	if err := h.server.emailService.SendVerificationEmail(email, u.Username, u.VerifyToken); err != nil {
		log.Printf("SendVerificationEmail failed for %s: %v", email, err)
	}

	h.renderMessagePage(w, r, "Verification email sent", genericMsg)
}

// HandleLanding handles the landing page for unauthenticated users
func (h *AuthHandlers) HandleLanding(w http.ResponseWriter, r *http.Request) {
	h.renderLandingPage(w, r)
}

// HandleGuestLogin handles "continue as guest" action
func (h *AuthHandlers) HandleGuestLogin(w http.ResponseWriter, r *http.Request) {
	// Check if basic auth is required
	if h.server.config.Server.Auth.Enabled {
		// Basic auth is enabled - let the existing middleware handle it
		// Set a flag cookie to indicate guest mode was chosen
		http.SetCookie(w, &http.Cookie{
			Name:     "sopds_guest_mode",
			Value:    "1",
			Path:     "/",
			MaxAge:   int(AnonCookieExpiry.Seconds()),
			HttpOnly: true,
		})
	}

	// Generate anonymous session ID if not exists
	anonID := GetAnonCookie(r)
	if anonID == "" {
		anonID = generateAnonID()
		SetAnonCookie(w, anonID)
	}

	http.Redirect(w, r, h.server.config.Server.WebPrefix+"/", http.StatusSeeOther)
}

// migrateAnonBookshelf copies anonymous bookshelf items to user's account
func (h *AuthHandlers) migrateAnonBookshelf(ctx context.Context, r *http.Request, userID int64) {
	anonID := GetAnonCookie(r)
	if anonID == "" {
		return
	}

	// Get anonymous bookshelf items
	// Migrate to user's bookshelf (don't override existing)
	h.server.svc.MigrateAnonBookshelf(ctx, anonID, userID)
}

// Helper functions for rendering pages
func (h *AuthHandlers) renderLandingPage(w http.ResponseWriter, r *http.Request) {
	h.server.renderAuthTemplate(w, r, "landing", nil)
}

func (h *AuthHandlers) renderLoginPage(w http.ResponseWriter, r *http.Request, data map[string]string) {
	h.server.renderAuthTemplate(w, r, "login", data)
}

func (h *AuthHandlers) renderRegisterPage(w http.ResponseWriter, r *http.Request, data map[string]string) {
	h.server.renderAuthTemplate(w, r, "register", data)
}

func (h *AuthHandlers) renderForgotPasswordPage(w http.ResponseWriter, r *http.Request, data map[string]string) {
	h.server.renderAuthTemplate(w, r, "forgot-password", data)
}

func (h *AuthHandlers) renderResendVerificationPage(w http.ResponseWriter, r *http.Request, data map[string]string) {
	h.server.renderAuthTemplate(w, r, "resend-verification", data)
}

func (h *AuthHandlers) renderResetPasswordPage(w http.ResponseWriter, r *http.Request, token string, data map[string]string) {
	if data == nil {
		data = make(map[string]string)
	}
	data["token"] = token
	h.server.renderAuthTemplate(w, r, "reset-password", data)
}

func (h *AuthHandlers) renderMessagePage(w http.ResponseWriter, r *http.Request, title, message string) {
	h.server.renderAuthTemplate(w, r, "message", map[string]string{
		"title":   title,
		"message": message,
	})
}

// jsonResponse sends a JSON response
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// generateAnonID creates a unique anonymous session ID
func generateAnonID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
