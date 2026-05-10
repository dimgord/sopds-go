package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"strings"

	"github.com/dimgord/sopds-go/internal/config"
)

// EmailService handles sending emails
type EmailService struct {
	config    *config.SMTPConfig
	siteTitle string
	baseURL   string
}

// NewEmailService creates a new email service
func NewEmailService(cfg *config.SMTPConfig, siteTitle, baseURL string) *EmailService {
	return &EmailService{
		config:    cfg,
		siteTitle: siteTitle,
		baseURL:   baseURL,
	}
}

// IsEnabled returns true if email sending is enabled
func (e *EmailService) IsEnabled() bool {
	return e.config != nil && e.config.Enabled
}

// SendVerificationEmail sends an email verification link
func (e *EmailService) SendVerificationEmail(toEmail, username, token string) error {
	if !e.IsEnabled() {
		log.Printf("VERIFY EMAIL: User %s (%s) - Token: %s", username, toEmail, token)
		log.Printf("VERIFY EMAIL: Link: %s/web/verify-email?token=%s", e.baseURL, token)
		return nil
	}

	verifyURL := fmt.Sprintf("%s/web/verify-email?token=%s", e.baseURL, token)

	subject := fmt.Sprintf("Verify your email - %s", e.siteTitle)
	body := fmt.Sprintf(`Hello %s,

Welcome to %s!

Please verify your email address by clicking the link below:

%s

This link will expire in 24 hours.

If you did not create an account, you can safely ignore this email.

Best regards,
%s
`, username, e.siteTitle, verifyURL, e.siteTitle)

	return e.sendEmail(toEmail, subject, body)
}

// SendPasswordResetEmail sends a password reset link
func (e *EmailService) SendPasswordResetEmail(toEmail, username, token string) error {
	if !e.IsEnabled() {
		log.Printf("RESET PASSWORD: User %s (%s) - Token: %s", username, toEmail, token)
		log.Printf("RESET PASSWORD: Link: %s/web/reset-password?token=%s", e.baseURL, token)
		return nil
	}

	resetURL := fmt.Sprintf("%s/web/reset-password?token=%s", e.baseURL, token)

	subject := fmt.Sprintf("Reset your password - %s", e.siteTitle)
	body := fmt.Sprintf(`Hello %s,

You requested to reset your password for %s.

Click the link below to set a new password:

%s

This link will expire in 1 hour.

If you did not request a password reset, you can safely ignore this email.

Best regards,
%s
`, username, e.siteTitle, resetURL, e.siteTitle)

	return e.sendEmail(toEmail, subject, body)
}

// sendEmail sends an email using SMTP
func (e *EmailService) sendEmail(to, subject, body string) error {
	from := e.config.From
	if from == "" {
		from = fmt.Sprintf("%s <noreply@%s>", e.siteTitle, e.config.Host)
	}

	// Build email message
	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"%s", from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", e.config.Host, e.config.Port)

	// Setup authentication
	var auth smtp.Auth
	if e.config.Username != "" {
		auth = smtp.PlainAuth("", e.config.Username, e.config.Password, e.config.Host)
	}

	// Extract email address from "Name <email>" format
	fromEmail := from
	if idx := strings.Index(from, "<"); idx != -1 {
		fromEmail = strings.TrimSuffix(from[idx+1:], ">")
	}

	// Send with TLS (port 465) or STARTTLS (port 587)
	if e.config.UseTLS {
		return e.sendWithTLS(addr, auth, fromEmail, to, []byte(msg))
	}

	// Use standard SMTP (with optional STARTTLS)
	return smtp.SendMail(addr, auth, fromEmail, []string{to}, []byte(msg))
}

// sendWithTLS sends email using implicit TLS (port 465)
func (e *EmailService) sendWithTLS(addr string, auth smtp.Auth, from, to string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName: e.config.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect with TLS: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.config.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT command failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close email body: %w", err)
	}

	return client.Quit()
}
