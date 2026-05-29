package domain

import "fmt"

// FieldViolation is a single failed invariant: which field and why. Message
// is field-relative ("is required") so a transport renders "<field> <message>".
type FieldViolation struct {
	Field   string
	Message string
}

// ValidationError carries every invariant a value failed at once, so callers
// (and ultimately API clients) get the full list rather than one-at-a-time.
// Entity factories return this; transports translate it without needing to
// know about any specific field or rule.
type ValidationError struct {
	Violations []FieldViolation
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %d field(s)", len(e.Violations))
}

func (e *ValidationError) add(field, message string) {
	e.Violations = append(e.Violations, FieldViolation{Field: field, Message: message})
}
