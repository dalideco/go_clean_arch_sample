package response

import (
	"errors"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/domain"
)

func TestValidationDetails_NilForRandomErr(t *testing.T) {
	out := ValidationDetails(errors.New("something else"))
	assert.Nil(t, out, "non-validation errors return nil")
}

func TestValidationDetails_NilOnPlainNil(t *testing.T) {
	assert.Nil(t, ValidationDetails(nil))
}

func TestValidationDetails_FromDomainValidationError(t *testing.T) {
	domErr := &domain.ValidationError{
		Violations: []domain.FieldViolation{
			{Field: "email", Message: "is required"},
			{Field: "name", Message: "must not be empty"},
		},
	}

	out := ValidationDetails(domErr)
	require.Len(t, out, 2)
	assert.Equal(t, FieldError{Field: "email", Message: "is required"}, out[0])
	assert.Equal(t, FieldError{Field: "name", Message: "must not be empty"}, out[1])
}

func TestValidationDetails_UnwrapsWrappedDomainError(t *testing.T) {
	// fmt.Errorf("...: %w", &domain.ValidationError{...}) should still match.
	domErr := &domain.ValidationError{
		Violations: []domain.FieldViolation{{Field: "email", Message: "bad"}},
	}
	wrapped := errors.Join(errors.New("layered"), domErr)

	out := ValidationDetails(wrapped)
	require.Len(t, out, 1)
	assert.Equal(t, "email", out[0].Field)
}

// ----- gin/binding integration -----

type sampleReq struct {
	Email string `json:"email" binding:"required,email"`
	Name  string `json:"my_name" binding:"required"`
}

func TestValidationDetails_FromGinBindingError(t *testing.T) {
	RegisterFieldNames() // makes the validator report JSON field names

	v := binding.Validator.Engine().(*validator.Validate)
	err := v.Struct(sampleReq{}) // both fields empty → two field errors
	require.Error(t, err)

	out := ValidationDetails(err)
	require.Len(t, out, 2)

	fields := map[string]string{}
	for _, fe := range out {
		fields[fe.Field] = fe.Message
	}
	// Both should use *json* names, not Go struct names.
	assert.Equal(t, "is required", fields["email"])
	assert.Equal(t, "is required", fields["my_name"], "JSON tag honored, not the Go field name")
}

func TestValidationDetails_BindingEmailFormatMessage(t *testing.T) {
	RegisterFieldNames()
	v := binding.Validator.Engine().(*validator.Validate)
	err := v.Struct(sampleReq{Email: "not-an-email", Name: "x"})
	require.Error(t, err)

	out := ValidationDetails(err)
	require.Len(t, out, 1)
	assert.Equal(t, "email", out[0].Field)
	assert.Equal(t, "must be a valid email address", out[0].Message)
}
