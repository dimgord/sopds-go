package user

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// Validation errors
var (
	ErrInvalidUsername      = errors.New("username must be 3-30 characters, alphanumeric and underscores only")
	ErrInvalidEmail         = errors.New("invalid email address")
	ErrWeakPassword         = errors.New("password must be at least 8 characters with 1 lowercase, 1 uppercase, and 1 digit")
	ErrUserNotFound         = errors.New("user not found")
	ErrEmailExists          = errors.New("email already registered")
	ErrUsernameExists       = errors.New("username already taken")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrEmailNotVerified     = errors.New("email not verified")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrTooManyRequests      = errors.New("too many requests, please try again later")
)

// User represents a registered user
type User struct {
	ID                 int64
	Username           string
	Email              string
	PasswordHash       string
	EmailVerified      bool
	VerifyToken        string
	VerifyTokenExpires *time.Time
	VerifyTokenSentAt  *time.Time // when the most recent verification email was sent — gates resend cooldown
	ResetToken         string
	ResetTokenExpires  *time.Time
	CreatedAt          time.Time
	LastLogin          *time.Time
}

// VerifyResendCooldown is the minimum time between verification-email sends
// for a given user. Short enough to be friendly (user accidentally deleted
// the first email and wants another) but long enough to make burst-spam
// unattractive.
const VerifyResendCooldown = 60 * time.Second

// NewUser creates a new user with validated fields
func NewUser(username, email, password string) (*User, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(strings.ToLower(email))

	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Generate verification token
	verifyToken, err := generateToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	verifyExpires := now.Add(24 * time.Hour)

	return &User{
		Username:           username,
		Email:              email,
		PasswordHash:       string(hash),
		EmailVerified:      false,
		VerifyToken:        verifyToken,
		VerifyTokenExpires: &verifyExpires,
		VerifyTokenSentAt:  &now,
		CreatedAt:          now,
	}, nil
}

// RegenerateVerifyToken generates a fresh verification token (new value,
// 24h expiry, sent-at = now) for an already-existing user. Caller MUST
// check CanResendVerification first to enforce rate limit. The previous
// token is invalidated by replacement — any old verification email link
// stops working as soon as this returns.
func (u *User) RegenerateVerifyToken() error {
	token, err := generateToken()
	if err != nil {
		return err
	}
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	u.VerifyToken = token
	u.VerifyTokenExpires = &expires
	u.VerifyTokenSentAt = &now
	return nil
}

// CanResendVerification reports whether the cooldown has elapsed since the
// last verification email was sent. Returns (true, 0) if allowed, or
// (false, remaining) with the wait time until the next allowed send.
// Users with VerifyTokenSentAt == nil (pre-migration accounts, or freshly
// reset accounts) are always allowed to resend.
func (u *User) CanResendVerification() (allowed bool, remaining time.Duration) {
	if u.VerifyTokenSentAt == nil {
		return true, 0
	}
	elapsed := time.Since(*u.VerifyTokenSentAt)
	if elapsed >= VerifyResendCooldown {
		return true, 0
	}
	return false, VerifyResendCooldown - elapsed
}

// CheckPassword verifies the password against the hash
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}

// GenerateResetToken creates a password reset token
func (u *User) GenerateResetToken() (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(1 * time.Hour)
	u.ResetToken = token
	u.ResetTokenExpires = &expires
	return token, nil
}

// ClearResetToken removes the reset token
func (u *User) ClearResetToken() {
	u.ResetToken = ""
	u.ResetTokenExpires = nil
}

// SetPassword sets a new password
func (u *User) SetPassword(password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.ClearResetToken()
	return nil
}

// VerifyEmail marks the email as verified
func (u *User) VerifyEmail() {
	u.EmailVerified = true
	u.VerifyToken = ""
	u.VerifyTokenExpires = nil
	u.VerifyTokenSentAt = nil // clear cooldown state — irrelevant once verified
}

// UpdateLastLogin sets the last login time
func (u *User) UpdateLastLogin() {
	now := time.Now()
	u.LastLogin = &now
}

// IsResetTokenValid checks if the reset token is valid and not expired
func (u *User) IsResetTokenValid(token string) bool {
	if u.ResetToken == "" || token == "" || u.ResetToken != token {
		return false
	}
	if u.ResetTokenExpires == nil || time.Now().After(*u.ResetTokenExpires) {
		return false
	}
	return true
}

// IsVerifyTokenValid checks if the verify token is valid and not expired
func (u *User) IsVerifyTokenValid(token string) bool {
	if u.VerifyToken == "" || token == "" || u.VerifyToken != token {
		return false
	}
	if u.VerifyTokenExpires == nil || time.Now().After(*u.VerifyTokenExpires) {
		return false
	}
	return true
}

// ValidateUsername checks username format
func ValidateUsername(username string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 30 {
		return ErrInvalidUsername
	}
	// Alphanumeric and underscore only
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, username)
	if !matched {
		return ErrInvalidUsername
	}
	return nil
}

// ValidateEmail checks email format
func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	// Simple email regex
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`, email)
	if !matched {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword checks password strength
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrWeakPassword
	}
	var hasLower, hasUpper, hasDigit bool
	for _, c := range password {
		switch {
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}
	if !hasLower || !hasUpper || !hasDigit {
		return ErrWeakPassword
	}
	return nil
}

// PasswordStrength returns validation status for real-time feedback
type PasswordStrength struct {
	Length    bool `json:"length"`    // At least 8 chars
	Lowercase bool `json:"lowercase"` // Has lowercase
	Uppercase bool `json:"uppercase"` // Has uppercase
	Digit     bool `json:"digit"`     // Has digit
	Valid     bool `json:"valid"`     // All requirements met
}

// CheckPasswordStrength returns detailed password validation
func CheckPasswordStrength(password string) PasswordStrength {
	s := PasswordStrength{
		Length: len(password) >= 8,
	}
	for _, c := range password {
		switch {
		case unicode.IsLower(c):
			s.Lowercase = true
		case unicode.IsUpper(c):
			s.Uppercase = true
		case unicode.IsDigit(c):
			s.Digit = true
		}
	}
	s.Valid = s.Length && s.Lowercase && s.Uppercase && s.Digit
	return s
}

// generateToken creates a random 32-byte hex token
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
