package application

import "errors"

// ErrEventNotFound is the sentinel for "no event with the given ID".
// Detect with errors.Is(err, ErrEventNotFound) in callers.
var ErrEventNotFound = errors.New("event not found")
