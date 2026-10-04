package providers

import "errors"

// ConsumerError identifies a downstream failure, preserving its cause.
type ConsumerError struct{ Err error }

func (e *ConsumerError) Error() string { return e.Err.Error() }
func (e *ConsumerError) Unwrap() error { return e.Err }
func IsConsumerError(err error) bool   { var consumer *ConsumerError; return errors.As(err, &consumer) }
