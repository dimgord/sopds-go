package repository

import (
	"context"

	"github.com/sopds/sopds-go/internal/domain/user"
)

// UserRepository defines operations for user persistence
type UserRepository interface {
	// Create saves a new user
	Create(ctx context.Context, u *user.User) error

	// GetByID finds a user by ID
	GetByID(ctx context.Context, id int64) (*user.User, error)

	// GetByEmail finds a user by email
	GetByEmail(ctx context.Context, email string) (*user.User, error)

	// GetByUsername finds a user by username
	GetByUsername(ctx context.Context, username string) (*user.User, error)

	// GetByLogin finds a user by email or username (for login)
	GetByLogin(ctx context.Context, login string) (*user.User, error)

	// GetByVerifyToken finds a user by verification token
	GetByVerifyToken(ctx context.Context, token string) (*user.User, error)

	// GetByResetToken finds a user by reset token
	GetByResetToken(ctx context.Context, token string) (*user.User, error)

	// Update saves changes to an existing user
	Update(ctx context.Context, u *user.User) error

	// ExistsEmail checks if email is already registered
	ExistsEmail(ctx context.Context, email string) (bool, error)

	// ExistsUsername checks if username is already taken
	ExistsUsername(ctx context.Context, username string) (bool, error)

	// UpdateLastLogin updates the last login timestamp
	UpdateLastLogin(ctx context.Context, id int64) error
}
