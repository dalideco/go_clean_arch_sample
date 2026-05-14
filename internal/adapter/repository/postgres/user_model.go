package postgres

import (
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

func (m userModel) toDomain() domain.User {
	return domain.User{
		ID:        m.ID,
		Email:     m.Email,
		Name:      m.Name,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
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
