package client

import "fmt"

type AuthenticationStage string

const (
	AuthenticationStageAuthenticate AuthenticationStage = "authenticate"
	AuthenticationStageProfile      AuthenticationStage = "profile"
)

type AuthenticationStageError struct {
	Stage AuthenticationStage
	Err   error
}

func (e *AuthenticationStageError) Error() string {
	return fmt.Sprintf(
		"HDDT GDT %s stage failed: %v",
		e.Stage,
		e.Err,
	)
}

func (e *AuthenticationStageError) Unwrap() error {
	return e.Err
}

func newAuthenticationStageError(
	stage AuthenticationStage,
	err error,
) error {
	if err == nil {
		return nil
	}

	return &AuthenticationStageError{
		Stage: stage,
		Err:   err,
	}
}
