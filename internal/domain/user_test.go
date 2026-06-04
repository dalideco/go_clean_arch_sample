package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUser_HappyPath(t *testing.T) {
	u, err := NewUser("alice@example.com", "Alice")
	require.NoError(t, err)
	require.NotNil(t, u)

	assert.NotEqual(t, uuid.Nil, u.ID, "ID should be minted")
	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, "Alice", u.Name)
	assert.False(t, u.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, u.UpdatedAt.IsZero(), "UpdatedAt should be set")
	assert.Equal(t, u.CreatedAt, u.UpdatedAt, "create + update equal on initial mint")
}

func TestNewUser_TrimsWhitespace(t *testing.T) {
	u, err := NewUser("  alice@example.com  ", "  Alice  ")
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, "Alice", u.Name)
}

func TestNewUser_Validation(t *testing.T) {
	type want struct {
		field, message string
	}
	cases := []struct {
		name       string
		email      string
		userName   string
		violations []want
	}{
		{
			name:       "empty email",
			email:      "",
			userName:   "Alice",
			violations: []want{{"email", "is required"}},
		},
		{
			name:       "whitespace email",
			email:      "   ",
			userName:   "Alice",
			violations: []want{{"email", "is required"}},
		},
		{
			name:       "malformed bare word",
			email:      "foo",
			userName:   "Alice",
			violations: []want{{"email", "must be a valid email address"}},
		},
		{
			name:       "missing tld",
			email:      "foo@",
			userName:   "Alice",
			violations: []want{{"email", "must be a valid email address"}},
		},
		{
			name:       "display-name form rejected",
			email:      "Foo <foo@example.com>",
			userName:   "Alice",
			violations: []want{{"email", "must be a valid email address"}},
		},
		{
			name:       "empty name",
			email:      "alice@example.com",
			userName:   "",
			violations: []want{{"name", "is required"}},
		},
		{
			name:       "whitespace-only name",
			email:      "alice@example.com",
			userName:   "   ",
			violations: []want{{"name", "is required"}},
		},
		{
			name:     "both invalid — accumulates all violations",
			email:    "",
			userName: "",
			violations: []want{
				{"email", "is required"},
				{"name", "is required"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := NewUser(tc.email, tc.userName)
			assert.Nil(t, u, "no user on validation failure")
			require.Error(t, err)

			var ve *ValidationError
			require.True(t, errors.As(err, &ve), "expected *ValidationError, got %T", err)
			require.Len(t, ve.Violations, len(tc.violations))
			for i, exp := range tc.violations {
				assert.Equal(t, exp.field, ve.Violations[i].Field)
				assert.Equal(t, exp.message, ve.Violations[i].Message)
			}
		})
	}
}

func TestReconstituteUser_HappyPath(t *testing.T) {
	id := uuid.New()
	created := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)

	u, err := ReconstituteUser(id, "alice@example.com", "Alice", created, updated)
	require.NoError(t, err)
	require.NotNil(t, u)

	// Identity and timestamps come from the caller (the DB row); they are
	// *not* freshly minted. This is the read path's whole job.
	assert.Equal(t, id, u.ID)
	assert.Equal(t, created, u.CreatedAt)
	assert.Equal(t, updated, u.UpdatedAt)
	assert.Equal(t, "alice@example.com", u.Email)
}

func TestReconstituteUser_Validates(t *testing.T) {
	// Drift case: a stored row has data that no longer satisfies invariants
	// (or never did, via out-of-band insert). ReconstituteUser must surface
	// this, not silently return a "user" that lies about its validity.
	u, err := ReconstituteUser(uuid.New(), "garbage", "x", time.Now(), time.Now())
	assert.Nil(t, u)
	require.Error(t, err)

	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	require.Len(t, ve.Violations, 1)
	assert.Equal(t, "email", ve.Violations[0].Field)
}
