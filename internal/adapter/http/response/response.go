// Package response renders the project's uniform JSON envelope. Every API
// response carries a top-level "success" boolean. Success bodies put the
// payload under a named key (flat): {"success":true,"users":[...]}. Error
// bodies carry a machine code and optional field-level details:
// {"success":false,"error":"invalid_body","details":[{"field":...}]}.
package response

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/dali/go_clean_arch_sample/internal/domain"
)

// FieldError is one invalid field in an error response. Message is
// field-relative ("is required") so a client renders "<field> <message>".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// OK writes a 200 success envelope with payload under key.
func OK(c *gin.Context, key string, payload any) {
	c.JSON(http.StatusOK, gin.H{"success": true, key: payload})
}

// Created writes a 201 success envelope with payload under key.
func Created(c *gin.Context, key string, payload any) {
	c.JSON(http.StatusCreated, gin.H{"success": true, key: payload})
}

// Error writes an error envelope. Pass details for validation failures.
func Error(c *gin.Context, status int, code string, details ...FieldError) {
	c.JSON(status, errorBody(code, details))
}

// AbortError is Error for middleware: it aborts the handler chain.
func AbortError(c *gin.Context, status int, code string, details ...FieldError) {
	c.AbortWithStatusJSON(status, errorBody(code, details))
}

func errorBody(code string, details []FieldError) gin.H {
	body := gin.H{"success": false, "error": code}
	if len(details) > 0 {
		body["details"] = details
	}
	return body
}

// RegisterFieldNames makes the validator report the JSON field name (not the
// Go struct field name) in binding errors. Call once at startup.
func RegisterFieldNames() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
}

// ValidationDetails extracts per-field details from a validation error,
// whether it came from gin binding (validator.ValidationErrors) or the
// domain (*domain.ValidationError). Returns nil for any other error, so the
// caller can treat nil as "not a validation failure".
func ValidationDetails(err error) []FieldError {
	var bindErrs validator.ValidationErrors
	if errors.As(err, &bindErrs) {
		out := make([]FieldError, len(bindErrs))
		for i, fe := range bindErrs {
			out[i] = FieldError{Field: fe.Field(), Message: bindMessage(fe)}
		}
		return out
	}

	var domErr *domain.ValidationError
	if errors.As(err, &domErr) {
		out := make([]FieldError, len(domErr.Violations))
		for i, v := range domErr.Violations {
			out[i] = FieldError{Field: v.Field, Message: v.Message}
		}
		return out
	}

	return nil
}

func bindMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	default:
		return "is invalid"
	}
}
