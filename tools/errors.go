package tools

import "errors"

// ExecutionError carries execution-specific error handling hints for the engine.
type ExecutionError struct {
	Err                 error
	PreserveObservation bool
}

func (e *ExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func PreserveObservationError(err error) error {
	if err == nil {
		return nil
	}
	var execErr *ExecutionError
	if errors.As(err, &execErr) {
		execErr.PreserveObservation = true
		return err
	}
	return &ExecutionError{
		Err:                 err,
		PreserveObservation: true,
	}
}

func ShouldPreserveObservationOnError(err error) bool {
	var execErr *ExecutionError
	return errors.As(err, &execErr) && execErr.PreserveObservation
}
