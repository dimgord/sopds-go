package persistence

import (
	"context"
	"strings"
	"time"

	"github.com/dimgord/sopds-go/internal/domain/user"
	"gorm.io/gorm"
)

// UserModel is the GORM model for users table
type UserModel struct {
	UserID             int64      `gorm:"column:user_id;primaryKey;autoIncrement"`
	Username           string     `gorm:"column:username;size:30;uniqueIndex;not null"`
	Email              string     `gorm:"column:email;size:255;uniqueIndex;not null"`
	PasswordHash       string     `gorm:"column:password_hash;size:255;not null"`
	EmailVerified      bool       `gorm:"column:email_verified;not null;default:false"`
	VerifyToken        *string    `gorm:"column:verify_token;size:64"`
	VerifyTokenExpires *time.Time `gorm:"column:verify_token_expires"`
	ResetToken         *string    `gorm:"column:reset_token;size:64"`
	ResetTokenExpires  *time.Time `gorm:"column:reset_token_expires"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null;default:now()"`
	LastLogin          *time.Time `gorm:"column:last_login"`
}

func (UserModel) TableName() string {
	return "users"
}

// UserRepository implements repository.UserRepository
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create saves a new user
func (r *UserRepository) Create(ctx context.Context, u *user.User) error {
	model := userToModel(u)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			if strings.Contains(err.Error(), "email") {
				return user.ErrEmailExists
			}
			if strings.Contains(err.Error(), "username") {
				return user.ErrUsernameExists
			}
		}
		return err
	}
	u.ID = model.UserID
	return nil
}

// GetByID finds a user by ID
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", id).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrUserNotFound
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// GetByEmail finds a user by email
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).Where("LOWER(email) = LOWER(?)", email).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrUserNotFound
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// GetByUsername finds a user by username
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).Where("LOWER(username) = LOWER(?)", username).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrUserNotFound
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// GetByLogin finds a user by email or username
func (r *UserRepository) GetByLogin(ctx context.Context, login string) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).
		Where("LOWER(email) = LOWER(?) OR LOWER(username) = LOWER(?)", login, login).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrUserNotFound
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// GetByVerifyToken finds a user by verification token
func (r *UserRepository) GetByVerifyToken(ctx context.Context, token string) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).
		Where("verify_token = ? AND verify_token_expires > ?", token, time.Now()).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrInvalidToken
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// GetByResetToken finds a user by reset token
func (r *UserRepository) GetByResetToken(ctx context.Context, token string) (*user.User, error) {
	var model UserModel
	if err := r.db.WithContext(ctx).
		Where("reset_token = ? AND reset_token_expires > ?", token, time.Now()).
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, user.ErrInvalidToken
		}
		return nil, err
	}
	return modelToUser(&model), nil
}

// Update saves changes to an existing user
func (r *UserRepository) Update(ctx context.Context, u *user.User) error {
	model := userToModel(u)
	return r.db.WithContext(ctx).Save(&model).Error
}

// ExistsEmail checks if email is already registered
func (r *UserRepository) ExistsEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&UserModel{}).
		Where("LOWER(email) = LOWER(?)", email).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ExistsUsername checks if username is already taken
func (r *UserRepository) ExistsUsername(ctx context.Context, username string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&UserModel{}).
		Where("LOWER(username) = LOWER(?)", username).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateLastLogin updates the last login timestamp
func (r *UserRepository) UpdateLastLogin(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&UserModel{}).
		Where("user_id = ?", id).
		Update("last_login", time.Now()).Error
}

// userToModel converts domain user to GORM model
func userToModel(u *user.User) *UserModel {
	model := &UserModel{
		UserID:             u.ID,
		Username:           u.Username,
		Email:              u.Email,
		PasswordHash:       u.PasswordHash,
		EmailVerified:      u.EmailVerified,
		VerifyTokenExpires: u.VerifyTokenExpires,
		ResetTokenExpires:  u.ResetTokenExpires,
		CreatedAt:          u.CreatedAt,
		LastLogin:          u.LastLogin,
	}
	if u.VerifyToken != "" {
		model.VerifyToken = &u.VerifyToken
	}
	if u.ResetToken != "" {
		model.ResetToken = &u.ResetToken
	}
	return model
}

// modelToUser converts GORM model to domain user
func modelToUser(m *UserModel) *user.User {
	u := &user.User{
		ID:                 m.UserID,
		Username:           m.Username,
		Email:              m.Email,
		PasswordHash:       m.PasswordHash,
		EmailVerified:      m.EmailVerified,
		VerifyTokenExpires: m.VerifyTokenExpires,
		ResetTokenExpires:  m.ResetTokenExpires,
		CreatedAt:          m.CreatedAt,
		LastLogin:          m.LastLogin,
	}
	if m.VerifyToken != nil {
		u.VerifyToken = *m.VerifyToken
	}
	if m.ResetToken != nil {
		u.ResetToken = *m.ResetToken
	}
	return u
}
