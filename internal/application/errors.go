package application

import "errors"

// ErrEventNotFound is the sentinel for "no event with the given ID".
// Detect with errors.Is(err, ErrEventNotFound) in callers.
var ErrEventNotFound = errors.New("event not found")

// ErrInvalidSignature is the sentinel for "signature did not match".
// Detect with errors.Is. Returns 401 in HTTP layer.
var ErrInvalidSignature = errors.New("invalid signature")
