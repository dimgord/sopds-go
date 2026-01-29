package user

import (
	"errors"
	"testing"
	"time"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		username string
		valid    bool
	}{
		{"john", true},
		{"john_doe", true},
		{"user123", true},
		{"User_123", true},
		{"ab", false},                  // Too short
		{"a", false},                   // Too short
		{"", false},                    // Empty
		{"  john  ", true},             // Trimmed
		{"john-doe", false},            // Invalid char
		{"john.doe", false},            // Invalid char
		{"john@doe", false},            // Invalid char
		{"a23456789012345678901234567890", true},  // 30 chars - max
		{"a234567890123456789012345678901", false}, // 31 chars - too long
	}

	for _, tc := range tests {
		err := ValidateUsername(tc.username)
		if tc.valid && err != nil {
			t.Errorf("ValidateUsername(%q) returned error: %v, expected valid", tc.username, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidateUsername(%q) returned nil, expected error", tc.username)
		}
		if !tc.valid && err != nil && !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("ValidateUsername(%q) returned %v, expected ErrInvalidUsername", tc.username, err)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"test@example.com", true},
		{"user.name@domain.org", true},
		{"user+tag@example.co.uk", true},
		{"a@b.cc", true},
		{"", false},
		{"invalid", false},
		{"@example.com", false},
		{"user@", false},
		{"user@.com", false},
		{"user@domain", false},
	}

	for _, tc := range tests {
		err := ValidateEmail(tc.email)
		if tc.valid && err != nil {
			t.Errorf("ValidateEmail(%q) returned error: %v, expected valid", tc.email, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidateEmail(%q) returned nil, expected error", tc.email)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		password string
		valid    bool
	}{
		{"Password1", true},
		{"Abcdefg1", true},
		{"Complex1Pass", true},
		{"short", false},         // Too short
		{"password1", false},     // No uppercase
		{"PASSWORD1", false},     // No lowercase
		{"Password", false},      // No digit
		{"12345678", false},      // No letters
		{"", false},              // Empty
		{"Pass1", false},         // Too short
	}

	for _, tc := range tests {
		err := ValidatePassword(tc.password)
		if tc.valid && err != nil {
			t.Errorf("ValidatePassword(%q) returned error: %v, expected valid", tc.password, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidatePassword(%q) returned nil, expected error", tc.password)
		}
	}
}

func TestCheckPasswordStrength(t *testing.T) {
	tests := []struct {
		password  string
		length    bool
		lowercase bool
		uppercase bool
		digit     bool
		valid     bool
	}{
		{"Password1", true, true, true, true, true},
		{"pass", false, true, false, false, false},
		{"PASSWORD", true, false, true, false, false},  // 8 chars = length ok
		{"12345678", true, false, false, true, false},
		{"longpassword", true, true, false, false, false},
		{"LONGPASSWORD", true, false, true, false, false},
		{"LongPass1", true, true, true, true, true},
	}

	for _, tc := range tests {
		s := CheckPasswordStrength(tc.password)
		if s.Length != tc.length {
			t.Errorf("CheckPasswordStrength(%q).Length = %v, expected %v", tc.password, s.Length, tc.length)
		}
		if s.Lowercase != tc.lowercase {
			t.Errorf("CheckPasswordStrength(%q).Lowercase = %v, expected %v", tc.password, s.Lowercase, tc.lowercase)
		}
		if s.Uppercase != tc.uppercase {
			t.Errorf("CheckPasswordStrength(%q).Uppercase = %v, expected %v", tc.password, s.Uppercase, tc.uppercase)
		}
		if s.Digit != tc.digit {
			t.Errorf("CheckPasswordStrength(%q).Digit = %v, expected %v", tc.password, s.Digit, tc.digit)
		}
		if s.Valid != tc.valid {
			t.Errorf("CheckPasswordStrength(%q).Valid = %v, expected %v", tc.password, s.Valid, tc.valid)
		}
	}
}

func TestNewUser(t *testing.T) {
	user, err := NewUser("testuser", "test@example.com", "Password1")
	if err != nil {
		t.Fatalf("NewUser() returned error: %v", err)
	}

	if user.Username != "testuser" {
		t.Errorf("Username = %q, expected testuser", user.Username)
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email = %q, expected test@example.com", user.Email)
	}
	if user.PasswordHash == "" {
		t.Error("PasswordHash should not be empty")
	}
	if user.PasswordHash == "Password1" {
		t.Error("PasswordHash should be hashed, not plain text")
	}
	if user.EmailVerified {
		t.Error("EmailVerified should be false for new user")
	}
	if user.VerifyToken == "" {
		t.Error("VerifyToken should be set for new user")
	}
	if user.VerifyTokenExpires == nil {
		t.Error("VerifyTokenExpires should be set")
	}
}

func TestNewUserTrimsAndLowercases(t *testing.T) {
	user, err := NewUser("  TestUser  ", "  Test@EXAMPLE.COM  ", "Password1")
	if err != nil {
		t.Fatalf("NewUser() returned error: %v", err)
	}

	if user.Username != "TestUser" {
		t.Errorf("Username = %q, expected TestUser (trimmed)", user.Username)
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email = %q, expected test@example.com (trimmed and lowercased)", user.Email)
	}
}

func TestNewUserInvalidUsername(t *testing.T) {
	_, err := NewUser("ab", "test@example.com", "Password1")
	if err == nil {
		t.Error("NewUser with invalid username should return error")
	}
	if !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("Expected ErrInvalidUsername, got %v", err)
	}
}

func TestNewUserInvalidEmail(t *testing.T) {
	_, err := NewUser("testuser", "invalid-email", "Password1")
	if err == nil {
		t.Error("NewUser with invalid email should return error")
	}
	if !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("Expected ErrInvalidEmail, got %v", err)
	}
}

func TestNewUserWeakPassword(t *testing.T) {
	_, err := NewUser("testuser", "test@example.com", "weak")
	if err == nil {
		t.Error("NewUser with weak password should return error")
	}
	if !errors.Is(err, ErrWeakPassword) {
		t.Errorf("Expected ErrWeakPassword, got %v", err)
	}
}

func TestCheckPassword(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")

	if !user.CheckPassword("Password1") {
		t.Error("CheckPassword should return true for correct password")
	}
	if user.CheckPassword("WrongPassword1") {
		t.Error("CheckPassword should return false for wrong password")
	}
	if user.CheckPassword("") {
		t.Error("CheckPassword should return false for empty password")
	}
}

func TestSetPassword(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	oldHash := user.PasswordHash

	err := user.SetPassword("NewPassword2")
	if err != nil {
		t.Fatalf("SetPassword() returned error: %v", err)
	}

	if user.PasswordHash == oldHash {
		t.Error("PasswordHash should change after SetPassword")
	}
	if !user.CheckPassword("NewPassword2") {
		t.Error("CheckPassword should return true for new password")
	}
	if user.CheckPassword("Password1") {
		t.Error("CheckPassword should return false for old password")
	}
}

func TestSetPasswordWeak(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")

	err := user.SetPassword("weak")
	if err == nil {
		t.Error("SetPassword with weak password should return error")
	}
	if !errors.Is(err, ErrWeakPassword) {
		t.Errorf("Expected ErrWeakPassword, got %v", err)
	}
}

func TestGenerateResetToken(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")

	token, err := user.GenerateResetToken()
	if err != nil {
		t.Fatalf("GenerateResetToken() returned error: %v", err)
	}

	if token == "" {
		t.Error("Token should not be empty")
	}
	if user.ResetToken != token {
		t.Error("User.ResetToken should match returned token")
	}
	if user.ResetTokenExpires == nil {
		t.Error("ResetTokenExpires should be set")
	}
	if time.Now().After(*user.ResetTokenExpires) {
		t.Error("ResetTokenExpires should be in the future")
	}
}

func TestClearResetToken(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	user.GenerateResetToken()

	user.ClearResetToken()

	if user.ResetToken != "" {
		t.Errorf("ResetToken should be empty, got %q", user.ResetToken)
	}
	if user.ResetTokenExpires != nil {
		t.Error("ResetTokenExpires should be nil")
	}
}

func TestVerifyEmail(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")

	if user.EmailVerified {
		t.Error("New user should have EmailVerified = false")
	}

	user.VerifyEmail()

	if !user.EmailVerified {
		t.Error("EmailVerified should be true after VerifyEmail")
	}
	if user.VerifyToken != "" {
		t.Error("VerifyToken should be cleared")
	}
	if user.VerifyTokenExpires != nil {
		t.Error("VerifyTokenExpires should be nil")
	}
}

func TestUpdateLastLogin(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")

	if user.LastLogin != nil {
		t.Error("New user should have nil LastLogin")
	}

	before := time.Now()
	user.UpdateLastLogin()
	after := time.Now()

	if user.LastLogin == nil {
		t.Fatal("LastLogin should be set")
	}
	if user.LastLogin.Before(before) || user.LastLogin.After(after) {
		t.Error("LastLogin should be set to current time")
	}
}

func TestIsResetTokenValid(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	token, _ := user.GenerateResetToken()

	// Valid token
	if !user.IsResetTokenValid(token) {
		t.Error("IsResetTokenValid should return true for valid token")
	}

	// Wrong token
	if user.IsResetTokenValid("wrongtoken") {
		t.Error("IsResetTokenValid should return false for wrong token")
	}

	// Empty token
	if user.IsResetTokenValid("") {
		t.Error("IsResetTokenValid should return false for empty token")
	}

	// No token set
	user.ClearResetToken()
	if user.IsResetTokenValid(token) {
		t.Error("IsResetTokenValid should return false when no token set")
	}
}

func TestIsResetTokenValidExpired(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	token, _ := user.GenerateResetToken()

	// Set expired time
	expired := time.Now().Add(-1 * time.Hour)
	user.ResetTokenExpires = &expired

	if user.IsResetTokenValid(token) {
		t.Error("IsResetTokenValid should return false for expired token")
	}
}

func TestIsVerifyTokenValid(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	token := user.VerifyToken

	// Valid token
	if !user.IsVerifyTokenValid(token) {
		t.Error("IsVerifyTokenValid should return true for valid token")
	}

	// Wrong token
	if user.IsVerifyTokenValid("wrongtoken") {
		t.Error("IsVerifyTokenValid should return false for wrong token")
	}

	// Empty token
	if user.IsVerifyTokenValid("") {
		t.Error("IsVerifyTokenValid should return false for empty token")
	}

	// After verification
	user.VerifyEmail()
	if user.IsVerifyTokenValid(token) {
		t.Error("IsVerifyTokenValid should return false after email verified")
	}
}

func TestIsVerifyTokenValidExpired(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	token := user.VerifyToken

	// Set expired time
	expired := time.Now().Add(-25 * time.Hour)
	user.VerifyTokenExpires = &expired

	if user.IsVerifyTokenValid(token) {
		t.Error("IsVerifyTokenValid should return false for expired token")
	}
}

func TestSetPasswordClearsResetToken(t *testing.T) {
	user, _ := NewUser("testuser", "test@example.com", "Password1")
	user.GenerateResetToken()

	if user.ResetToken == "" {
		t.Error("ResetToken should be set before SetPassword")
	}

	user.SetPassword("NewPassword2")

	if user.ResetToken != "" {
		t.Error("SetPassword should clear ResetToken")
	}
	if user.ResetTokenExpires != nil {
		t.Error("SetPassword should clear ResetTokenExpires")
	}
}
