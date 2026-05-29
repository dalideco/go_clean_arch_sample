package postgres

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dali/go_project_sample/internal/domain"
)

type userModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email     string    `gorm:"uniqueIndex;not null"`
	Name      string    `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (userModel) TableName() string { return "users" }

func (m userModel) toDomain() (domain.User, error) {
	u, err := domain.ReconstituteUser(m.ID, m.Email, m.Name, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("reconstitute user %s: %w", m.ID, err)
	}
	return *u, nil
}

func fromDomain(u domain.User) userModel {
	return userModel{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}
