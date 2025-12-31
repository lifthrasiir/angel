package types

import "fmt"

// BadRequestError represents a 400 Bad Request error.
type BadRequestError struct{ error }

// NotFoundError represents a 404 Not Found error.
type NotFoundError struct{ error }

// MakeBadRequestError creates a new BadRequestError with formatted message.
func MakeBadRequestError(format string, args ...interface{}) error {
	return BadRequestError{fmt.Errorf(format, args...)}
}

// MakeNotFoundError creates a new NotFoundError with formatted message.
func MakeNotFoundError(format string, args ...interface{}) error {
	return NotFoundError{fmt.Errorf(format, args...)}
}
