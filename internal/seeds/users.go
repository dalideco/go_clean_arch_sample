package seeds

import (
	"context"
	"errors"
	"fmt"

	"github.com/dali/go_project_sample/internal/usecase"
)

var demoUsers = []struct{ Email, Name string }{
	{"alice@example.com", "Alice"},
	{"bob@example.com", "Bob"},
	{"carol@example.com", "Carol"},
}

func init() {
	register(Seeder{
		Name:   "users",
		Tables: []string{"users"},
		Run: func(ctx context.Context, repos usecase.Repositories) (int, error) {
			uc := usecase.NewUserUseCase(repos.Users)
			n := 0
			for _, d := range demoUsers {
				// Look first, only insert on a real miss — avoids a noisy failed
				// INSERT on every re-seed. The natural-key check is the seeder's
				// idempotency mechanism; ErrUserEmailTaken from Create is the
				// belt-and-braces guard against a TOCTOU race we don't care about
				// in a dev seeder.
				if _, err := uc.GetByEmail(ctx, d.Email); err == nil {
					continue
				} else if !errors.Is(err, usecase.ErrUserNotFound) {
					return n, fmt.Errorf("seed user %s lookup: %w", d.Email, err)
				}
				if _, err := uc.Create(ctx, d.Email, d.Name); err != nil {
					if errors.Is(err, usecase.ErrUserEmailTaken) {
						continue
					}
					return n, fmt.Errorf("seed user %s create: %w", d.Email, err)
				}
				n++
			}
			return n, nil
		},
	})
}
