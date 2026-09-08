package port

import "errors"

var (
	ErrConflict     = errors.New("event conflict")
	ErrInvalidEvent = errors.New("invalid event")
)
