package postgres

import (
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/usecase"
)

// NewRepositories builds the concrete GORM-backed repositories and returns
// them as the infrastructure-free usecase.Repositories bundle. This is the
// single seam where "we use GORM" meets the rest of the app — swapping the
// ORM means rewriting this package, not the wiring layer.
func NewRepositories(db *gorm.DB) usecase.Repositories {
	return usecase.Repositories{
		Users: NewUserRepository(db),
	}
}
