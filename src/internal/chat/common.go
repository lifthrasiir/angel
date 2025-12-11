package chat

import "fmt"

type BadRequestError struct{ error }
type NotFoundError struct{ error }

func badRequestError(format string, args ...interface{}) error {
	return BadRequestError{fmt.Errorf(format, args...)}
}

func notFoundError(format string, args ...interface{}) error {
	return NotFoundError{fmt.Errorf(format, args...)}
}
