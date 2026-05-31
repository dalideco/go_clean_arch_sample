package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/domain"
	"github.com/dali/go_project_sample/internal/usecase"
)

// pgUniqueViolation is the SQLSTATE code Postgres returns for a unique-index
// violation (23505). We translate it to a domain-defined sentinel so callers
// don't have to know about pgconn.
const pgUniqueViolation = "23505"

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) List(ctx context.Context) ([]domain.User, error) {
	var models []userModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, err
	}
	users := make([]domain.User, len(models))
	for i, m := range models {
		u, err := m.toDomain()
		if err != nil {
			return nil, err
		}
		users[i] = u
	}
	return users, nil
}

func (r *UserRepository) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var m userModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, usecase.ErrUserNotFound
		}
		return nil, err
	}
	u, err := m.toDomain()
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var m userModel
	if err := r.db.WithContext(ctx).First(&m, "email = ?", email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, usecase.ErrUserNotFound
		}
		return nil, err
	}
	u, err := m.toDomain()
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	m := fromDomain(*u)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return usecase.ErrUserEmailTaken
		}
		return err
	}
	u.CreatedAt = m.CreatedAt
	u.UpdatedAt = m.UpdatedAt
	return nil
}
