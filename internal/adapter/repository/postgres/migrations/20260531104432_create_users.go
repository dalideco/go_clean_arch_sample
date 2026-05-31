package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// migration0001CreateUsers creates the users table. The usersV1 struct is a
// frozen snapshot of the schema at this point; the live userModel may evolve
// freely without affecting how this migration runs on a fresh database.
var migration0001CreateUsers = &gormigrate.Migration{
	ID: "0001_create_users",
	Migrate: func(tx *gorm.DB) error {
		type usersV1 struct {
			ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
			Email     string    `gorm:"uniqueIndex;not null"`
			Name      string    `gorm:"not null"`
			CreatedAt time.Time
			UpdatedAt time.Time
		}
		// Pin the table name so GORM's pluralizer can't surprise us across
		// versions ("usersV1" otherwise becomes "users_v_1" or similar).
		return tx.Table("users").Migrator().CreateTable(&usersV1{})
	},
	Rollback: func(tx *gorm.DB) error {
		return tx.Migrator().DropTable("users")
	},
}
