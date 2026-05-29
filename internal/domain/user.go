package domain

import (
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        uuid.UUID
	Email     string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// validateUser checks the intrinsic invariants shared by creation and
// reconstitution, accumulating every violation into a *ValidationError so
// callers see all problems at once. Intrinsic and stable rules only —
// mutable policy (allowlists, tier limits, ...) belongs in the use case,
// never here, since these invariants also gate loading existing rows.
func validateUser(email, name string) error {
	ve := &ValidationError{}
	if strings.TrimSpace(email) == "" {
		ve.add("email", "is required")
	} else if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		ve.add("email", "must be a valid email address")
	}
	if strings.TrimSpace(name) == "" {
		ve.add("name", "is required")
	}
	if len(ve.Violations) > 0 {
		return ve
	}
	return nil
}

// NewUser is the creation factory: it validates the inputs and mints a fresh
// identity and timestamps. The caller reads u.ID from the result, so the ID
// is known before the row is inserted.
func NewUser(email, name string) (*User, error) {
	email, name = strings.TrimSpace(email), strings.TrimSpace(name)
	if err := validateUser(email, name); err != nil {
		return nil, err
	}
	now := time.Now()
	return &User{
		ID:        uuid.New(),
		Email:     email,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ReconstituteUser rebuilds a User loaded from persistence: same invariants,
// but the stored identity and timestamps are restored rather than minted. A
// validation failure here means stored data has drifted from the invariants
// (an out-of-band write or a tightened rule) and signals a needed migration.
func ReconstituteUser(id uuid.UUID, email, name string, createdAt, updatedAt time.Time) (*User, error) {
	if err := validateUser(email, name); err != nil {
		return nil, err
	}
	return &User{
		ID:        id,
		Email:     email,
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}
