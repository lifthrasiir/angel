package chat

import (
	. "github.com/lifthrasiir/angel/internal/types"
)

func badRequestError(format string, args ...interface{}) error {
	return MakeBadRequestError(format, args...)
}

func notFoundError(format string, args ...interface{}) error {
	return MakeNotFoundError(format, args...)
}
